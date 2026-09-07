package server

import (
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/server/auth"
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
// it. The default event when the snippet names none is a page view.
const (
	staandPrefix  = "staand:"
	staandEvent   = "e"
	staandPage    = "page"
	staandVisitor = "v"
	staandView    = "page_view"
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
func staandMarket(namespace string) bool {
	return namespace != "" && namespace != auth.NamespaceSystem && namespace != auth.NamespaceDefault
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

	// The predicate is the stand vocabulary plus the event the pixel side named:
	// ?e=contact_click records staand:contact_click. No event is a page view.
	predicate := staandPredicate(r.URL.Query().Get(staandEvent))

	// The subject is the thing the hit is about — the page — so the attestation
	// reads aloud (CDR-010): "/deep-clean staand:page_view at golem.club by
	// staand:boutique". The visitor id is an attribute, never the subject.
	subject := standPage(r.URL.Query().Get(staandPage))

	store, err := s.storeIn(market)
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
	attrs := staandAttributes(r.URL.Query())
	attrs[staandSlugAttr] = slug
	if v := r.URL.Query().Get(staandVisitor); v != "" && len(v) <= maxStaandAttributeValue {
		attrs[staandVisitor] = v
	}

	// The actor is the stand, forced here so the store cannot sign the arrival as
	// whoever it is: the arrival is the stand's claim, recorded and untrusted.
	actor := staandPrefix + slug
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, "_", store.AttestationExists)
	if err != nil {
		s.logger.Errorw("Stand arrival lost: no ASID for it",
			"market", market, "slug", slug, "subject", subject, "error", err)
		return
	}
	now := time.Now()
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
	s.logger.Infow("Stand arrival recorded",
		"market", market, "slug", slug, "subject", subject, "predicate", predicate)
}

// staandPredicate is the stand vocabulary plus the event the pixel side named,
// sanitised. An empty or unusable event is a page view (page_view, Google's own
// name), so a bare <img> still records. The event is letters, digits and -_. only.
func staandPredicate(event string) string {
	clean := staandEventName(event)
	if clean == "" {
		clean = staandView
	}
	return staandPrefix + clean
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
	sys, err := s.storeIn(auth.NamespaceSystem)
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
	Dropped  int           `json:"dropped"`
	LastSeen string        `json:"lastSeen"`
	Events   []staandCount `json:"events"`
	Pages    []staandCount `json:"pages"`
}

// staandCount is one coarse tally the market view shows: a name (an event, or a
// page) and how many times it was attested (ADR-035).
type staandCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
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

func (s *QNTXServer) listStaands(w http.ResponseWriter, _ *http.Request) {
	live, err := s.liveStaands()
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
// attributed to the ROOT identity that asked. The subject is the stand's key,
// so /s/{market}/{slug} resolves to it.
func (s *QNTXServer) writeStaandDef(r *http.Request, market, slug, predicate string, attrs map[string]any) error {
	sys, err := s.storeIn(auth.NamespaceSystem)
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
func (s *QNTXServer) liveStaands() ([]staandInfo, error) {
	sys, err := s.storeIn(auth.NamespaceSystem)
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
		}
		if _, done := activity[market]; !done {
			activity[market] = s.staandActivity(market)
		}
		if t, seen := activity[market][slug]; seen {
			info.Arrivals = t.count
			info.Sites = t.sites()
			info.Events = topCounts(t.events, 20)
			info.Pages = topCounts(t.pages, 10)
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

// staandTally is one stand's arrivals folded down: how many, when the last one
// landed, and the distinct sites they came from.
type staandTally struct {
	count  int
	last   time.Time
	hosts  map[string]struct{}
	events map[string]int
	pages  map[string]int
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
func (s *QNTXServer) staandActivity(market string) map[string]*staandTally {
	tally := map[string]*staandTally{}
	store, err := s.storeIn(market)
	if err != nil {
		s.logger.Errorw("could not open a market for activity", "market", market, "error", err)
		return tally
	}
	arrivals, err := store.GetAttestations(ats.AttestationFilter{Source: staandSource, Limit: 5000})
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
			t = &staandTally{hosts: map[string]struct{}{}, events: map[string]int{}, pages: map[string]int{}}
			tally[slug] = t
		}
		t.count++
		if len(as.Predicates) > 0 {
			t.events[as.Predicates[0]]++
		}
		if len(as.Subjects) > 0 {
			t.pages[as.Subjects[0]]++
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
	return tally
}

// staandAttributes is what survives of the query string: every parameter but the
// event, the page, the visitor id and the reserved slug key, capped in count and
// size. Arrivals past the cap lose their tail rather than the whole arrival,
// which is the fact being recorded.
func staandAttributes(params map[string][]string) map[string]any {
	out := make(map[string]any)
	for key, values := range params {
		if key == staandEvent || key == staandPage || key == staandVisitor || key == staandSlugAttr || len(values) == 0 {
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
