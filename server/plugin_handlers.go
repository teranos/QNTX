package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// PluginHandler serves read-only plugin information endpoints.
type PluginHandler struct {
	registry *plugin.Registry
	logger   *zap.SugaredLogger
}

// NewPluginHandler creates a handler for plugin info endpoints.
func NewPluginHandler(registry *plugin.Registry, logger *zap.SugaredLogger) *PluginHandler {
	return &PluginHandler{registry: registry, logger: logger}
}

// HandlePlugins returns all installed plugins and their status.
// GET /api/plugins
func (h *PluginHandler) HandlePlugins(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if h.registry == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"plugins": []interface{}{},
		})
		return
	}

	// Get all plugins and their health status
	ctx := r.Context()
	healthResults := h.registry.HealthCheckAll(ctx)
	stateResults := h.registry.GetAllStates()

	type PluginInfo struct {
		Name        string                 `json:"name"`
		Version     string                 `json:"version"`
		QNTXVersion string                 `json:"qntx_version,omitempty"`
		Description string                 `json:"description"`
		Author      string                 `json:"author,omitempty"`
		License     string                 `json:"license,omitempty"`
		Healthy     bool                   `json:"healthy"`
		Message     string                 `json:"message,omitempty"`
		Details     map[string]interface{} `json:"details,omitempty"`
		State       string                 `json:"state"`
		Pausable    bool                   `json:"pausable"`
	}

	plugins := make([]PluginInfo, 0)

	// Include all known plugins — both fully registered and failed/loading
	seen := make(map[string]bool)
	for _, name := range h.registry.List() {
		seen[name] = true
		p, ok := h.registry.Get(name)
		if !ok {
			continue
		}

		meta := p.Metadata()
		health := healthResults[name]
		state := stateResults[name]

		plugins = append(plugins, PluginInfo{
			Name:        meta.Name,
			Version:     meta.Version,
			QNTXVersion: meta.QNTXVersion,
			Description: meta.Description,
			Author:      meta.Author,
			License:     meta.License,
			Healthy:     health.Healthy,
			Message:     health.Message,
			Details:     health.Details,
			State:       string(state),
			Pausable:    h.registry.IsPausable(name),
		})
	}

	// Add pre-registered plugins that failed to load (not in plugins map)
	for _, name := range h.registry.ListEnabled() {
		if seen[name] {
			continue
		}
		state := stateResults[name]
		info := PluginInfo{
			Name:  name,
			State: string(state),
		}
		if errMsg, ok := h.registry.GetError(name); ok {
			info.Message = errMsg
		}
		plugins = append(plugins, info)
	}

	response := map[string]interface{}{
		"plugins": plugins,
	}

	writeJSON(w, http.StatusOK, response)
}

