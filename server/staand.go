package server

import (
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// A stand answers on /s/{market}/{slug} (ADR-035): a public pixel that records
// one untrusted arrival per hit and returns a 1×1 GIF, so an <img> carries it
// with no CORS, no preflight, no script. "stand" in the UI, staand in the code.
const staandPathPrefix = "/s/"

// The stand's definition is a system attestation ROOT writes: trusted config,
// not market data. The latest of created and deleted for a stand's key is the
// whole truth of whether it stands. Arrivals — untrusted public writes — land
// in the market the definition names, never in system or default.
const (
	staandCreated = "staand:created"
	staandDeleted = "staand:deleted"
	staandSource  = "staand"
)

// staandPrefix fixes the vocabulary a stand may write. Every arrival's predicate
// is this prefix plus an event the pixel side names in its snippet (ADR-035),
// the way gtag('event', name, …) names an event. A stand writes nothing outside
// it. A bare hit is a page view, and page_view is Google's name for it,
// mirrored rather than invented — a site that already fires gtag arrives here
// speaking the word it already speaks.
const (
	staandPrefix  = "staand:"
	staandEvent   = "e"
	staandPage    = "page"
	staandVisitor = "v"
	staandVisit   = "visit"
	staandRef     = "ref"
	staandView    = "page_view"
)

// The campaign five, under the names every tool and every ad platform already
// uses, so a link built for anything else arrives here already correct. They are
// fields rather than parameters for that reason (ADR-036).
var staandCampaign = []string{
	"utm_source",
	"utm_medium",
	"utm_campaign",
	"utm_content",
	"utm_term",
}

// What the referrer becomes once it is taken apart. Grouping by domain is the
// question people ask, and a whole URL answers it only after being split again
// every time it is asked.
const (
	staandRefDomain = "referrer_domain"
	staandRefPath   = "referrer_path"
)

// A stand's definition carries no attributes: its key (market/slug) is the
// subject, its creator is the actor, and its write-origin is inherited from the
// namespace's front door (ADR-032), not stored on the stand.

// staandSlugAttr carries the slug on every arrival, so arrivals attribute back
// to the stand that recorded them. It is reserved: a pixel-side query parameter
// of this name is dropped, never written, so attribution cannot be forged.
const staandSlugAttr = "staand"

// An arrival is a stranger's claim, so what it carries is capped rather than
// trusted.
const (
	maxStaandSubject        = 128
	maxStaandAttributes     = 8
	maxStaandAttributeKey   = 32
	maxStaandAttributeValue = 128
)

// How much of a stand's life one read covers, and how many rows a breakdown
// answers with when the caller names no limit.
const (
	maxStaandRead    = 5000
	staandCountLimit = 100
)

// A 1×1 transparent GIF. Answering with an image is what lets an <img> carry
// the stand with no CORS, no preflight and no script.
var staandPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// staandMarket reports whether a namespace may hold a stand's arrivals. Never
// system or default: an arrival is an untrusted public write, and those two
// namespaces hold the node's own records — users, tokens, grants (ADR-026).
//
// The rule lives in server/namespaces, which is where the store is handed out,
// so a stand refused here is refused again at the door rather than only here.
func staandMarket(namespace string) bool {
	return namespaces.PublicMay(namespace)
}

// staandKey is a stand's identity in system: the market it feeds and its slug.
// The definition is stored under this, so /s/{market}/{slug} resolves to one
// definition and two markets may share a slug without colliding.
func staandKey(market, slug string) string {
	return market + "/" + slug
}

// staandDrop counts one arrival refused by the per-stand rate limit, so the
// market view can show recorded against rate-limited (ADR-035).
func (s *QNTXServer) staandDrop(key string) {
	v, _ := s.staandDrops.LoadOrStore(key, new(atomic.Int64))
	if c, ok := v.(*atomic.Int64); ok {
		c.Add(1)
	}
}

// staandDropCount reads how many arrivals a stand has had refused by its budget.
func (s *QNTXServer) staandDropCount(key string) int {
	if v, ok := s.staandDrops.Load(key); ok {
		if c, ok := v.(*atomic.Int64); ok {
			return int(c.Load())
		}
	}
	return 0
}

// staandEventCap is how many distinct events one stand may name in the Sentry
// dimension before the rest fold to "other". The pixel side names events, so
// this is what keeps a caller from inventing thousands of series.
const staandEventCap = 20

// staandEventSet is one stand's seen events, capped.
type staandEventSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (e *staandEventSet) allow(event string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.seen[event]; ok {
		return event
	}
	if len(e.seen) >= staandEventCap {
		return "other"
	}
	e.seen[event] = struct{}{}
	return event
}

// staandEventDim bounds the event dimension for Sentry: a stand's first
// staandEventCap distinct events keep their name; past that, new ones fold into
// "other", so an invented event cannot explode the metric's cardinality.
func (s *QNTXServer) staandEventDim(key, event string) string {
	v, _ := s.staandEvents.LoadOrStore(key, &staandEventSet{seen: map[string]struct{}{}})
	set, ok := v.(*staandEventSet)
	if !ok {
		return "other"
	}
	return set.allow(event)
}

// HandleStaand answers GET /s/{market}/{slug}. The market is where arrivals land
// and the slug names the stand; together they resolve to the stand's defining
// system attestation, which says whether it stands and the door it is bound to.
func (s *QNTXServer) HandleStaand(w http.ResponseWriter, r *http.Request) {
	// The pixel goes out whatever happened: probing a stand teaches nothing.
	defer func() {
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			if _, err := w.Write(staandPixel); err != nil {
				s.logger.Errorw("could not write the stand pixel", "error", err)
			}
		}
	}()

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return
	}

	rest, ok := strings.CutPrefix(r.URL.Path, staandPathPrefix)
	if !ok {
		return
	}
	market, slug, ok := strings.Cut(rest, "/")
	if !ok || market == "" || slug == "" || strings.Contains(slug, "/") {
		return
	}
	if !staandMarket(market) {
		s.logger.Infow("Stand arrival refused",
			"market", market, "slug", slug,
			"reason", "a stand market is never system or default")
		return
	}

	if !s.staandStands(market, slug) {
		s.logger.Infow("Stand arrival refused",
			"market", market, "slug", slug, "reason", "no stand stands here",
			"client", r.RemoteAddr)
		return
	}

	// A budget per stand, keyed by its address, so one busy stand cannot spend
	// another's and a flood spends only its own. The pixel still goes out; the
	// arrival is what a full bucket drops, and the drop is counted for the view.
	key := staandKey(market, slug)
	if s.rlStaand != nil && !s.rlStaand.allow(key) {
		s.staandDrop(key)
		s.logger.Infow("Stand arrival refused",
			"market", market, "slug", slug, "reason", "rate limit",
			"client", r.RemoteAddr)
		return
	}

	// The door: the write-origin is the namespace's front door (ADR-032). An
	// empty binding (no door) is open, and a missing Referer is allowed; only a
	// present host that does not match the door is refused.
	binding := s.namespaceDoorBinding(market)
	host := originHost(r.Referer())
	if !originAllowed(binding, host) {
		s.logger.Infow("Stand arrival refused",
			"market", market, "slug", slug, "reason", "origin not the namespace door",
			"door", binding, "host", host, "client", r.RemoteAddr)
		return
	}

	// Everything the hit says, read once into the shape ADR-036 fixed.
	now := time.Now()
	arrival := staandArrival(r, market, slug, now)

	// The predicate is the stand vocabulary plus the event the pixel side named:
	// ?e=contact_click records staand:contact_click. No event is a page view.
	predicate := staandPredicate(arrival.Event)

	// The subject is the thing the hit is about — the page — so the attestation
	// reads aloud (CDR-010): "/deep-clean staand:page_view at golem.club by
	// staand:boutique". The visitor id is an attribute, never the subject.
	subject := arrival.Path

	store, err := s.held.WriteAsPublic(market)
	if err != nil {
		s.logger.Errorw("Stand arrival lost: its market is not served",
			"market", market, "slug", slug, "error", err)
		return
	}

	// The context is the page the pixel fired from. It names the site and the
	// page, and the store already says which market (ADR-026, ADR-035).
	context := r.Referer()
	if context == "" {
		context = "_"
	}

	// The slug rides on the arrival (reserved key) so activity can attribute it;
	// the visitor id rides as an attribute (v), there only to count, never the
	// subject (CDR-010).
	attrs := staandAttrs(arrival)

	// The actor is the stand, forced here so the store cannot sign the arrival as
	// whoever it is: the arrival is the stand's claim, recorded and untrusted.
	actor := staandPrefix + slug
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, "_", store.AttestationExists)
	if err != nil {
		s.logger.Errorw("Stand arrival lost: no ASID for it",
			"market", market, "slug", slug, "subject", subject, "error", err)
		return
	}
	as := &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{context},
		Actors:     []string{actor},
		Source:     staandSource,
		Timestamp:  now,
		Attributes: attrs,
		CreatedAt:  now,
	}
	if err := store.CreateAttestation(as); err != nil {
		s.logger.Errorw("Stand arrival lost: the store did not take it",
			"market", market, "slug", slug, "subject", subject, "error", err)
		return
	}
	// Sentry gets the counter, sliced by stand and by event (ADR-035). The event
	// is caller-controlled, so it is bounded first: past a cap of distinct events
	// per stand, the rest fold to "other". The page stays out — unbounded, in the
	// glyph's fold instead.
	measure.Count(measure.StaandArrivals, 1,
		measure.String(measure.AttrStand, key),
		measure.String(measure.AttrEvent, s.staandEventDim(key, predicate)))
	s.logger.Infow("Stand arrival recorded",
		"market", market, "slug", slug, "subject", subject, "predicate", predicate)
}

