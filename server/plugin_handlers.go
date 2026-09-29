package server

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// PluginHandler serves read-only plugin information endpoints.
type PluginHandler struct {
	registry *plugin.Registry
	logger   *zap.SugaredLogger
	// health reads the last probe: the results, when it was taken, and why the
	// most recent attempt failed if it did. It reaches no plugin.
	health func() (map[string]plugin.HealthStatus, time.Time, string)
	// sigils is one plugin's sigils, with who reaches each, and why any signum
	// it handed is not served. Nil on a node that serves no plugin sigils.
	sigils func(name string) ([]sigilRow, []string)
	// records is every plugin the node knows, running or not. Nil lists only
	// what the registry holds.
	records func() ([]plugingrpc.PluginRecord, error)
}

// NewPluginHandler creates a handler for plugin info endpoints.
func NewPluginHandler(registry *plugin.Registry, logger *zap.SugaredLogger,
	health func() (map[string]plugin.HealthStatus, time.Time, string)) *PluginHandler {
	return &PluginHandler{registry: registry, logger: logger, health: health}
}

// list is every installed plugin and its status: plugins_list's answer.
func (h *PluginHandler) list() map[string]interface{} {
	if h.registry == nil {
		return map[string]interface{}{"plugins": []interface{}{}}
	}

	// Read the last probe rather than making one. Probing here cost gRPC calls
	// per request and held the registry's read lock across them.
	healthResults, probedAt, probeFailure := h.health()
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
		// ModuleDigest identifies the element module this plugin serves, so the
		// browser can import a replaced one instead of the module record it
		// already holds for that URL. Empty for anything not serving a module.
		ModuleDigest string `json:"module_digest,omitempty"`
		// Sigils is what the plugin does, as the node serves it (ADR-039), and
		// SignaRefused is why a signum it handed is served nowhere.
		Sigils       []sigilRow `json:"sigils,omitempty"`
		SignaRefused []string   `json:"signa_refused,omitempty"`
		// Repo and Enabled are the plugin's record: where it was added from,
		// and whether it is switched on.
		Repo    string `json:"repo,omitempty"`
		Enabled bool   `json:"enabled"`
	}

	known := map[string]plugingrpc.PluginRecord{}
	var recordsFailure string
	if h.records != nil {
		held, err := h.records()
		if err != nil {
			recordsFailure = err.Error()
		}
		for _, record := range held {
			known[record.Name] = record
		}
	}

	// A plugin that serves a canvas module can say which one. Asked of the
	// interface, so the answer does not depend on how the plugin is run.
	type moduleDigester interface{ ModuleDigest() string }

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

		info := PluginInfo{
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
		}
		if digester, serves := p.(moduleDigester); serves {
			info.ModuleDigest = digester.ModuleDigest()
		}
		if h.sigils != nil {
			info.Sigils, info.SignaRefused = h.sigils(name)
		}
		info.Repo, info.Enabled = known[name].Repo, known[name].Enabled
		plugins = append(plugins, info)
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
		seen[name] = true
		info.Repo, info.Enabled = known[name].Repo, known[name].Enabled
		plugins = append(plugins, info)
	}

	// Every added plugin the registry does not hold: disabled, or enabled and
	// not loaded, with why.
	for _, name := range slices.Sorted(maps.Keys(known)) {
		if seen[name] {
			continue
		}
		record := known[name]
		info := PluginInfo{Name: name, State: "disabled", Repo: record.Repo, Enabled: record.Enabled}
		if record.Enabled {
			info.State = string(plugin.StateFailed)
			if errMsg, ok := h.registry.GetError(name); ok {
				info.Message = errMsg
			}
		}
		plugins = append(plugins, info)
	}

	// Health here is a probe with an age. Saying when it was taken is what keeps
	// a stale answer from reading as a current one.
	response := map[string]interface{}{
		"plugins": plugins,
	}
	if !probedAt.IsZero() {
		response["health_probed_at"] = probedAt.UTC().Format(time.RFC3339)
		response["health_age_ms"] = time.Since(probedAt).Milliseconds()
	}
	if probeFailure != "" {
		response["health_probe_failure"] = probeFailure
	}
	if recordsFailure != "" {
		response["records_failure"] = recordsFailure
	}
	return response
}

// routes is what each running plugin serves: plugins_routes's answer.
func (h *PluginHandler) routes() map[string]interface{} {
	if h.registry == nil {
		return map[string]interface{}{"routes": []interface{}{}}
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
		}

		routes = append(routes, route)
	}

	return map[string]interface{}{"routes": routes}
}

// elements is the element definitions running plugins make: plugins_elements's
// answer. A sigil gives an object, so the rows are under elements.
func (h *PluginHandler) elements(ctx context.Context) map[string]interface{} {
	if h.registry == nil {
		return map[string]interface{}{"elements": []interface{}{}}
	}

	type PluginElementDef struct {
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

	items := make([]PluginElementDef, 0)

	// Iterate through all plugins and get their element definitions
	for _, name := range h.registry.List() {
		plugin, ok := h.registry.Get(name)
		if !ok {
			continue
		}

		// Check if plugin supports custom UI
		// Use the client proxy to call RegisterElements
		type elementProvider interface {
			RegisterElements(ctx context.Context) (*protocol.ElementDefResponse, error)
		}

		provider, ok := plugin.(elementProvider)
		if !ok {
			// Plugin doesn't implement RegisterElements - skip
			continue
		}

		// Get element definitions from plugin
		resp, err := provider.RegisterElements(ctx)
		if err != nil {
			h.logger.Debugw("Plugin does not provide element definitions",
				"plugin", name,
				"error", err)
			continue
		}

		// Convert to response format
		for _, def := range resp.Elements {
			contentURL := fmt.Sprintf("/api/%s%s", name, def.ContentPath)
			cssURL := ""
			if def.CssPath != "" {
				cssURL = fmt.Sprintf("/api/%s%s", name, def.CssPath)
			}
			moduleURL := ""
			if def.ModulePath != "" {
				moduleURL = fmt.Sprintf("/api/%s%s", name, def.ModulePath)
			}

			items = append(items, PluginElementDef{
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

	return map[string]interface{}{"elements": items}
}
