package server

import (
	"context"
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
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// A datapunt answers on /d/{market}/{slug} (ADR-035): a public pixel that records
// one untrusted arrival per hit and returns a 1×1 GIF, so an <img> carries it
// with no CORS, no preflight, no script. "/d/ for datapixel".
const datapuntPathPrefix = "/d/"

// The datapunt's definition is an attestation written into system: trusted config,
// not market data. The latest of created and deleted for a datapunt's key is the
// whole truth of whether it stands. Arrivals — untrusted public writes — land
// in the market the definition names, never in system or default.
const (
	datapuntCreated = "datapunt:created"
	datapuntDeleted = "datapunt:deleted"
	datapuntSource  = "datapixel"
)

// What a pixel writes is sourced datapixel, not datapunt: the datapunt plugin
// writes its own statements in the same namespace, sourced datapunt, and a read
// of arrivals is a read of one source.

// datapuntPrefix fixes the vocabulary a datapunt may write. Every arrival's predicate
// is this prefix plus an event the pixel side names in its snippet (ADR-035),
// the way gtag('event', name, …) names an event. A datapunt writes nothing outside
// it. A bare hit is a page view, and page_view is Google's name for it,
// mirrored rather than invented — a site that already fires gtag arrives here
// speaking the word it already speaks.
const (
	datapuntPrefix  = "datapunt:"
	datapuntEvent   = "e"
	datapuntPage    = "page"
	datapuntVisitor = "v"
	datapuntVisit   = "visit"
	datapuntRef     = "ref"
	datapuntView    = "page_view"
)

// The campaign five, under the names every tool and every ad platform already
// uses, so a link built for anything else arrives here already correct. They are
// fields rather than parameters for that reason (ADR-036).
var datapuntCampaign = []string{
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
	datapuntRefDomain = "referrer_domain"
	datapuntRefPath   = "referrer_path"
)

// A datapunt's definition carries no attributes: its key (market/slug) is the
// subject, its creator is the actor, and its write-origin is inherited from the
// namespace's front door (ADR-032), not stored on the datapunt.

// datapuntSlugAttr carries the slug on every arrival, so arrivals attribute back
// to the datapunt that recorded them. It is reserved: a pixel-side query parameter
// of this name is dropped, never written, so attribution cannot be forged.
const datapuntSlugAttr = "datapunt"

// An arrival is a stranger's claim, so what it carries is capped rather than
// trusted.
const (
	maxDatapuntSubject        = 128
	maxDatapuntAttributes     = 8
	maxDatapuntAttributeKey   = 32
	maxDatapuntAttributeValue = 128
)

// The shortest thing the node will read as an id. Short enough that any minted
// value clears it, long enough that a word somebody typed does not.
const minDatapuntID = 8

// How much of a datapunt's life one read covers, and how many rows a breakdown
// answers with when the caller names no limit.
const (
	maxDatapuntRead    = 5000
	datapuntCountLimit = 100
)

// A 1×1 transparent GIF. Answering with an image is what lets an <img> carry
// the datapunt with no CORS, no preflight and no script.
var datapuntPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// datapuntMarket reports whether a namespace may hold a datapunt's arrivals. Never
// system or default: an arrival is an untrusted public write, and those two
// namespaces hold the node's own records — users, tokens, grants (ADR-026).
//
// The rule lives in server/namespaces, which is where the store is handed out,
// so a datapunt refused here is refused again at the door rather than only here.
func datapuntMarket(namespace string) bool {
	return namespaces.PublicMay(namespace)
}

// datapuntKey is a datapunt's identity in system: the market it feeds and its slug.
// The definition is stored under this, so /d/{market}/{slug} resolves to one
// definition and two markets may share a slug without colliding.
func datapuntKey(market, slug string) string {
	return market + "/" + slug
}

// datapuntDrop counts one arrival refused by the per-datapunt rate limit, so the
// market view can show recorded against rate-limited (ADR-035).
func (s *QNTXServer) datapuntDrop(key string) {
	v, _ := s.datapuntDrops.LoadOrStore(key, new(atomic.Int64))
	if c, ok := v.(*atomic.Int64); ok {
		c.Add(1)
	}
}

// datapuntDropCount reads how many arrivals a datapunt has had refused by its budget.
func (s *QNTXServer) datapuntDropCount(key string) int {
	if v, ok := s.datapuntDrops.Load(key); ok {
		if c, ok := v.(*atomic.Int64); ok {
			return int(c.Load())
		}
	}
	return 0
}

// datapuntEventCap is how many distinct events one datapunt may name in the Sentry
// dimension before the rest fold to "other". The pixel side names events, so
// this is what keeps a caller from inventing thousands of series.
const datapuntEventCap = 20

// datapuntEventSet is one datapunt's seen events, capped.
type datapuntEventSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (e *datapuntEventSet) allow(event string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.seen[event]; ok {
		return event
	}
	if len(e.seen) >= datapuntEventCap {
		return "other"
	}
	e.seen[event] = struct{}{}
	return event
}

// datapuntEventDim bounds the event dimension for Sentry: a datapunt's first
// datapuntEventCap distinct events keep their name; past that, new ones fold into
// "other", so an invented event cannot explode the metric's cardinality.
func (s *QNTXServer) datapuntEventDim(key, event string) string {
	v, _ := s.datapuntEvents.LoadOrStore(key, &datapuntEventSet{seen: map[string]struct{}{}})
	set, ok := v.(*datapuntEventSet)
	if !ok {
		return "other"
	}
	return set.allow(event)
}

// HandleDatapunt answers GET /d/{market}/{slug}. The market is where arrivals land
// and the slug names the datapunt; together they resolve to the datapunt's defining
// system attestation, which says whether it stands and the door it is bound to.
func (s *QNTXServer) HandleDatapunt(w http.ResponseWriter, r *http.Request) {
	// The pixel goes out whatever happened: probing a datapunt teaches nothing.
	defer func() {
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			if _, err := w.Write(datapuntPixel); err != nil {
				s.logger.Errorw("could not write the datapunt pixel", "error", err)
			}
		}
	}()

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return
	}

	rest, ok := strings.CutPrefix(r.URL.Path, datapuntPathPrefix)
	if !ok {
		return
	}
	market, slug, ok := strings.Cut(rest, "/")
	if !ok || market == "" || slug == "" || strings.Contains(slug, "/") {
		return
	}
	if !datapuntMarket(market) {
		s.logger.Infow("Datapunt arrival refused",
			"market", market, "slug", slug,
			"reason", "a datapunt market is never system or default")
		return
	}

	if !s.datapuntList(market, slug) {
		s.logger.Infow("Datapunt arrival refused",
			"market", market, "slug", slug, "reason", "no datapunt datapunts here",
			"client", r.RemoteAddr)
		return
	}

	// A budget per datapunt, keyed by its address, so one busy datapunt cannot spend
	// another's and a flood spends only its own. The pixel still goes out; the
	// arrival is what a full bucket drops, and the drop is counted for the view.
	key := datapuntKey(market, slug)
	if s.rlDatapunt != nil && !s.rlDatapunt.allow(key) {
		s.datapuntDrop(key)
		s.logger.Infow("Datapunt arrival refused",
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
		// Refused from localhost is the site's own dev copy: noise. Any other
		// host is someone else carrying the pixel, and that is said.
		if host != "localhost" {
			s.logger.Infow("Datapunt arrival refused",
				"market", market, "slug", slug, "reason", "origin not the namespace door",
				"door", binding, "host", host, "client", r.RemoteAddr)
		}
		return
	}

	// Everything the hit says, read once into the shape ADR-036 fixed.
	now := time.Now()
	arrival := datapuntArrival(r, market, slug, now)

	// The predicate is the datapunt vocabulary plus the event the pixel side named:
	// ?e=contact_click records datapunt:contact_click. No event is a page view.
	predicate := datapuntPredicate(arrival.Event)

	// The subject is the thing the hit is about — the page — so the attestation
	// reads aloud (CDR-010): "/deep-clean datapunt:page_view at golem.club by
	// datapunt:boutique". The visitor id is an attribute, never the subject.
	subject := arrival.Path

	store, err := s.held.WriteAsPublic(market)
	if err != nil {
		s.logger.Errorw("Datapunt arrival lost: its market is not served",
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
	attrs := datapuntAttrs(arrival)

	// The actor is the datapunt, forced here so the store cannot sign the arrival as
	// whoever it is: the arrival is the datapunt's claim, recorded and untrusted.
	actor := datapuntPrefix + slug
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, "_", store.AttestationExists)
	if err != nil {
		s.logger.Errorw("Datapunt arrival lost: no ASID for it",
			"market", market, "slug", slug, "subject", subject, "error", err)
		return
	}
	as := &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{context},
		Actors:     []string{actor},
		Source:     datapuntSource,
		Timestamp:  now,
		Attributes: attrs,
		CreatedAt:  now,
	}
	if err := store.CreateAttestation(as); err != nil {
		s.logger.Errorw("Datapunt arrival lost: the store did not take it",
			"market", market, "slug", slug, "subject", subject, "error", err)
		return
	}
	// Sentry gets the counter, sliced by datapunt and by event (ADR-035). The event
	// is caller-controlled, so it is bounded first: past a cap of distinct events
	// per datapunt, the rest fold to "other". The page stays out — unbounded, in the
	// element's fold instead.
	measure.Count(measure.DatapuntArrivals, 1,
		measure.String(measure.AttrDatapunt, key),
		measure.String(measure.AttrEvent, s.datapuntEventDim(key, predicate)))
	// The arrival is the attestation above and the count; the line is for
	// whoever is at the machine.
	s.logger.Debugw("Datapunt arrival recorded",
		"market", market, "slug", slug, "subject", subject, "predicate", predicate)
}

