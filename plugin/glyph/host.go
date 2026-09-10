// Package glyph serves a canvas glyph module from the node itself.
//
// A glyph is a browser module. The canvas imports it, calls the render it
// exports, and reads what it is from the glyphDef beside it — the frontend
// probes /api/{name}/glyph-module.js for every name the node lists and takes
// the definition from the module rather than from anything the node says. So
// what a glyph needs from the node is a name and a file, and a process to hold
// them is a cost with nothing on the other side of it.
//
// A Host is that name and that file. It satisfies plugin.DomainPlugin, which
// is what the registry stores and what the router resolves, and it is reached
// through the same /api/{name} route a plugin is, because the router asks the
// interface and never asks how the answer is produced.
package glyph

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// ModuleRoute is the path the canvas imports a glyph module from, beneath the
// glyph's own /api/{name}/. The frontend probes this exact name, so a host that
// answers it is discovered without announcing anything.
const ModuleRoute = "/glyph-module.js"

// ModuleContentType is what the import is refused without. A module served as
// anything else fails in the browser as a blocked import, which reads as a
// missing glyph rather than as a wrong header.
const ModuleContentType = "text/javascript; charset=utf-8"

// Subject is what a glyph module attestation is about: this, joined to the
// glyph's name. The same shape as the glyph config convention in the server.
const Subject = "glyph-"

// Predicate is the claim: this attestation carries the module.
const Predicate = "module"

// SourceAttribute is the attribute the module's text is under.
const SourceAttribute = "source"

// Host serves one glyph module, read from the attestation that carries it.
// plugin.Base is deliberately not embedded — see TestAGlyphIsNotPausable.
type Host struct {
	name   string
	logger *zap.SugaredLogger

	mu    sync.Mutex
	store ats.AttestationStore
}

// New builds a host for one declared glyph. The name is the route it answers
// on, and the glyph its subject names.
func New(name string, logger *zap.SugaredLogger) *Host {
	return &Host{name: name, logger: logger}
}

// subject is what this host's module attestation is about.
func (h *Host) subject() string { return Subject + h.name }

// latest is the module attestation standing now, or nil when none is.
//
// Storage orders timestamp DESC, so one row is the current one: publishing a
// module is writing another attestation, and the newer supersedes.
func (h *Host) latest() *types.As {
	h.mu.Lock()
	store := h.store
	h.mu.Unlock()

	if store == nil {
		return nil
	}

	held, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{h.subject()},
		Predicates: []string{Predicate},
		Limit:      1,
	})
	if err != nil {
		h.logger.Errorw("Glyph module cannot be read from the store",
			"glyph", h.name, "subject", h.subject(), "error", err)
		return nil
	}
	if len(held) == 0 {
		return nil
	}
	return held[0]
}

// source is the module text the attestation carries.
func source(held *types.As) (string, bool) {
	if held == nil {
		return "", false
	}
	text, written := held.Attributes[SourceAttribute].(string)
	return text, written && text != ""
}

// ModuleDigest is what the browser puts in its import URL so that a published
// module is a different module rather than the one it already has.
//
// It is the attestation's own id. Publishing writes a new attestation, so a
// new id is exactly what a new module is; nothing is hashed and nothing is
// cached. A glyph nobody has published has no id, and the empty string is that.
func (h *Host) ModuleDigest() string {
	held := h.latest()
	if held == nil {
		return ""
	}
	return held.ID
}

// Metadata names the glyph. No version field: the version is the id of the
// attestation standing now, and ModuleDigest is where that is asked.
func (h *Host) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        h.name,
		Description: "canvas glyph published as " + h.subject(),
	}
}

// Initialize keeps the store, which is the whole of what a glyph needs from
// the node: the module is read from it per request rather than held.
func (h *Host) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	if services == nil {
		return errors.New("a glyph host is given no services, and the module lives in the store")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = services.ATSStore()
	return nil
}

// Shutdown has nothing to close.
func (h *Host) Shutdown(ctx context.Context) error { return nil }

// RegisterWebSocket registers nothing. A glyph reaches the node through the
// node's own API with the viewer's session, the way any page does.
func (h *Host) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	return map[string]plugin.WebSocketHandler{}, nil
}

// RegisterHTTP mounts the one route a glyph has.
func (h *Host) RegisterHTTP(mux *http.ServeMux) error {
	mux.HandleFunc("GET "+ModuleRoute, func(w http.ResponseWriter, r *http.Request) {
		held := h.latest()
		text, written := source(held)
		if !written {
			// The canvas reports only that an import failed. Without this line
			// the subject it found nothing under is written down nowhere.
			h.logger.Errorw("No glyph module is published; the canvas will show a failed import",
				"glyph", h.name, "subject", h.subject(), "predicate", Predicate)
			http.Error(w, "no module published for "+h.subject(), http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", ModuleContentType)

		// The id is in the URL the browser asked with, so what it holds under
		// that URL is this module and stays right until another is published.
		w.Header().Set("ETag", `"`+held.ID+`"`)

		// ServeContent writes the body, so there is no write here to drop the
		// failure of, and it answers a conditional request on its own.
		http.ServeContent(w, r, "glyph-module.js", held.Timestamp, strings.NewReader(text))
	})
	return nil
}

// Health answers about the module rather than reporting that the host is
// running. The host is always running; whether a module is published, and
// which one, is what anyone asks of a glyph.
func (h *Host) Health(ctx context.Context) plugin.HealthStatus {
	held := h.latest()
	if _, written := source(held); !written {
		return plugin.HealthStatus{
			Healthy: false,
			Message: "no module published for " + h.subject(),
			Details: map[string]interface{}{"subject": h.subject(), "predicate": Predicate},
		}
	}
	return plugin.HealthStatus{
		Healthy: true,
		Message: "serving " + held.ID,
		Details: map[string]interface{}{
			"subject":   h.subject(),
			"as":        held.ID,
			"published": held.Timestamp,
			"by":        held.Actors,
			"signer":    held.SignerDID,
		},
	}
}

var _ plugin.DomainPlugin = (*Host)(nil)
