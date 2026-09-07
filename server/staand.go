package server

import (
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

// A staand answers on /s/{namespace}/{slug} (ADR-035): a market's public receive
// point for one predicate. Every call records one arrival and returns a 1×1 GIF,
// so an <img> carries it with no CORS, no preflight, no script.
const staandPathPrefix = "/s/"

// The latest of these two lines for a slug in a market is the whole truth of
// whether a staand stands there. Raising and striking are attestations ROOT
// writes; nothing here mints a token.
const (
	staandRaised = "staand:raised"
	staandStruck = "staand:struck"
	staandSource = "staand"
)

// The ware a staand writes and its label live on the raising attestation.
const (
	staandWares = "writes"
	staandLabel = "label"
)

// An arrival is a stranger's claim, so what it carries is capped rather than
// trusted.
const (
	maxStaandSubject        = 128
	maxStaandAttributes     = 8
	maxStaandAttributeKey   = 32
	maxStaandAttributeValue = 128
)

// A 1×1 transparent GIF. Answering with an image is what lets an <img> carry
// the staand with no CORS, no preflight and no script.
var staandPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// staandMarket reports whether a namespace may hold a staand. Never system or
// default: an arrival is an untrusted public write, and those two namespaces
// hold the node's own records — users, tokens, grants (ADR-026).
func staandMarket(namespace string) bool {
	return namespace != "" && namespace != auth.NamespaceSystem && namespace != auth.NamespaceDefault
}

// HandleStaand answers GET /s/{namespace}/{slug}. The namespace is the market
// and the slug names the staand; the pair resolve to the staand's defining
// attestation, which gives the one predicate it writes.
func (s *QNTXServer) HandleStaand(w http.ResponseWriter, r *http.Request) {
	// The pixel goes out whatever happened: probing a staand teaches nothing.
	defer func() {
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			if _, err := w.Write(staandPixel); err != nil {
				s.logger.Errorw("could not write the staand pixel", "error", err)
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
	namespace, slug, ok := strings.Cut(rest, "/")
	if !ok || namespace == "" || slug == "" || strings.Contains(slug, "/") {
		return
	}
	if !staandMarket(namespace) {
		s.logger.Infow("Staand arrival refused",
			"namespace", namespace, "slug", slug,
			"reason", "a staand market is never system or default")
		return
	}

	ware, label, live := s.staandFor(namespace, slug)
	if !live {
		s.logger.Infow("Staand arrival refused",
			"namespace", namespace, "slug", slug, "reason", "no staand stands here",
			"client", r.RemoteAddr)
		return
	}

	subject := staandSubject(ware, r.URL.Query().Get("subject"))
	if subject == "" {
		s.logger.Infow("Staand arrival refused",
			"namespace", namespace, "slug", slug, "reason", "no usable subject")
		return
	}

	store, err := s.storeIn(namespace)
	if err != nil {
		s.logger.Errorw("Staand arrival lost: its market is not served",
			"namespace", namespace, "slug", slug, "error", err)
		return
	}

	// The context is the page the pixel fired from. It names the city and the
	// page, and the store already says which market (ADR-026, ADR-035).
	context := r.Referer()
	if context == "" {
		context = "_"
	}

	actor := "staand:" + label
	if label == "" {
		actor = "staand:" + slug
	}

	// The ASID is generated here so the record keeps the forced actor: the arrival
	// is a claim by the staand, and letting the store derive the actor would sign
	// it as whoever the store is.
	id, err := identity.GenerateASUIDWithRetry("AS", subject, ware, "_", store.AttestationExists)
	if err != nil {
		s.logger.Errorw("Staand arrival lost: no ASID for it",
			"namespace", namespace, "slug", slug, "subject", subject, "error", err)
		return
	}
	now := time.Now()
	as := &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{ware},
		Contexts:   []string{context},
		Actors:     []string{actor},
		Source:     staandSource,
		Timestamp:  now,
		Attributes: staandAttributes(r.URL.Query()),
		CreatedAt:  now,
	}
	if err := store.CreateAttestation(as); err != nil {
		s.logger.Errorw("Staand arrival lost: the store did not take it",
			"namespace", namespace, "slug", slug, "subject", subject, "error", err)
		return
	}
	s.logger.Infow("Staand arrival recorded",
		"namespace", namespace, "slug", slug, "subject", subject, "ware", ware)
}

// attrString reads a string attribute, and the empty string when it is absent
// or not a string. The assertion's ok is read here, so no caller discards it.
func attrString(attrs map[string]any, key string) string {
	if v, ok := attrs[key].(string); ok {
		return v
	}
	return ""
}

// staandFor resolves a slug in a market to the staand that stands there. The
// latest of the slug's raise and strike lines is the whole truth: a raise that
// nothing has struck since is live, and gives the ware and the label.
func (s *QNTXServer) staandFor(namespace, slug string) (ware, label string, live bool) {
	if !staandMarket(namespace) {
		return "", "", false
	}
	store, err := s.storeIn(namespace)
	if err != nil {
		return "", "", false
	}
	// Query by the slug alone and settle raise against strike here: a store's
	// filter may AND several predicates rather than OR them, so asking for both
	// at once can match neither.
	found, err := store.GetAttestations(ats.AttestationFilter{
		Subjects: []string{slug},
		Limit:    200,
	})
	if err != nil {
		return "", "", false
	}

	var latest *types.As
	for _, as := range found {
		if !slices.Contains(as.Predicates, staandRaised) && !slices.Contains(as.Predicates, staandStruck) {
			continue
		}
		if latest == nil || as.Timestamp.After(latest.Timestamp) {
			latest = as
		}
	}
	if latest == nil || !slices.Contains(latest.Predicates, staandRaised) {
		return "", "", false
	}

	ware = attrString(latest.Attributes, staandWares)
	label = attrString(latest.Attributes, staandLabel)
	if ware == "" {
		return "", "", false
	}
	return ware, label, true
}

// staandInfo is one staand as the market glyph sees it: the slug it answers on,
// the ware it writes, its label, and the URL to place.
type staandInfo struct {
	Slug      string `json:"slug"`
	Predicate string `json:"predicate"`
	Label     string `json:"label"`
	URL       string `json:"url"`
}

// HandleStaands is the market glyph's endpoint, ROOT only (reach table). GET
// lists a market's staands, POST creates one, DELETE takes one down. Every path
// refuses system and default (staandMarket).
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
	market := r.URL.Query().Get("market")
	if !staandMarket(market) {
		writeError(w, http.StatusBadRequest, "name a market that is not system or default")
		return
	}
	live, err := s.liveStaands(market)
	if err != nil {
		s.logger.Errorw("could not list the staands in a market", "market", market, "error", err)
		writeError(w, http.StatusBadRequest, "cannot list the staands in "+market)
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{"staands": live})
}

// createStaand writes the defining attestation for a new staand into a market.
// The market is named in the body and is never system or default.
func (s *QNTXServer) createStaand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Market    string `json:"market"`
		Slug      string `json:"slug"`
		Predicate string `json:"predicate"`
		Label     string `json:"label"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}
	if !staandMarket(req.Market) {
		writeError(w, http.StatusBadRequest, "a staand market is never system or default")
		return
	}
	if req.Slug == "" || req.Predicate == "" {
		writeError(w, http.StatusBadRequest, "a staand needs a slug and a predicate")
		return
	}
	if err := s.writeStaandLine(r, req.Market, req.Slug, staandRaised,
		map[string]any{staandWares: req.Predicate, staandLabel: req.Label}); err != nil {
		s.logger.Errorw("could not create a staand",
			"market", req.Market, "slug", req.Slug, "error", err)
		writeError(w, http.StatusBadRequest, "could not create the staand in "+req.Market)
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{
		"slug": req.Slug, "url": staandPathPrefix + req.Market + "/" + req.Slug,
	})
}

// deleteStaand supersedes a staand with a struck line, so its pixel stops
// recording. Both lines stay (ADR-026).
func (s *QNTXServer) deleteStaand(w http.ResponseWriter, r *http.Request) {
	market := r.URL.Query().Get("market")
	slug := r.URL.Query().Get("slug")
	if !staandMarket(market) {
		writeError(w, http.StatusBadRequest, "a staand market is never system or default")
		return
	}
	if slug == "" {
		writeError(w, http.StatusBadRequest, "name the slug to remove")
		return
	}
	if err := s.writeStaandLine(r, market, slug, staandStruck, nil); err != nil {
		s.logger.Errorw("could not remove a staand", "market", market, "slug", slug, "error", err)
		writeError(w, http.StatusBadRequest, "could not remove the staand in "+market)
		return
	}
	respond(w, s.logger, http.StatusOK, map[string]any{"slug": slug, "status": "removed"})
}

// writeStaandLine writes a raise or strike line for a slug into a market,
// attributed to the ROOT identity that asked.
func (s *QNTXServer) writeStaandLine(r *http.Request, market, slug, predicate string, attrs map[string]any) error {
	store, err := s.storeIn(market)
	if err != nil {
		return err
	}
	actor := "root"
	if admitted, ok := auth.AdmissionFrom(r.Context()); ok && admitted.Identity != "" {
		actor = admitted.Identity
	}
	id, err := identity.GenerateASUIDWithRetry("AS", slug, predicate, "_", store.AttestationExists)
	if err != nil {
		return err
	}
	now := time.Now()
	return store.CreateAttestation(&types.As{
		ID:         id,
		Subjects:   []string{slug},
		Predicates: []string{predicate},
		Contexts:   []string{"_"},
		Actors:     []string{actor},
		Source:     staandSource,
		Timestamp:  now,
		Attributes: attrs,
		CreatedAt:  now,
	})
}

// liveStaands is every staand that stands in a market now: the latest line per
// slug decides, and a slug whose latest is a strike is left out.
func (s *QNTXServer) liveStaands(namespace string) ([]staandInfo, error) {
	store, err := s.storeIn(namespace)
	if err != nil {
		return nil, err
	}

	// Two single-predicate queries rather than one naming both, because a store
	// may AND the predicates of a filter rather than OR them.
	raised, err := store.GetAttestations(ats.AttestationFilter{Predicates: []string{staandRaised}, Limit: 1000})
	if err != nil {
		return nil, err
	}
	struck, err := store.GetAttestations(ats.AttestationFilter{Predicates: []string{staandStruck}, Limit: 1000})
	if err != nil {
		return nil, err
	}

	latest := map[string]*types.As{}
	keep := func(as *types.As) {
		if len(as.Subjects) == 0 {
			return
		}
		slug := as.Subjects[0]
		if cur, seen := latest[slug]; !seen || as.Timestamp.After(cur.Timestamp) {
			latest[slug] = as
		}
	}
	for _, as := range raised {
		keep(as)
	}
	for _, as := range struck {
		keep(as)
	}

	var live []staandInfo
	for slug, as := range latest {
		if !slices.Contains(as.Predicates, staandRaised) {
			continue
		}
		ware := attrString(as.Attributes, staandWares)
		if ware == "" {
			continue
		}
		label := attrString(as.Attributes, staandLabel)
		live = append(live, staandInfo{
			Slug:      slug,
			Predicate: ware,
			Label:     label,
			URL:       staandPathPrefix + namespace + "/" + slug,
		})
	}
	sort.Slice(live, func(i, j int) bool { return live[i].Slug < live[j].Slug })
	return live, nil
}

// staandSubject builds the arrival's subject: the ware's vocabulary, a colon,
// and the local part the caller sent. A `page:seen` staand asked for `VISIT01`
// records `page:VISIT01`, so the caller names the individual within the kind the
// staand fixes. The local part is letters, digits and -_. only.
func staandSubject(ware, local string) string {
	if local == "" || len(local) > maxStaandSubject {
		return ""
	}
	for _, r := range local {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum && r != '-' && r != '_' && r != '.' {
			return ""
		}
	}
	vocabulary := ware
	if head, _, found := strings.Cut(ware, ":"); found {
		vocabulary = head
	}
	return vocabulary + ":" + local
}

// staandAttributes is what survives of the query string: every parameter but
// the subject, capped in count and size. Arrivals past the cap lose their tail
// rather than the whole arrival, which is the fact being recorded.
func staandAttributes(params map[string][]string) map[string]any {
	out := make(map[string]any)
	for key, values := range params {
		if key == "subject" || len(values) == 0 {
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