// datapuntArrival reads a hit into the shape ADR-036 fixed. It is the one place
// the node decides what a request said, so what a datapunt can know is one struct
// to read rather than a handler to trace.
//
// The empty fields are the honest ones. Visit, the referrer split, the campaign
// five, browser, operating system, device, screen, geo and language have no
// source on this request yet; they are absent here, not hidden.
func datapuntArrival(r *http.Request, market, slug string, at time.Time) *protocol.Arrival {
	q := r.URL.Query()

	event := datapuntEventName(q.Get(datapuntEvent))
	if event == "" {
		// Google calls a bare hit page_view, so a bare hit here is page_view.
		// Borrowing the industry's word is not the node inventing one.
		event = datapuntView
	}

	domain, path := datapuntReferrer(q.Get(datapuntRef))

	return &protocol.Arrival{
		At:      at.Format(time.RFC3339Nano),
		Market:  market,
		Slug:    slug,
		Visitor: datapuntID(q.Get(datapuntVisitor)),
		Visit:   datapuntID(q.Get(datapuntVisit)),
		Path:    datapuntPagePath(q.Get(datapuntPage)),
		Event:   event,

		ReferrerDomain: domain,
		ReferrerPath:   path,

		UtmSource:   datapuntBounded(q.Get(datapuntCampaign[0])),
		UtmMedium:   datapuntBounded(q.Get(datapuntCampaign[1])),
		UtmCampaign: datapuntBounded(q.Get(datapuntCampaign[2])),
		UtmContent:  datapuntBounded(q.Get(datapuntCampaign[3])),
		UtmTerm:     datapuntBounded(q.Get(datapuntCampaign[4])),

		Params: datapuntAttributes(q),
	}
}

