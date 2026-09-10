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
	"os"

	"github.com/teranos/QNTX/plugin"
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

// Host serves one declared glyph module, reading the file when it is asked
// for. plugin.Base is deliberately not embedded — see TestAGlyphIsNotPausable.
type Host struct {
	name   string
	module string
	logger *zap.SugaredLogger
}

// New builds a host for one declared glyph. The name is the route it answers
// on; module is the path on disk it answers with.
func New(name, module string, logger *zap.SugaredLogger) *Host {
	return &Host{name: name, module: module, logger: logger}
}

// Module is the path this host serves, so what a node is running can be read
// off the node rather than inferred from its configuration.
func (h *Host) Module() string { return h.module }

// Metadata names the glyph. No version: the glyph is the file on disk, and a
// number here would be a second answer to what is being served.
func (h *Host) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        h.name,
		Description: "canvas glyph served from " + h.module,
	}
}

// Initialize keeps none of the registry. Nothing starts, because the file is
// opened per request rather than held.
func (h *Host) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
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
		info, err := os.Stat(h.module)
		if err != nil {
			// The canvas reports only that an import failed. Without this line
			// the file it could not read is written down nowhere.
			h.logger.Errorw("Glyph module cannot be read; the canvas will show a failed import",
				"glyph", h.Metadata().Name, "module", h.module, "error", err)
			http.Error(w, "glyph module cannot be read: "+h.module, http.StatusServiceUnavailable)
			return
		}
		if info.IsDir() {
			h.logger.Errorw("Glyph module is a directory, not a module",
				"glyph", h.Metadata().Name, "module", h.module)
			http.Error(w, "glyph module is a directory: "+h.module, http.StatusServiceUnavailable)
			return
		}

		// Set before serving: ServeFile keeps a Content-Type already on the
		// header and would otherwise derive one from the file's extension,
		// which the declaration does not promise anything about.
		w.Header().Set("Content-Type", ModuleContentType)

		// ServeFile writes the body, so there is no write here to drop the
		// failure of, and it answers a conditional request from the file's own
		// modification time.
		http.ServeFile(w, r, h.module)
	})
	return nil
}

// Health reads the file rather than reporting that the host is running. The
// host is always running; the question anyone asks of a glyph is whether the
// module is there.
func (h *Host) Health(ctx context.Context) plugin.HealthStatus {
	if _, err := os.Stat(h.module); err != nil {
		return plugin.HealthStatus{
			Healthy: false,
			Message: "glyph module cannot be read: " + h.module,
			Details: map[string]interface{}{"module": h.module, "error": err.Error()},
		}
	}
	return plugin.HealthStatus{
		Healthy: true,
		Message: "serving " + h.module,
		Details: map[string]interface{}{"module": h.module},
	}
}

var _ plugin.DomainPlugin = (*Host)(nil)