// staandArrival reads a hit into the shape ADR-036 fixed. It is the one place
// the node decides what a request said, so what a stand can know is one struct
// to read rather than a handler to trace.
//
// The empty fields are the honest ones. Visit, the referrer split, the campaign
// five, browser, operating system, device, screen, geo and language have no
// source on this request yet; they are absent here, not hidden.
func staandArrival(r *http.Request, market, slug string, at time.Time) *protocol.Arrival {
	q := r.URL.Query()

	event := staandEventName(q.Get(staandEvent))
	if event == "" {
		// Google calls a bare hit page_view, so a bare hit here is page_view.
		// Borrowing the industry's word is not the node inventing one.
		event = staandView
	}

	domain, path := staandReferrer(q.Get(staandRef))

	return &protocol.Arrival{
		At:      at.Format(time.RFC3339Nano),
		Market:  market,
		Slug:    slug,
		Visitor: staandBounded(q.Get(staandVisitor)),
		Visit:   staandBounded(q.Get(staandVisit)),
		Path:    standPage(q.Get(staandPage)),
		Event:   event,

		ReferrerDomain: domain,
		ReferrerPath:   path,

		UtmSource:   staandBounded(q.Get(staandCampaign[0])),
		UtmMedium:   staandBounded(q.Get(staandCampaign[1])),
		UtmCampaign: staandBounded(q.Get(staandCampaign[2])),
		UtmContent:  staandBounded(q.Get(staandCampaign[3])),
		UtmTerm:     staandBounded(q.Get(staandCampaign[4])),

		Params: staandAttributes(q),
	}
}