// HandlePluginRoutes returns all plugin-registered routes and capabilities.
// GET /api/plugins/routes
func (h *PluginHandler) HandlePluginRoutes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if h.registry == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"routes": []interface{}{}})
		return
	}

	type RouteEndpoint struct {
		Method      string `json:"method"`
		Path        string `json:"path"`
		Description string `json:"description,omitempty"`
	}

	type PluginRoute struct {
		Name      string          `json:"name"`
		HTTP      string          `json:"http"`
		WebSocket string          `json:"ws,omitempty"`
		Roles     []string        `json:"roles,omitempty"`
		Handlers  []string        `json:"handlers,omitempty"`
		Schedules int             `json:"schedules,omitempty"`
		Watchers  int             `json:"watchers,omitempty"`
		Endpoints []RouteEndpoint `json:"endpoints,omitempty"`
	}

	routes := make([]PluginRoute, 0)
	for _, name := range h.registry.List() {
		p, ok := h.registry.Get(name)
		if !ok {
			continue
		}

		route := PluginRoute{
			Name: name,
			HTTP: "/api/" + name + "/",
		}

		// Check WebSocket registration
		wsHandlers, err := p.RegisterWebSocket()
		if err == nil && len(wsHandlers) > 0 {
			route.WebSocket = "/ws/" + name
		}

		// Check capabilities via type assertion to ExternalDomainProxy
		if proxy, ok := p.(*plugingrpc.ExternalDomainProxy); ok {
			if proxy.IsLLMProvider() {
				route.Roles = append(route.Roles, "llm-provider")
				route.Endpoints = append(route.Endpoints, RouteEndpoint{
					Method:      "POST",
					Path:        "/api/prompt/direct",
					Description: "LLM inference via " + name + " (set \"provider\": \"" + name + "\" in request body)",
				})
			}
			if proxy.IsSearchProvider() {
				route.Roles = append(route.Roles, "search-provider")
			}
			if proxy.IsEmbeddingProvider() {
				route.Roles = append(route.Roles, "embedding-provider")
			}
			route.Handlers = proxy.GetHandlerNames()
			route.Schedules = len(proxy.GetSchedules())
			route.Watchers = len(proxy.GetWatchers())
			for _, rt := range proxy.GetHTTPRoutes() {
				route.Endpoints = append(route.Endpoints, RouteEndpoint{
					Method:      rt.GetMethod(),
					Path:        "/api/" + name + rt.GetPath(),
					Description: rt.GetDescription(),
				})
			}
		}

		routes = append(routes, route)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"routes": routes})
}

// HandlePluginGlyphs returns custom glyph type definitions from all plugins.
// GET /api/plugins/glyphs
func (h *PluginHandler) HandlePluginGlyphs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if h.registry == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	type PluginGlyphDef struct {
		Plugin        string `json:"plugin"`
		Symbol        string `json:"symbol"`
		Title         string `json:"title"`
		Label         string `json:"label"`
		ContentURL    string `json:"content_url"`
		CSSURL        string `json:"css_url,omitempty"`
		ModuleURL     string `json:"module_url,omitempty"`
		DefaultWidth  int    `json:"default_width,omitempty"`
		DefaultHeight int    `json:"default_height,omitempty"`
	}

	glyphs := make([]PluginGlyphDef, 0)
	ctx := r.Context()

	// Iterate through all plugins and get their glyph definitions
	for _, name := range h.registry.List() {
		plugin, ok := h.registry.Get(name)
		if !ok {
			continue
		}

		// Check if plugin supports custom UI
		// Use the client proxy to call RegisterGlyphs
		type glyphProvider interface {
			RegisterGlyphs(ctx context.Context) (*protocol.GlyphDefResponse, error)
		}

		provider, ok := plugin.(glyphProvider)
		if !ok {
			// Plugin doesn't implement RegisterGlyphs - skip
			continue
		}

		// Get glyph definitions from plugin
		resp, err := provider.RegisterGlyphs(ctx)
		if err != nil {
			h.logger.Debugw("Plugin does not provide glyph definitions",
				"plugin", name,
				"error", err)
			continue
		}

		// Convert to response format
		for _, def := range resp.Glyphs {
			contentURL := fmt.Sprintf("/api/%s%s", name, def.ContentPath)
			cssURL := ""
			if def.CssPath != "" {
				cssURL = fmt.Sprintf("/api/%s%s", name, def.CssPath)
			}
			moduleURL := ""
			if def.ModulePath != "" {
				moduleURL = fmt.Sprintf("/api/%s%s", name, def.ModulePath)
			}

			glyphs = append(glyphs, PluginGlyphDef{
				Plugin:        name,
				Symbol:        def.Symbol,
				Title:         def.Title,
				Label:         def.Label,
				ContentURL:    contentURL,
				CSSURL:        cssURL,
				ModuleURL:     moduleURL,
				DefaultWidth:  int(def.DefaultWidth),
				DefaultHeight: int(def.DefaultHeight),
			})
		}
	}

	writeJSON(w, http.StatusOK, glyphs)
}