// datapuntBounded is a stranger's value, kept only if it fits. Over the bound it
// is nothing rather than a truncated something: half an id identifies nobody,
// and half a campaign name is a campaign that was never run.
func datapuntBounded(v string) string {
	if len(v) > maxDatapuntAttributeValue {
		return ""
	}
	return v
}

// datapuntShared is what a site sends when it wanted an id and had none. A
// browser refusing storage cannot mint one, and the site that ships a fallback
// ships the same fallback to everybody.
var datapuntShared = []string{
	"anon",
	"anonymous",
	"none",
	"null",
	"undefined",
	"unknown",
	"0",
	"-",
}

// datapuntID is an id the node is willing to believe identifies one browser.
//
// A shared sentinel is refused rather than stored: every arrival carrying it
// folds into one visitor who never leaves and walks for months, and the node
// cannot tell that from a real person afterwards. Empty is honest; "anon" is a
// crowd wearing a name.
func datapuntID(v string) string {
	v = datapuntBounded(v)
	if v == "" {
		return ""
	}
	if slices.Contains(datapuntShared, strings.ToLower(v)) {
		return ""
	}
	// An id that cannot tell two browsers apart is not one. Anything a site
	// mints for this — a UUID, a hash, a random string — clears this easily.
	if len(v) < minDatapuntID {
		return ""
	}
	return v
}

// datapuntReferrer splits what the site says sent this person here. It is not the
// Referer header — on a pixel that names the page the img sits in, which is the
// site's own page and already the arrival's context.
func datapuntReferrer(raw string) (string, string) {
	if raw == "" || len(raw) > maxDatapuntAttributeValue {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Hostname()), u.Path
}

// datapuntAttrs is what the record keeps beyond its subject, predicate, context
// and actor. Empty means nil: a field the hit did not carry is absent from the
// attestation rather than present and blank.
func datapuntAttrs(a *protocol.Arrival) map[string]any {
	out := make(map[string]any, len(a.Params)+8)
	for k, v := range a.Params {
		out[k] = v
	}
	// The slug rides on every arrival (reserved key) so activity can attribute
	// one back to the datapunt that recorded it.
	out[datapuntSlugAttr] = a.Slug

	keep := func(key, value string) {
		if value != "" {
			out[key] = value
		}
	}
	keep(datapuntVisitor, a.Visitor)
	keep(datapuntVisit, a.Visit)
	keep(datapuntRefDomain, a.ReferrerDomain)
	keep(datapuntRefPath, a.ReferrerPath)
	keep(datapuntCampaign[0], a.UtmSource)
	keep(datapuntCampaign[1], a.UtmMedium)
	keep(datapuntCampaign[2], a.UtmCampaign)
	keep(datapuntCampaign[3], a.UtmContent)
	keep(datapuntCampaign[4], a.UtmTerm)
	return out
}

// datapuntPredicate is the datapunt vocabulary plus the event, which datapuntArrival
// has already sanitised and defaulted. A datapunt writes nothing outside it.
func datapuntPredicate(event string) string {
	return datapuntPrefix + event
}

// datapuntEventName is the event the pixel side named, sanitised to the vocabulary
// tail: letters, digits and -_. only (so page_view, contact_click pass), capped.
// Empty when unusable.
// datapuntReservedEvents are the events no snippet may name: what datapunt itself
// says of a kind, and what a definition says of a datapunt.
var datapuntReservedEvents = []string{"observed", "refused", "wanted", "created", "deleted"}

