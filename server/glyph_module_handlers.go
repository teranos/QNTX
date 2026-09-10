package server

// Glyph modules — the code a glyph is, published as an attestation and served
// as a browser module.
//
// A page imports what it renders, and an import is governed by script-src,
// which names origins and not routes. So this is a root of its own rather than
// a path under /api: the edge can put /g/ on the node while the rest of the
// page stays where it is, and the module is then same-origin with the page.

import (
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// glyphPathPrefix is the root every glyph module is served under.
const glyphPathPrefix = "/g/"

// GlyphSubjectPrefix is what a glyph module attestation is about, joined to
// the glyph's name. The shape /api/glyph-config already uses for glyph config.
const GlyphSubjectPrefix = "glyph-"

// GlyphModulePredicate is the claim: this attestation carries the module.
// A subject under the prefix with any other predicate is not served, which is
// where a glyph that is written but not yet public lives.
const GlyphModulePredicate = "module"

// GlyphSourceAttribute is the attribute the module's text is under.
const GlyphSourceAttribute = "source"

// glyphModuleContentType is what the import is refused without. Served as
// anything else it fails in the browser as a blocked import, which reads as a
// missing glyph rather than as a wrong header.
const glyphModuleContentType = "text/javascript; charset=utf-8"

// glyphModuleSuffix is what a name is asked for as, so the browser and the
// edge both see a JavaScript file rather than a bare word.
const glyphModuleSuffix = ".js"

// glyphIndexLimit bounds the listing. A node with more published glyphs than
// this has a different problem than a truncated list.
const glyphIndexLimit = 500

// StatusNoModulePublished is what the node answers when it looked and found
// nothing. Not 404: an edge may rewrite that one, and CloudFront's rewritable
// set is 400, 403, 404, 405, 414, 416 and the 5xx range — 410 is in neither,
// so it cannot be swallowed even by a rule somebody adds later.
//
// 404 also declines to say which of "no such thing", "not yours", and "wrong
// URL" happened. The node queried one subject and one predicate and got no
// rows, so it knows, and saying so is worth more than the convention.
const StatusNoModulePublished = http.StatusGone

// PublishedGlyph is one glyph the node serves, as the index reports it.
type PublishedGlyph struct {
	Name string `json:"name"`
	// As is the attestation standing now, and so the version: a page puts it in
	// the import URL, and a published module is a URL it has not imported.
	As        string    `json:"as"`
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
	By        []string  `json:"by,omitempty"`
	Signer    string    `json:"signer,omitempty"`
}

// HandleGlyphModule answers the index at /g/ and one module at /g/{name}.js.
func (s *QNTXServer) HandleGlyphModule(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	rest, under := strings.CutPrefix(r.URL.Path, glyphPathPrefix)
	if !under {
		writeError(w, http.StatusNotFound, "not a glyph path: "+r.URL.Path)
		return
	}

	if rest == "" {
		s.handleGlyphIndex(w, r)
		return
	}
	s.handleOneGlyphModule(w, r, rest)
}

// glyphStore is the store this reader's glyphs come from, or nil once the
// refusal has been written.
//
// A namespace is its own universe (ADR-026), and its glyphs are part of what
// it is: someone arriving through a door gets that door's canvas, not another
// one's. A reader with no session has named no door, and storeFor answers with
// the served store for them.
func (s *QNTXServer) glyphStore(w http.ResponseWriter, r *http.Request) ats.AttestationStore {
	store, err := s.storeFor(r)
	if err != nil {
		writeWrappedError(w, s.logger, err,
			"failed to resolve the namespace this reader's glyphs come from",
			http.StatusInternalServerError)
		return nil
	}
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "attestation store not available")
		return nil
	}
	return store
}