// staandBounded is a stranger's value, kept only if it fits. Over the bound it
// is nothing rather than a truncated something: half an id identifies nobody,
// and half a campaign name is a campaign that was never run.
func staandBounded(v string) string {
	if len(v) > maxStaandAttributeValue {
		return ""
	}
	return v
}

// staandReferrer splits what the site says sent this person here. It is not the
// Referer header — on a pixel that names the page the img sits in, which is the
// site's own page and already the arrival's context.
func staandReferrer(raw string) (string, string) {
	if raw == "" || len(raw) > maxStaandAttributeValue {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Hostname()), u.Path
}

// staandAttrs is what the record keeps beyond its subject, predicate, context
// and actor. Empty means nil: a field the hit did not carry is absent from the
// attestation rather than present and blank.
func staandAttrs(a *protocol.Arrival) map[string]any {
	out := make(map[string]any, len(a.Params)+8)
	for k, v := range a.Params {
		out[k] = v
	}
	// The slug rides on every arrival (reserved key) so activity can attribute
	// one back to the stand that recorded it.
	out[staandSlugAttr] = a.Slug

	keep := func(key, value string) {
		if value != "" {
			out[key] = value
		}
	}
	keep(staandVisitor, a.Visitor)
	keep(staandVisit, a.Visit)
	keep(staandRefDomain, a.ReferrerDomain)
	keep(staandRefPath, a.ReferrerPath)
	keep(staandCampaign[0], a.UtmSource)
	keep(staandCampaign[1], a.UtmMedium)
	keep(staandCampaign[2], a.UtmCampaign)
	keep(staandCampaign[3], a.UtmContent)
	keep(staandCampaign[4], a.UtmTerm)
	return out
}

// staandPredicate is the stand vocabulary plus the event, which staandArrival
// has already sanitised and defaulted. A stand writes nothing outside it.
func staandPredicate(event string) string {
	return staandPrefix + event
}

// staandEventName is the event the pixel side named, sanitised to the vocabulary
// tail: letters, digits and -_. only (so page_view, contact_click pass), capped.
// Empty when unusable.
func staandEventName(local string) string {
	if local == "" || len(local) > maxStaandSubject {
		return ""
	}
	for _, r := range local {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum && r != '-' && r != '_' && r != '.' {
			return ""
		}
	}
	return local
}

// standPage is the page the hit is about — the arrival's subject (CDR-010). It is
// the caller's page path, trimmed, control-stripped and capped; empty falls back
// to "/". A stranger controls it, so it is bounded, not trusted.
func standPage(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxStaandSubject {
		raw = raw[:maxStaandSubject]
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "/"
	}
	return out
}

// attrString reads a string attribute, and the empty string when it is absent
// or not a string. The assertion's ok is read here, so no caller discards it.
func attrString(attrs map[string]any, key string) string {
	if v, ok := attrs[key].(string); ok {
		return v
	}
	return ""
}

// staandStands reports whether a stand stands. The definition is a system
// attestation; the latest of the stand's created and deleted lines is the whole
// truth. A create that nothing has deleted since is live.
func (s *QNTXServer) staandStands(market, slug string) bool {
	if !staandMarket(market) {
		return false
	}
	sys, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return false
	}
	key := staandKey(market, slug)
	// Query by the key alone and settle create against delete here: a store's
	// filter may AND several predicates rather than OR them, so asking for both
	// at once can match neither.
	found, err := sys.GetAttestations(ats.AttestationFilter{Subjects: []string{key}, Limit: 200})
	if err != nil {
		return false
	}
	latest := latestDefinition(found)
	return latest != nil && slices.Contains(latest.Predicates, staandCreated)
}

// namespaceDoorBinding is the write-origin a stand inherits: the hosts of its
// namespace's front door (ADR-032). A namespace with no door binds nothing, so
// the stand is open. The door's origins are full URLs; the binding is their
// hosts, space-joined, which originAllowed matches an arrival's host against.
func (s *QNTXServer) namespaceDoorBinding(market string) string {
	if s.deps == nil || s.deps.cfg == nil {
		return ""
	}
	door, ok := s.deps.cfg.Auth.Door[slug.Of(market)]
	if !ok {
		return ""
	}
	hosts := make([]string, 0, len(door.Origins))
	for _, o := range door.Origins {
		if h := originHost(o); h != "" {
			hosts = append(hosts, h)
		}
	}
	return strings.Join(hosts, " ")
}