func datapuntEventName(local string) string {
	if local == "" || len(local) > maxDatapuntSubject {
		return ""
	}
	// The datapunt plugin's own statements and a datapunt's definition share
	// the vocabulary, so a snippet may not name them (datapunt's #Event).
	if slices.Contains(datapuntReservedEvents, local) {
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

// datapuntPagePath is the page the hit is about — the arrival's subject (CDR-010). It is
// the caller's page path, trimmed, control-stripped and capped; empty falls back
// to "/". A stranger controls it, so it is bounded, not trusted.
func datapuntPagePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxDatapuntSubject {
		raw = raw[:maxDatapuntSubject]
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

// datapuntList reports whether a datapunt datapunts. The definition is a system
// attestation; the latest of the datapunt's created and deleted lines is the whole
// truth. A create that nothing has deleted since is live.
func (s *QNTXServer) datapuntList(market, slug string) bool {
	if !datapuntMarket(market) {
		return false
	}
	sys, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return false
	}
	key := datapuntKey(market, slug)
	// Query by the key alone and settle create against delete here: a store's
	// filter may AND several predicates rather than OR them, so asking for both
	// at once can match neither.
	found, err := sys.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{key}, Limit: 200})
	if err != nil {
		return false
	}
	latest := latestDefinition(found)
	return latest != nil && slices.Contains(latest.Predicates, datapuntCreated)
}