// handleGlyphIndex lists what is published, which is how a page learns which
// glyphs exist without being told by configuration.
func (s *QNTXServer) handleGlyphIndex(w http.ResponseWriter, r *http.Request) {
	store := s.glyphStore(w, r)
	if store == nil {
		return
	}

	held, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{GlyphModulePredicate},
		Limit:      glyphIndexLimit,
	})
	if err != nil {
		writeWrappedError(w, s.logger, err,
			"failed to list published glyph modules", http.StatusInternalServerError)
		return
	}

	// Storage orders timestamp DESC, so the first row for a subject is the one
	// standing. Anything after it is what that glyph used to be.
	published := make([]PublishedGlyph, 0, len(held))
	seen := make(map[string]bool, len(held))
	for _, as := range held {
		name, is := glyphName(as)
		if !is || seen[name] {
			continue
		}
		if _, written := glyphSource(as); !written {
			continue
		}
		seen[name] = true
		published = append(published, PublishedGlyph{
			Name:      name,
			As:        as.ID,
			URL:       glyphPathPrefix + name + glyphModuleSuffix,
			Published: as.Timestamp,
			By:        as.Actors,
			Signer:    as.SignerDID,
		})
	}

	respond(w, s.logger, http.StatusOK, map[string]any{"glyphs": published})
}

// handleOneGlyphModule serves the module standing under one name.
func (s *QNTXServer) handleOneGlyphModule(w http.ResponseWriter, r *http.Request, rest string) {
	name, isModule := strings.CutSuffix(rest, glyphModuleSuffix)
	if !isModule || name == "" || strings.Contains(name, "/") {
		// Outside the rewritable set for the same reason as above: a path typed
		// wrong should not come back looking like a page that loaded.
		writeError(w, http.StatusUnprocessableEntity,
			"a glyph module is asked for as /g/{name}.js, got "+rest)
		return
	}

	store := s.glyphStore(w, r)
	if store == nil {
		return
	}

	subject := GlyphSubjectPrefix + name
	held, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{subject},
		Predicates: []string{GlyphModulePredicate},
		Limit:      1,
	})
	if err != nil {
		writeWrappedError(w, s.logger, err,
			"failed to read the glyph module published as "+subject, http.StatusInternalServerError)
		return
	}

	if len(held) == 0 {
		// A page reports only that an import failed, so the subject nothing was
		// found under is said here and written down nowhere else.
		s.logger.Infow("No glyph module is published; the page will show a failed import",
			"glyph", name, "subject", subject, "predicate", GlyphModulePredicate)
		writeError(w, StatusNoModulePublished,
			"no module published for "+subject+" with predicate "+GlyphModulePredicate)
		return
	}

	text, written := glyphSource(held[0])
	if !written {
		// An attestation under the subject carrying no source is not a module.
		// Serving its empty body hands the page a module exporting nothing.
		s.logger.Errorw("A glyph module attestation carries no source",
			"glyph", name, "subject", subject, "as", held[0].ID, "attribute", GlyphSourceAttribute)
		writeError(w, http.StatusUnprocessableEntity,
			"the module published as "+subject+" carries no "+GlyphSourceAttribute)
		return
	}

	w.Header().Set("Content-Type", glyphModuleContentType)

	// The id is in the URL a page imports with, so what it holds under that URL
	// is this module and stays right until another is published.
	w.Header().Set("ETag", `"`+held[0].ID+`"`)

	// ServeContent writes the body, so no write here can drop a failure, and it
	// answers a conditional request on its own.
	http.ServeContent(w, r, name+glyphModuleSuffix, held[0].Timestamp, strings.NewReader(text))
}

// glyphName is the glyph an attestation is about, and whether it is about one
// at all. A row carrying the predicate under some other subject is not a glyph.
func glyphName(as *types.As) (string, bool) {
	if as == nil || len(as.Subjects) != 1 {
		return "", false
	}
	name, under := strings.CutPrefix(as.Subjects[0], GlyphSubjectPrefix)
	return name, under && name != ""
}

// glyphSource is the module text an attestation carries.
func glyphSource(as *types.As) (string, bool) {
	if as == nil {
		return "", false
	}
	text, written := as.Attributes[GlyphSourceAttribute].(string)
	return text, written && text != ""
}