// latestDefinition is the newest created-or-deleted line among some, or nil.
func latestDefinition(found []*types.As) *types.As {
	var latest *types.As
	for _, as := range found {
		if !slices.Contains(as.Predicates, staandCreated) && !slices.Contains(as.Predicates, staandDeleted) {
			continue
		}
		if latest == nil || as.Timestamp.After(latest.Timestamp) {
			latest = as
		}
	}
	return latest
}

// originHost is the host a Referer names, lowercased, or the empty string when
// there is none or it does not parse. It is what the door binding is checked
// against.
func originHost(referer string) string {
	if referer == "" {
		return ""
	}
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// originAllowed reports whether an arrival from host may write to a stand bound
// to origin. Empty binding is open; a present host must equal a bound host or be
// its subdomain; a missing host (no Referer) is allowed (ADR-035) — refusing it
// would shut out PDFs and other clients that carry no origin.
func originAllowed(origin, host string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	if host == "" {
		return true
	}
	for _, bound := range strings.Fields(origin) {
		bound = strings.ToLower(bound)
		if host == bound || strings.HasSuffix(host, "."+bound) {
			return true
		}
	}
	return false
}

// staandInfo is one stand as the stands glyph sees it. Beyond what it is (slug,
// namespace, URL) it carries its defining system attestation — the ASID, when
// it was created, and the DID that created it — the door it inherits from its
// namespace, the sites reporting back, and its activity: arrivals recorded
// against arrivals the rate limit refused, and when the last landed.
type staandInfo struct {
	Slug     string        `json:"slug"`
	Market   string        `json:"market"`
	URL      string        `json:"url"`
	Origin   string        `json:"origin"`
	Creator  string        `json:"creator"`
	DefID    string        `json:"defId"`
	Created  string        `json:"created"`
	Sites    []string      `json:"sites"`
	Arrivals int           `json:"arrivals"`
	Visitors int           `json:"visitors"`
	Dropped  int           `json:"dropped"`
	LastSeen string        `json:"lastSeen"`
	Events   []staandCount `json:"events"`
	Pages    []staandCount `json:"pages"`

	// How the people who arrived actually walked — what the counts above are
	// a fold of.
	Walks []staandWalk `json:"walks"`
}

// staandCount is one coarse tally the market view shows: a name (an event, or a
// page) and how many times it was attested (ADR-035).
type staandCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// staandStep is one arrival read as a step rather than as a number: when it
// landed, the page it was about, and the event the pixel named.
//
// when is the time itself and never leaves; At is what the view reads. Sorting
// the formatted string instead looks right and is not: Format writes whatever
// offset the store's time carries, so a tree of Z and +02:00 stamps orders by
// the digits of the hour and puts the walk in an order nobody walked.
type staandStep struct {
	At    string `json:"at"`
	Page  string `json:"page"`
	Event string `json:"event"`

	when time.Time
}

// staandWalk is one person's steps past the stand, in the order they took them.
// The visitor id is theirs and persists (the snippet keeps it in localStorage),
// so this is a person's whole path across every visit, not one sitting.
type staandWalk struct {
	Who   string       `json:"who"`
	Steps []staandStep `json:"steps"`
}

// walksOf turns the per-visitor steps into walks, most recently seen first, so
// whoever was here last is read first. Every walk, every step: what a stand
// recorded is what the view is given.
func walksOf(m map[string][]staandStep) []staandWalk {
	out := make([]staandWalk, 0, len(m))
	for who, steps := range m {
		out = append(out, staandWalk{Who: who, Steps: steps})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Steps, out[j].Steps
		if len(a) == 0 || len(b) == 0 {
			return len(a) > len(b)
		}
		last, other := a[len(a)-1].when, b[len(b)-1].when
		if !last.Equal(other) {
			return last.After(other)
		}
		return out[i].Who < out[j].Who
	})
	return out
}