// namespaceDoorBinding is the write-origin a datapunt inherits: the hosts of its
// namespace's front door (ADR-032). A namespace with no door binds nothing, so
// the datapunt is open. The door's origins are full URLs; the binding is their
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
		if !slices.Contains(as.Predicates, datapuntCreated) && !slices.Contains(as.Predicates, datapuntDeleted) {
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

// originAllowed reports whether an arrival from host may write to a datapunt bound
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

// seenOf is each name and when it was seen, the most recently seen first, the
// first limit of them. Last seen and not most seen: a name that has not been
// seen since last spring does not lead a list because it was seen often then.
func seenOf(m map[string][]time.Time, limit int) []*protocol.DatapuntSeen {
	type named struct {
		name string
		at   []time.Time
		last time.Time
	}
	all := make([]named, 0, len(m))
	for name, at := range m {
		n := named{name: name, at: at}
		for _, t := range at {
			if t.After(n.last) {
				n.last = t
			}
		}
		all = append(all, n)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].last.Equal(all[j].last) {
			return all[i].last.After(all[j].last)
		}
		return all[i].name < all[j].name
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	out := make([]*protocol.DatapuntSeen, 0, len(all))
	for _, n := range all {
		seen := make([]float64, 0, len(n.at))
		for _, t := range n.at {
			seen = append(seen, float64(t.UnixMilli()))
		}
		out = append(out, &protocol.DatapuntSeen{Name: n.name, Seen: seen})
	}
	return out
}

// datapuntStep is one arrival read as a step, as it is read: what is answered is
// protocol.DatapuntStep.
//
// when is the time itself and never leaves; At is what the view reads. Sorting
// the formatted string instead looks right and is not: Format writes whatever
// offset the store's time carries, so a tree of Z and +02:00 stamps orders by
// the digits of the hour and puts the walk in an order nobody walked.
type datapuntStep struct {
	At    string
	Page  string
	Event string

	when time.Time
}

// walksOf turns the per-visitor steps into walks, most recently seen first, so
// whoever was here last is read first. Every walk, every step: what a datapunt
// recorded is what the view is given.
func walksOf(m map[string][]datapuntStep) []*protocol.DatapuntWalk {
	type walk struct {
		who   string
		steps []datapuntStep
	}
	walks := make([]walk, 0, len(m))
	for who, steps := range m {
		walks = append(walks, walk{who: who, steps: steps})
	}
	sort.Slice(walks, func(i, j int) bool {
		a, b := walks[i].steps, walks[j].steps
		if len(a) == 0 || len(b) == 0 {
			return len(a) > len(b)
		}
		last, other := a[len(a)-1].when, b[len(b)-1].when
		if !last.Equal(other) {
			return last.After(other)
		}
		return walks[i].who < walks[j].who
	})
	out := make([]*protocol.DatapuntWalk, 0, len(walks))
	for _, w := range walks {
		steps := make([]*protocol.DatapuntStep, 0, len(w.steps))
		for _, step := range w.steps {
			steps = append(steps, &protocol.DatapuntStep{At: step.At, Page: step.Page, Event: step.Event})
		}
		out = append(out, &protocol.DatapuntWalk{Who: w.who, Steps: steps})
	}
	return out
}

// topCounts turns a count map into a list, most first, ties by name, capped.
func topCounts(m map[string]int, limit int) []*protocol.DatapuntCount {
	out := make([]*protocol.DatapuntCount, 0, len(m))
	for name, c := range m {
		out = append(out, &protocol.DatapuntCount{Name: name, Count: uint32(c)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetCount() != out[j].GetCount() {
			return out[i].GetCount() > out[j].GetCount()
		}
		return out[i].GetName() < out[j].GetName()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// The functions below answer the datapunts signum (datapunt_sigils.go). Each is
// handed who is asking and what they sent, and gives back the answer or a
// refusal (ADR-039). What is required and what a param takes is the sigil's to
// refuse, before any of these is asked.

// datapuntsList is every datapunt across all markets, with what arrived at each.
func (s *QNTXServer) datapuntsList(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	// The window the activity covers, in AX's own words. Naming none is a
	// datapunt's whole life, which is what this answered before it could be asked.
	since, until, refusal := datapuntRange(sent)
	if refusal != nil {
		return nil, refusal
	}
	live, err := s.liveDatapunts(since, until)
	if err != nil {
		s.logger.Errorw("could not list the datapunts", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "cannot list the datapunts: " + err.Error()}
	}
	return &protocol.Datapunts{Datapunts: live}, nil
}

// datapuntsCreate writes the defining attestation for a new datapunt into system.
// The namespace it feeds is named and is never system or default. The
// definition carries no attributes: the write-origin is the namespace's door.
func (s *QNTXServer) datapuntsCreate(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	market, slug := sent["market"], sent["slug"]
	if !datapuntMarket(market) {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "market",
			Says: "a datapunt namespace is never system or default"}
	}
	if strings.Contains(slug, "/") {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "slug",
			Says: "a datapunt needs a slug with no slash"}
	}
	if err := s.writeDatapuntDef(ctx, market, slug, datapuntCreated, nil); err != nil {
		s.logger.Errorw("could not create a datapunt", "market", market, "slug", slug, "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "could not create the datapunt in " + market + ": " + err.Error()}
	}
	return &protocol.DatapuntCreated{Slug: slug, Url: datapuntPathPrefix + market + "/" + slug}, nil
}

// datapuntsTakeDown supersedes a datapunt with a deleted line, so its pixel stops
// recording. Both lines stay (ADR-026).
func (s *QNTXServer) datapuntsTakeDown(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	market, slug := sent["market"], sent["slug"]
	if !datapuntMarket(market) {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "market",
			Says: "a datapunt market is never system or default"}
	}
	if err := s.writeDatapuntDef(ctx, market, slug, datapuntDeleted, nil); err != nil {
		s.logger.Errorw("could not remove a datapunt", "market", market, "slug", slug, "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "could not remove the datapunt in " + market + ": " + err.Error()}
	}
	return &protocol.DatapuntTakenDown{Slug: slug, Status: "removed"}, nil
}

// writeDatapuntDef writes a created or deleted line for a datapunt into system,
// attributed to the identity that asked. The subject is the datapunt's key, so
// /d/{market}/{slug} resolves to it.
//
// A definition is trusted config rather than an arrival, so it is one of the
// lines the node keeps about itself and lands in system, never in the market
// its arrivals go to.
func (s *QNTXServer) writeDatapuntDef(ctx context.Context, market, slug, predicate string, attrs map[string]any) error {
	sys, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return err
	}
	actor := "root"
	if admitted, ok := auth.AdmissionFrom(ctx); ok && admitted.Identity != "" {
		actor = admitted.Identity
	}
	key := datapuntKey(market, slug)
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
		Source:     datapuntSource,
		Timestamp:  now,
		Attributes: attrs,
		CreatedAt:  now,
	})
}

// liveDatapunts is every datapunt that stands now, across all markets. The
// definitions are read from system, the latest line per datapunt decides, and a
// datapunt whose latest is a delete is left out. Each live datapunt is then filled
// with the activity read from the market it feeds.
func (s *QNTXServer) liveDatapunts(since, until types.TimeBound) ([]*protocol.Datapunt, error) {
	sys, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, err
	}

	// Two single-predicate queries rather than one naming both, because a store
	// may AND the predicates of a filter rather than OR them.
	created, err := sys.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{datapuntCreated}, Limit: 1000})
	if err != nil {
		return nil, err
	}
	deleted, err := sys.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{datapuntDeleted}, Limit: 1000})
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

	// Activity is read from each market once, not once per datapunt.
	activity := map[string]map[string]*datapuntTally{}

	var live []*protocol.Datapunt
	for key, as := range latest {
		if !slices.Contains(as.Predicates, datapuntCreated) {
			continue
		}
		market, slug, ok := strings.Cut(key, "/")
		if !ok || !datapuntMarket(market) {
			continue
		}
		creator := ""
		if len(as.Actors) > 0 {
			creator = as.Actors[0]
		}
		info := &protocol.Datapunt{
			Slug:    slug,
			Market:  market,
			Url:     datapuntPathPrefix + market + "/" + slug,
			Origin:  s.namespaceDoorBinding(market),
			Creator: creator,
			DefId:   as.ID,
			Created: as.Timestamp.Format(time.RFC3339),
			Sites:   []string{},
			Dropped: uint32(s.datapuntDropCount(key)),
			Events:  []*protocol.DatapuntSeen{},
			Pages:   []*protocol.DatapuntSeen{},
			Walks:   []*protocol.DatapuntWalk{},
		}
		if _, done := activity[market]; !done {
			activity[market] = s.datapuntActivity(market, since, until)
		}
		if t, seen := activity[market][slug]; seen {
			info.Arrivals = uint32(t.count)
			info.Visitors = uint32(len(t.visitors))
			info.Sites = t.sites()
			info.Events = seenOf(t.events, 20)
			info.Pages = seenOf(t.pages, 10)
			info.Walks = walksOf(t.walks)
			if !t.last.IsZero() {
				info.LastSeen = t.last.Format(time.RFC3339)
			}
		}
		live = append(live, info)
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].GetMarket() != live[j].GetMarket() {
			return live[i].GetMarket() < live[j].GetMarket()
		}
		return live[i].GetSlug() < live[j].GetSlug()
	})
	return live, nil
}

// datapuntDimension is what a breakdown groups by. Umami calls this `type` and
// its list is its own column names; ours are an arrival's fields (ADR-036), so
// nothing translates between what a datapunt records and what may be asked of it.
const (
	dimPage     = "page"
	dimEvent    = "event"
	dimSite     = "site"
	dimReferrer = "referrer"
	dimVisitor  = "visitor"
	dimVisit    = "visit"
)

// datapuntDimensionOf reads one arrival's value for a dimension, or the empty
// string when that arrival does not carry it. An arrival missing the dimension
// is not a row: it is a hit that did not answer this question.
func datapuntDimensionOf(as *types.As, dim string) string {
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
		return attrString(as.Attributes, datapuntRefDomain)
	case dimVisitor:
		return attrString(as.Attributes, datapuntVisitor)
	case dimVisit:
		return attrString(as.Attributes, datapuntVisit)
	}
	// The campaign five answer under their own names, the ones every tool uses.
	if slices.Contains(datapuntCampaign, dim) {
		return attrString(as.Attributes, dim)
	}
	return ""
}

// datapuntKnownDimension reports whether a dimension is one a datapunt can answer.
// An unknown one is refused rather than answered with nothing, so a caller's
// typo does not read as a datapunt that saw no traffic.
func datapuntKnownDimension(dim string) bool {
	return slices.Contains(datapuntDimensions(), dim)
}

// datapuntDimensions is every dimension a datapunt answers by. The one list: what a
// breakdown accepts and what its sigil says it takes are both read from here.
func datapuntDimensions() []string {
	return append([]string{dimPage, dimEvent, dimSite, dimReferrer, dimVisitor, dimVisit}, datapuntCampaign...)
}

// datapuntRange is the window a read covers, in AX's own words: since and until
// take `yesterday`, `last monday` or an ISO stamp alike (ADR-036). Absent is
// unbounded, which is what a datapunt's whole life is.
func datapuntRange(sent sigil.Sent) (types.TimeBound, types.TimeBound, *protocol.Refusal) {
	since, until := types.Unbounded(), types.Unbounded()
	if v := sent["since"]; v != "" {
		t, err := parser.ParseTemporalExpression(v)
		if err != nil {
			return types.TimeBound{}, types.TimeBound{}, &protocol.Refusal{Why: sigil.Invalid, Param: "since",
				Says: errors.Wrapf(err, "since %q is not a time", v).Error()}
		}
		since = types.BoundAt(*t)
	}
	if v := sent["until"]; v != "" {
		t, err := parser.ParseTemporalExpression(v)
		if err != nil {
			return types.TimeBound{}, types.TimeBound{}, &protocol.Refusal{Why: sigil.Invalid, Param: "until",
				Says: errors.Wrapf(err, "until %q is not a time", v).Error()}
		}
		until = types.BoundAt(*t)
	}
	return since, until, nil
}

// datapuntsMetrics is one datapunt's arrivals grouped by one dimension, most first.
// One sigil taking a dimension rather than a sigil per question. That market,
// slug and type were sent, and that type is one a datapunt answers by, is the
// sigil's to refuse before this is asked.
func (s *QNTXServer) datapuntsMetrics(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	market, slug, dim := sent["market"], sent["slug"], sent["type"]
	since, until, refusal := datapuntRange(sent)
	if refusal != nil {
		return nil, refusal
	}
	// That limit is a count is the sigil's to refuse (sigil.Count), so what
	// arrives here reads.
	limit := datapuntCountLimit
	if v := sent["limit"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}

	arrivals, refusal := s.datapuntArrivals("a breakdown", market, slug, since, until)
	if refusal != nil {
		return nil, refusal
	}

	// A campaign is counted by the pages it brought, as Umami counts it
	// (getUTM: eventType pageView): a click on the page it landed on is not
	// another arrival from the campaign.
	viewsOnly := slices.Contains(datapuntCampaign, dim)

	counts := map[string]int{}
	for _, as := range arrivals {
		if attrString(as.Attributes, datapuntSlugAttr) != slug {
			continue
		}
		if viewsOnly && datapuntDimensionOf(as, dimEvent) != datapuntPredicate(datapuntView) {
			continue
		}
		if v := datapuntDimensionOf(as, dim); v != "" {
			counts[v]++
		}
	}
	return &protocol.DatapuntMetrics{Market: market, Slug: slug, Type: string(dim), Counts: topCounts(counts, limit)}, nil
}