// topCounts turns a count map into a list, most first, ties by name, capped.
func topCounts(m map[string]int, limit int) []staandCount {
	out := make([]staandCount, 0, len(m))
	for name, c := range m {
		out = append(out, staandCount{Name: name, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// HandleStaands is the market glyph's endpoint, ROOT only (reach table). GET
// lists every stand across all markets, POST creates one into a named market,
// DELETE takes one down. The definition is written into system either way.
func (s *QNTXServer) HandleStaands(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listStaands(w, r)
	case http.MethodPost:
		s.createStaand(w, r)
	case http.MethodDelete:
		s.deleteStaand(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET lists, POST creates, DELETE removes")
	}
}

func (s *QNTXServer) listStaands(w http.ResponseWriter, r *http.Request) {
	// The window the activity covers, in AX's own words. Naming none is a
	// stand's whole life, which is what this answered before it could be asked.
	since, until, err := staandRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	live, err := s.liveStaands(since, until)
	if err != nil {
		s.logger.Errorw("could not list the stands", "error", err)
		writeError(w, http.StatusBadRequest, "cannot list the stands")
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{"staands": live})
}

// createStaand writes the defining attestation for a new stand into system. The
// namespace it feeds is named in the body and is never system or default. The
// definition carries no attributes: the write-origin is the namespace's door.
func (s *QNTXServer) createStaand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Market string `json:"market"`
		Slug   string `json:"slug"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}
	if !staandMarket(req.Market) {
		writeError(w, http.StatusBadRequest, "a stand namespace is never system or default")
		return
	}
	if req.Slug == "" || strings.Contains(req.Slug, "/") {
		writeError(w, http.StatusBadRequest, "a stand needs a slug with no slash")
		return
	}
	if err := s.writeStaandDef(r, req.Market, req.Slug, staandCreated, nil); err != nil {
		s.logger.Errorw("could not create a stand",
			"market", req.Market, "slug", req.Slug, "error", err)
		writeError(w, http.StatusBadRequest, "could not create the stand in "+req.Market)
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{
		"slug": req.Slug, "url": staandPathPrefix + req.Market + "/" + req.Slug,
	})
}

// deleteStaand supersedes a stand with a deleted line, so its pixel stops
// recording. Both lines stay (ADR-026).
func (s *QNTXServer) deleteStaand(w http.ResponseWriter, r *http.Request) {
	market := r.URL.Query().Get("market")
	slug := r.URL.Query().Get("slug")
	if !staandMarket(market) {
		writeError(w, http.StatusBadRequest, "a stand market is never system or default")
		return
	}
	if slug == "" {
		writeError(w, http.StatusBadRequest, "name the slug to remove")
		return
	}
	if err := s.writeStaandDef(r, market, slug, staandDeleted, nil); err != nil {
		s.logger.Errorw("could not remove a stand", "market", market, "slug", slug, "error", err)
		writeError(w, http.StatusBadRequest, "could not remove the stand in "+market)
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{"slug": slug, "status": "removed"})
}

// writeStaandDef writes a created or deleted line for a stand into system,
// attributed to the identity that asked. The subject is the stand's key, so
// /s/{market}/{slug} resolves to it.
//
// A definition is trusted config rather than an arrival, so it is one of the
// lines the node keeps about itself. Who may write one is the reach table's:
// /api/staands is ROOT and SUPER.
func (s *QNTXServer) writeStaandDef(r *http.Request, market, slug, predicate string, attrs map[string]any) error {
	sys, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return err
	}
	actor := "root"
	if admitted, ok := auth.AdmissionFrom(r.Context()); ok && admitted.Identity != "" {
		actor = admitted.Identity
	}
	key := staandKey(market, slug)
	id, err := identity.GenerateASUIDWithRetry("AS", key, predicate, auth.NamespaceSystem, sys.AttestationExists)
	if err != nil {
		return err
	}
	now := time.Now()
	return sys.CreateAttestation(&types.As{
		ID:         id,
		Subjects:   []string{key},
		Predicates: []string{predicate},
		Contexts:   []string{auth.NamespaceSystem},
		Actors:     []string{actor},
		Source:     staandSource,
		Timestamp:  now,
		Attributes: attrs,
		CreatedAt:  now,
	})
}

// liveStaands is every stand that stands now, across all markets. The
// definitions are read from system, the latest line per stand decides, and a
// stand whose latest is a delete is left out. Each live stand is then filled
// with the activity read from the market it feeds.
func (s *QNTXServer) liveStaands(since, until *time.Time) ([]staandInfo, error) {
	sys, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, err
	}

	// Two single-predicate queries rather than one naming both, because a store
	// may AND the predicates of a filter rather than OR them.
	created, err := sys.GetAttestations(ats.AttestationFilter{Predicates: []string{staandCreated}, Limit: 1000})
	if err != nil {
		return nil, err
	}
	deleted, err := sys.GetAttestations(ats.AttestationFilter{Predicates: []string{staandDeleted}, Limit: 1000})
	if err != nil {
		return nil, err
	}

	latest := map[string]*types.As{}
	keep := func(as *types.As) {
		if len(as.Subjects) == 0 {
			return
		}
		key := as.Subjects[0]
		if cur, seen := latest[key]; !seen || as.Timestamp.After(cur.Timestamp) {
			latest[key] = as
		}
	}
	for _, as := range created {
		keep(as)
	}
	for _, as := range deleted {
		keep(as)
	}

	// Activity is read from each market once, not once per stand.
	activity := map[string]map[string]*staandTally{}

	var live []staandInfo
	for key, as := range latest {
		if !slices.Contains(as.Predicates, staandCreated) {
			continue
		}
		market, slug, ok := strings.Cut(key, "/")
		if !ok || !staandMarket(market) {
			continue
		}
		creator := ""
		if len(as.Actors) > 0 {
			creator = as.Actors[0]
		}
		info := staandInfo{
			Slug:    slug,
			Market:  market,
			URL:     staandPathPrefix + market + "/" + slug,
			Origin:  s.namespaceDoorBinding(market),
			Creator: creator,
			DefID:   as.ID,
			Created: as.Timestamp.Format(time.RFC3339),
			Sites:   []string{},
			Dropped: s.staandDropCount(key),
			Events:  []staandCount{},
			Pages:   []staandCount{},
			Walks:   []staandWalk{},
		}
		if _, done := activity[market]; !done {
			activity[market] = s.staandActivity(market, since, until)
		}
		if t, seen := activity[market][slug]; seen {
			info.Arrivals = t.count
			info.Visitors = len(t.visitors)
			info.Sites = t.sites()
			info.Events = topCounts(t.events, 20)
			info.Pages = topCounts(t.pages, 10)
			info.Walks = walksOf(t.walks)
			if !t.last.IsZero() {
				info.LastSeen = t.last.Format(time.RFC3339)
			}
		}
		live = append(live, info)
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].Market != live[j].Market {
			return live[i].Market < live[j].Market
		}
		return live[i].Slug < live[j].Slug
	})
	return live, nil
}

// staandDimension is what a breakdown groups by. Umami calls this `type` and
// its list is its own column names; ours are an arrival's fields (ADR-036), so
// nothing translates between what a stand records and what may be asked of it.
const (
	dimPage     = "page"
	dimEvent    = "event"
	dimSite     = "site"
	dimReferrer = "referrer"
	dimVisitor  = "visitor"
	dimVisit    = "visit"
)

// staandDimensionOf reads one arrival's value for a dimension, or the empty
// string when that arrival does not carry it. An arrival missing the dimension
// is not a row: it is a hit that did not answer this question.
func staandDimensionOf(as *types.As, dim string) string {
	switch dim {
	case dimPage:
		if len(as.Subjects) > 0 {
			return as.Subjects[0]
		}
		return ""
	case dimEvent:
		if len(as.Predicates) > 0 {
			return as.Predicates[0]
		}
		return ""
	case dimSite:
		if len(as.Contexts) > 0 {
			return originHost(as.Contexts[0])
		}
		return ""
	case dimReferrer:
		return attrString(as.Attributes, staandRefDomain)
	case dimVisitor:
		return attrString(as.Attributes, staandVisitor)
	case dimVisit:
		return attrString(as.Attributes, staandVisit)
	}
	// The campaign five answer under their own names, the ones every tool uses.
	if slices.Contains(staandCampaign, dim) {
		return attrString(as.Attributes, dim)
	}
	return ""
}

// staandKnownDimension reports whether a dimension is one a stand can answer.
// An unknown one is refused rather than answered with nothing, so a caller's
// typo does not read as a stand that saw no traffic.
func staandKnownDimension(dim string) bool {
	switch dim {
	case dimPage, dimEvent, dimSite, dimReferrer, dimVisitor, dimVisit:
		return true
	}
	return slices.Contains(staandCampaign, dim)
}

// staandRange is the window a read covers, in AX's own words: since and until
// take `yesterday`, `last monday` or an ISO stamp alike (ADR-036). Absent is
// unbounded, which is what a stand's whole life is.
func staandRange(r *http.Request) (*time.Time, *time.Time, error) {
	var since, until *time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := parser.ParseTemporalExpression(v)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "since %q is not a time", v)
		}
		since = t
	}
	if v := r.URL.Query().Get("until"); v != "" {
		t, err := parser.ParseTemporalExpression(v)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "until %q is not a time", v)
		}
		until = t
	}
	return since, until, nil
}

// HandleStaandMetrics answers GET /api/staands/metrics: one stand's arrivals
// grouped by one dimension, most first. One endpoint with a dimension parameter
// rather than an endpoint per question, which is what the industry settled on.
func (s *QNTXServer) HandleStaandMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "a breakdown is read, not written")
		return
	}
	q := r.URL.Query()
	market, slug := q.Get("market"), q.Get("slug")
	if market == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "a breakdown is of one stand: name market and slug")
		return
	}
	dim := q.Get("type")
	if !staandKnownDimension(dim) {
		writeError(w, http.StatusBadRequest,
			"no stand answers by "+dim+": ask by page, event, site, referrer, visitor, visit or a utm field")
		return
	}
	since, until, err := staandRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit := staandCountLimit
	if v := q.Get("limit"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "limit "+v+" is not a count")
			return
		}
		limit = n
	}

	store, err := s.held.Read(market)
	if err != nil {
		s.logger.Errorw("could not open a market for a breakdown",
			"market", market, "slug", slug, "type", dim, "error", err)
		writeError(w, http.StatusNotFound, "no market "+market+" is served here")
		return
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{
		Source:    staandSource,
		TimeStart: since,
		TimeEnd:   until,
		Limit:     maxStaandRead,
	})
	if err != nil {
		s.logger.Errorw("could not read arrivals for a breakdown",
			"market", market, "slug", slug, "type", dim, "error", err)
		writeError(w, http.StatusInternalServerError, "the store did not answer")
		return
	}

	counts := map[string]int{}
	for _, as := range arrivals {
		if attrString(as.Attributes, staandSlugAttr) != slug {
			continue
		}
		if v := staandDimensionOf(as, dim); v != "" {
			counts[v]++
		}
	}
	respond(w, s.logger, http.StatusOK, map[string]any{
		"market": market,
		"slug":   slug,
		"type":   dim,
		"counts": topCounts(counts, limit),
	})
}

// staandVisits derives one sitting per visit id. Nothing writes a Visit — the
// arrivals stay the record and this is a projection over them, the same way
// Plausible maintains sessions as a view over its event table (ADR-036).
//
// An arrival carrying no visit id belongs to no visit. Falling back to the
// visitor would turn a person's whole life into one sitting and call it a
// measurement, which is worse than answering with nothing.
func staandVisits(arrivals []*types.As, market, slug string) []*protocol.Visit {
	grouped := map[string][]*types.As{}
	for _, as := range arrivals {
		if attrString(as.Attributes, staandSlugAttr) != slug {
			continue
		}
		if id := attrString(as.Attributes, staandVisit); id != "" {
			grouped[id] = append(grouped[id], as)
		}
	}

	// Ordered on the instant, never on the formatted stamp: for an hour each
	// October two offsets are live at once and the text sorts backwards.
	type sitting struct {
		began time.Time
		visit *protocol.Visit
	}
	sittings := make([]sitting, 0, len(grouped))

	for id, steps := range grouped {
		sort.Slice(steps, func(i, j int) bool { return steps[i].Timestamp.Before(steps[j].Timestamp) })
		first, last := steps[0], steps[len(steps)-1]

		views := 0
		for _, as := range steps {
			if len(as.Predicates) > 0 && as.Predicates[0] == staandPrefix+staandView {
				views++
			}
		}
		sittings = append(sittings, sitting{began: first.Timestamp, visit: &protocol.Visit{
			Visit:           id,
			Visitor:         attrString(first.Attributes, staandVisitor),
			Market:          market,
			Slug:            slug,
			Started:         first.Timestamp.Format(time.RFC3339),
			Ended:           last.Timestamp.Format(time.RFC3339),
			DurationSeconds: uint32(last.Timestamp.Sub(first.Timestamp).Seconds()),
			EntryPath:       subjectOf(first),
			ExitPath:        subjectOf(last),
			Views:           uint32(views),
			Events:          uint32(len(steps)),
			// One arrival and gone, and that arrival a page view. A convention
			// rather than a measurement — Umami counts a sitting bounced when it
			// holds one hit and no custom event.
			Bounce: len(steps) == 1 && views == 1,
		}})
	}

	// Most recent first: a sitting is asked about while it is still recent.
	sort.Slice(sittings, func(i, j int) bool { return sittings[j].began.Before(sittings[i].began) })
	out := make([]*protocol.Visit, 0, len(sittings))
	for _, s := range sittings {
		out = append(out, s.visit)
	}
	return out
}

// subjectOf is the page an arrival is about, or empty when it names none.
func subjectOf(as *types.As) string {
	if len(as.Subjects) > 0 {
		return as.Subjects[0]
	}
	return ""
}

// staandActivityRow is one arrival read as a line rather than as a number. The
// shape is Umami's session activity: one row per event, newest first, and the
// reading of it is left to whoever asked (ADR-036).
type staandActivityRow struct {
	At             string `json:"at"`
	Visit          string `json:"visit,omitempty"`
	Visitor        string `json:"visitor,omitempty"`
	Path           string `json:"path"`
	Query          string `json:"query,omitempty"`
	ReferrerDomain string `json:"referrer_domain,omitempty"`
	Event          string `json:"event"`
}

// staandActivityCap is how many rows one activity read answers with. Umami's
// session activity stops at 500 and so does this.
const staandActivityCap = 500

// HandleStaandActivity answers GET /api/staands/activity: what happened, in
// order, as rows. Naming a visit is one sitting; naming a visitor is that
// person; naming neither is the stand.
func (s *QNTXServer) HandleStaandActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "activity is read, not written")
		return
	}
	q := r.URL.Query()
	market, slug := q.Get("market"), q.Get("slug")
	if market == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "activity is of one stand: name market and slug")
		return
	}
	since, until, err := staandRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wantVisit, wantVisitor := q.Get("visit"), q.Get("visitor")

	store, err := s.held.Read(market)
	if err != nil {
		s.logger.Errorw("could not open a market for activity",
			"market", market, "slug", slug, "error", err)
		writeError(w, http.StatusNotFound, "no market "+market+" is served here")
		return
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{
		Source:    staandSource,
		TimeStart: since,
		TimeEnd:   until,
		Limit:     maxStaandRead,
	})
	if err != nil {
		s.logger.Errorw("could not read arrivals for activity",
			"market", market, "slug", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "the store did not answer")
		return
	}

	kept := make([]*types.As, 0, len(arrivals))
	for _, as := range arrivals {
		if attrString(as.Attributes, staandSlugAttr) != slug {
			continue
		}
		if wantVisit != "" && attrString(as.Attributes, staandVisit) != wantVisit {
			continue
		}
		if wantVisitor != "" && attrString(as.Attributes, staandVisitor) != wantVisitor {
			continue
		}
		kept = append(kept, as)
	}
	// Newest first, on the instant rather than the stamp, the way Umami orders
	// a session's activity.
	sort.Slice(kept, func(i, j int) bool { return kept[j].Timestamp.Before(kept[i].Timestamp) })
	if len(kept) > staandActivityCap {
		kept = kept[:staandActivityCap]
	}

	rows := make([]staandActivityRow, 0, len(kept))
	for _, as := range kept {
		event := ""
		if len(as.Predicates) > 0 {
			event = as.Predicates[0]
		}
		rows = append(rows, staandActivityRow{
			At:             as.Timestamp.Format(time.RFC3339),
			Visit:          attrString(as.Attributes, staandVisit),
			Visitor:        attrString(as.Attributes, staandVisitor),
			Path:           subjectOf(as),
			ReferrerDomain: attrString(as.Attributes, staandRefDomain),
			Event:          event,
		})
	}
	respond(w, s.logger, http.StatusOK, map[string]any{
		"market":   market,
		"slug":     slug,
		"activity": rows,
	})
}

// HandleStaandVisits answers GET /api/staands/visits: one stand's sittings,
// derived. Entry, exit, duration and bounce are computed here every time and
// stored nowhere, because a visit is a projection (ADR-036).
func (s *QNTXServer) HandleStaandVisits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "a visit is derived, not written")
		return
	}
	q := r.URL.Query()
	market, slug := q.Get("market"), q.Get("slug")
	if market == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "visits are of one stand: name market and slug")
		return
	}
	since, until, err := staandRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	store, err := s.held.Read(market)
	if err != nil {
		s.logger.Errorw("could not open a market for visits",
			"market", market, "slug", slug, "error", err)
		writeError(w, http.StatusNotFound, "no market "+market+" is served here")
		return
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{
		Source:    staandSource,
		TimeStart: since,
		TimeEnd:   until,
		Limit:     maxStaandRead,
	})
	if err != nil {
		s.logger.Errorw("could not read arrivals for visits",
			"market", market, "slug", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "the store did not answer")
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{
		"market": market,
		"slug":   slug,
		"visits": staandVisits(arrivals, market, slug),
	})
}

// staandTally is one stand's arrivals folded down: how many, when the last one
// landed, and the distinct sites they came from.
//
// walks is the fold that keeps its shape. A stand stands and a person walks
// past it, so the counts say how much arrived and the walks say what happened.
// Every other field here answers "how many"; this one answers "in what order".
type staandTally struct {
	count    int
	last     time.Time
	hosts    map[string]struct{}
	events   map[string]int
	pages    map[string]int
	visitors map[string]struct{}
	walks    map[string][]staandStep
}

// sites is the distinct hosts arrivals came from, sorted. A stand bound to a
// door still shows here what actually reported, so the door and the reality are
// both visible.
func (t *staandTally) sites() []string {
	out := make([]string, 0, len(t.hosts))
	for h := range t.hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// staandActivity folds a market's arrivals down per slug. Arrivals carry the
// slug on a reserved attribute (staandSlugAttr) and their source is the stand;
// definitions live in system, so nothing here is a definition. The context is
// the page URL, so its host is the site that reported.
func (s *QNTXServer) staandActivity(market string, since, until *time.Time) map[string]*staandTally {
	tally := map[string]*staandTally{}
	store, err := s.held.Read(market)
	if err != nil {
		s.logger.Errorw("could not open a market for activity", "market", market, "error", err)
		return tally
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{
		Source:    staandSource,
		TimeStart: since,
		TimeEnd:   until,
		Limit:     maxStaandRead,
	})
	if err != nil {
		s.logger.Errorw("could not read stand arrivals for activity", "market", market, "error", err)
		return tally
	}
	for _, as := range arrivals {
		slug := attrString(as.Attributes, staandSlugAttr)
		if slug == "" {
			continue
		}
		t, seen := tally[slug]
		if !seen {
			t = &staandTally{hosts: map[string]struct{}{}, events: map[string]int{}, pages: map[string]int{}, visitors: map[string]struct{}{}, walks: map[string][]staandStep{}}
			tally[slug] = t
		}
		t.count++
		if len(as.Predicates) > 0 {
			t.events[as.Predicates[0]]++
		}
		if len(as.Subjects) > 0 {
			t.pages[as.Subjects[0]]++
		}
		if v := attrString(as.Attributes, staandVisitor); v != "" {
			t.visitors[v] = struct{}{}
			step := staandStep{At: as.Timestamp.Format(time.RFC3339), when: as.Timestamp}
			if len(as.Subjects) > 0 {
				step.Page = as.Subjects[0]
			}
			if len(as.Predicates) > 0 {
				step.Event = as.Predicates[0]
			}
			t.walks[v] = append(t.walks[v], step)
		}
		if as.Timestamp.After(t.last) {
			t.last = as.Timestamp
		}
		if len(as.Contexts) > 0 {
			if host := originHost(as.Contexts[0]); host != "" {
				t.hosts[host] = struct{}{}
			}
		}
	}

	for _, t := range tally {
		for _, steps := range t.walks {
			orderSteps(steps)
		}
	}
	return tally
}

// orderSteps puts one walk in the order it was walked. The store returns
// arrivals in its own order, so this is what makes the steps a path rather
// than a set.
//
// On the instant, never on the formatted stamp. For an hour each October two
// offsets are live at once, and 02:15+01:00 comes after 02:30+02:00 while
// sorting before it as text — the walk would read backwards exactly once a
// year, which is worse than reading backwards always.
func orderSteps(steps []staandStep) {
	sort.Slice(steps, func(i, j int) bool { return steps[i].when.Before(steps[j].when) })
}

// staandAttributes is what survives of the query string: every parameter but the
// event, the page, the visitor id and the reserved slug key, capped in count and
// size. Arrivals past the cap lose their tail rather than the whole arrival,
// which is the fact being recorded.
// staandReserved reports whether a query key is the stand's own. Reserved keys
// have a field on the arrival, so keeping them in params too would record the
// same fact twice under two names.
func staandReserved(key string) bool {
	switch key {
	case staandEvent, staandPage, staandVisitor, staandVisit, staandRef, staandSlugAttr:
		return true
	}
	return slices.Contains(staandCampaign, key)
}

func staandAttributes(params map[string][]string) map[string]string {
	out := make(map[string]string)
	for key, values := range params {
		if staandReserved(key) || len(values) == 0 {
			continue
		}
		if len(out) >= maxStaandAttributes {
			break
		}
		if len(key) > maxStaandAttributeKey || len(values[0]) > maxStaandAttributeValue {
			continue
		}
		out[key] = values[0]
	}
	return out
}