// datapuntArrivals is what arrived in one market inside a window, read for the
// three sigils that read one datapunt. A market the node does not serve is not
// found, and a store that does not answer is the node's own failure: what went
// wrong goes to the log with what was being read, and the caller is told only
// that it did.
func (s *QNTXServer) datapuntArrivals(reading, market, slug string, since, until types.TimeBound) ([]*types.As, *protocol.Refusal) {
	store, err := s.held.Read(market)
	if err != nil {
		s.logger.Errorw("could not open a market for "+reading,
			"market", market, "slug", slug, "error", err)
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "market",
			Says: "no market " + market + " is served here"}
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{
		Source:    datapuntSource,
		TimeStart: since,
		TimeEnd:   until,
		Limit:     maxDatapuntRead,
	})
	if err != nil {
		s.logger.Errorw("could not read arrivals for "+reading,
			"market", market, "slug", slug, "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the store did not answer: " + err.Error()}
	}
	return arrivals, nil
}

// datapuntVisits derives one sitting per visit id. Nothing writes a Visit — the
// arrivals stay the record and this is a projection over them, the same way
// Plausible maintains sessions as a view over its event table (ADR-036).
//
// An arrival carrying no visit id belongs to no visit. Falling back to the
// visitor would turn a person's whole life into one sitting and call it a
// measurement, which is worse than answering with nothing.
func datapuntVisits(arrivals []*types.As, market, slug string) []*protocol.Visit {
	grouped := map[string][]*types.As{}
	for _, as := range arrivals {
		if attrString(as.Attributes, datapuntSlugAttr) != slug {
			continue
		}
		if id := attrString(as.Attributes, datapuntVisit); id != "" {
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
			if len(as.Predicates) > 0 && as.Predicates[0] == datapuntPrefix+datapuntView {
				views++
			}
		}
		sittings = append(sittings, sitting{began: first.Timestamp, visit: &protocol.Visit{
			Visit:           id,
			Visitor:         attrString(first.Attributes, datapuntVisitor),
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

// walkKey is what one walk is drawn per. The visit when the arrival carries
// one, the visitor when it does not, and nothing when it carries neither.
//
// Falling back is the honest reading of an old arrival: it was recorded before
// anything sent a visit id, and one line for that browser is all the record
// supports. Every arrival from a snippet that sends `visit` splits properly.
func walkKey(as *types.As, visitor string) string {
	if visit := attrString(as.Attributes, datapuntVisit); visit != "" {
		return visit
	}
	return visitor
}

// subjectOf is the page an arrival is about, or empty when it names none.
func subjectOf(as *types.As) string {
	if len(as.Subjects) > 0 {
		return as.Subjects[0]
	}
	return ""
}

// datapuntActivityCap is how many rows one activity read answers with. Umami's
// session activity stops at 500 and so does this.
const datapuntActivityCap = 500

// datapuntsActivity is what happened, in order, as rows. Naming a visit is one
// sitting; naming a visitor is that person; naming neither is the datapunt.
func (s *QNTXServer) datapuntsActivity(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	market, slug := sent["market"], sent["slug"]
	since, until, refusal := datapuntRange(sent)
	if refusal != nil {
		return nil, refusal
	}
	wantVisit, wantVisitor := sent["visit"], sent["visitor"]

	arrivals, refusal := s.datapuntArrivals("activity", market, slug, since, until)
	if refusal != nil {
		return nil, refusal
	}

	kept := make([]*types.As, 0, len(arrivals))
	for _, as := range arrivals {
		if attrString(as.Attributes, datapuntSlugAttr) != slug {
			continue
		}
		if wantVisit != "" && attrString(as.Attributes, datapuntVisit) != wantVisit {
			continue
		}
		if wantVisitor != "" && attrString(as.Attributes, datapuntVisitor) != wantVisitor {
			continue
		}
		kept = append(kept, as)
	}
	// Newest first, on the instant rather than the stamp, the way Umami orders
	// a session's activity.
	sort.Slice(kept, func(i, j int) bool { return kept[j].Timestamp.Before(kept[i].Timestamp) })
	if len(kept) > datapuntActivityCap {
		kept = kept[:datapuntActivityCap]
	}

	rows := make([]*protocol.DatapuntActivityRow, 0, len(kept))
	for _, as := range kept {
		event := ""
		if len(as.Predicates) > 0 {
			event = as.Predicates[0]
		}
		rows = append(rows, &protocol.DatapuntActivityRow{
			At:             as.Timestamp.Format(time.RFC3339),
			Visit:          attrString(as.Attributes, datapuntVisit),
			Visitor:        attrString(as.Attributes, datapuntVisitor),
			Path:           subjectOf(as),
			ReferrerDomain: attrString(as.Attributes, datapuntRefDomain),
			Event:          event,
		})
	}
	return &protocol.DatapuntActivity{Market: market, Slug: slug, Activity: rows}, nil
}

// datapuntsVisits is one datapunt's sittings, derived. Entry, exit, duration and
// bounce are computed here every time and stored nowhere, because a visit is a
// projection (ADR-036).
func (s *QNTXServer) datapuntsVisits(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	market, slug := sent["market"], sent["slug"]
	since, until, refusal := datapuntRange(sent)
	if refusal != nil {
		return nil, refusal
	}
	arrivals, refusal := s.datapuntArrivals("visits", market, slug, since, until)
	if refusal != nil {
		return nil, refusal
	}
	return &protocol.DatapuntVisits{Market: market, Slug: slug, Visits: datapuntVisits(arrivals, market, slug)}, nil
}

// datapuntTally is one datapunt's arrivals folded down: how many, when the last one
// landed, and the distinct sites they came from.
//
// walks is the fold that keeps its shape. A datapunt datapunts and a person walks
// past it, so the counts say how much arrived and the walks say what happened.
// Every other field here answers "how many"; this one answers "in what order".
type datapuntTally struct {
	count    int
	last     time.Time
	hosts    map[string]struct{}
	events   map[string][]time.Time
	pages    map[string][]time.Time
	visitors map[string]struct{}
	walks    map[string][]datapuntStep
}

// sites is the distinct hosts arrivals came from, sorted. A datapunt bound to a
// door still shows here what actually reported, so the door and the reality are
// both visible.
func (t *datapuntTally) sites() []string {
	out := make([]string, 0, len(t.hosts))
	for h := range t.hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// datapuntActivity folds a market's arrivals down per slug. Arrivals carry the
// slug on a reserved attribute (datapuntSlugAttr) and their source is the datapunt;
// definitions live in system, so nothing here is a definition. The context is
// the page URL, so its host is the site that reported.
func (s *QNTXServer) datapuntActivity(market string, since, until types.TimeBound) map[string]*datapuntTally {
	tally := map[string]*datapuntTally{}
	store, err := s.held.Read(market)
	if err != nil {
		s.logger.Errorw("could not open a market for activity", "market", market, "error", err)
		return tally
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{
		Source:    datapuntSource,
		TimeStart: since,
		TimeEnd:   until,
		Limit:     maxDatapuntRead,
	})
	if err != nil {
		s.logger.Errorw("could not read datapunt arrivals for activity", "market", market, "error", err)
		return tally
	}
	for _, as := range arrivals {
		slug := attrString(as.Attributes, datapuntSlugAttr)
		if slug == "" {
			continue
		}
		t, seen := tally[slug]
		if !seen {
			t = &datapuntTally{hosts: map[string]struct{}{}, events: map[string][]time.Time{}, pages: map[string][]time.Time{}, visitors: map[string]struct{}{}, walks: map[string][]datapuntStep{}}
			tally[slug] = t
		}
		t.count++
		if len(as.Predicates) > 0 {
			t.events[as.Predicates[0]] = append(t.events[as.Predicates[0]], as.Timestamp)
		}
		if len(as.Subjects) > 0 {
			t.pages[as.Subjects[0]] = append(t.pages[as.Subjects[0]], as.Timestamp)
		}
		visitor := attrString(as.Attributes, datapuntVisitor)
		if visitor != "" {
			t.visitors[visitor] = struct{}{}
		}
		// A walk is one sitting, so it is keyed by the visit when the arrival
		// carries one. Keyed by the visitor it is a lifetime: somebody who came
		// back six times over two days drew one line twenty-five hours long.
		if key := walkKey(as, visitor); key != "" {
			step := datapuntStep{At: as.Timestamp.Format(time.RFC3339), when: as.Timestamp}
			if len(as.Subjects) > 0 {
				step.Page = as.Subjects[0]
			}
			if len(as.Predicates) > 0 {
				step.Event = as.Predicates[0]
			}
			t.walks[key] = append(t.walks[key], step)
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
func orderSteps(steps []datapuntStep) {
	sort.Slice(steps, func(i, j int) bool { return steps[i].when.Before(steps[j].when) })
}

// datapuntAttributes is what survives of the query string: every parameter but the
// event, the page, the visitor id and the reserved slug key, capped in count and
// size. Arrivals past the cap lose their tail rather than the whole arrival,
// which is the fact being recorded.
// datapuntReserved reports whether a query key is the datapunt's own. Reserved keys
// have a field on the arrival, so keeping them in params too would record the
// same fact twice under two names.
func datapuntReserved(key string) bool {
	switch key {
	case datapuntEvent, datapuntPage, datapuntVisitor, datapuntVisit, datapuntRef, datapuntSlugAttr:
		return true
	}
	return slices.Contains(datapuntCampaign, key)
}

func datapuntAttributes(params map[string][]string) map[string]string {
	out := make(map[string]string)
	for key, values := range params {
		if datapuntReserved(key) || len(values) == 0 {
			continue
		}
		if len(out) >= maxDatapuntAttributes {
			break
		}
		if len(key) > maxDatapuntAttributeKey || len(values[0]) > maxDatapuntAttributeValue {
			continue
		}
		out[key] = values[0]
	}
	return out
}
