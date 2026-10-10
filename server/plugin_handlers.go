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
	"github.com/teranos/errors"
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
	// it handed is not served.
	sigils func(name string) ([]*protocol.SigilRow, []string)
	// records is every plugin the node knows, running or not.
	records func() ([]plugingrpc.PluginRecord, error)
}

// NewPluginHandler creates a handler for plugin info endpoints.
func NewPluginHandler(registry *plugin.Registry, logger *zap.SugaredLogger,
	health func() (map[string]plugin.HealthStatus, time.Time, string),
	sigils func(name string) ([]*protocol.SigilRow, []string),
	records func() ([]plugingrpc.PluginRecord, error)) *PluginHandler {
	return &PluginHandler{registry: registry, logger: logger, health: health, sigils: sigils, records: records}
}

// list is every installed plugin and its status: plugins_list's answer.
func (h *PluginHandler) list() *protocol.PluginsList {
	answer := &protocol.PluginsList{Plugins: []*protocol.PluginInfo{}}

	// Read the last probe rather than making one. Probing here cost gRPC calls
	// per request and held the registry's read lock across them.
	healthResults, probedAt, probeFailure := h.health()
	stateResults := h.registry.GetAllStates()

	known := map[string]plugingrpc.PluginRecord{}
	held, err := h.records()
	if err != nil {
		answer.RecordsFailure = err.Error()
	}
	for _, record := range held {
		known[record.Name] = record
	}

	// A plugin that serves a canvas module can say which one. Asked of the
	// interface, so the answer does not depend on how the plugin is run.
	type moduleDigester interface{ ModuleDigest() string }

	// Include all known plugins — both fully registered and failed/loading
	seen := make(map[string]bool)
	for _, name := range h.registry.List() {
		seen[name] = true
		p, ok := h.registry.Get(name)
		if !ok {
			continue
		}

		meta := p.Metadata()
		health, probed := healthResults[name]
		state := stateResults[name]

		info := &protocol.PluginInfo{
			Name:        meta.Name,
			Version:     meta.Version,
			QntxVersion: meta.QNTXVersion,
			Description: meta.Description,
			Author:      meta.Author,
			License:     meta.License,
			Healthy:     health.Healthy,
			Probed:      probed,
			Message:     health.Message,
			Details:     healthDetails(health.Details),
			State:       string(state),
			Pausable:    h.registry.IsPausable(name),
		}
		if digester, serves := p.(moduleDigester); serves {
			info.ModuleDigest = digester.ModuleDigest()
		}
		info.Sigils, info.SignaRefused = h.sigils(name)
		info.Repo, info.Enabled = known[name].Repo, known[name].Enabled
		answer.Plugins = append(answer.Plugins, info)
	}

	// Add pre-registered plugins that failed to load (not in plugins map)
	for _, name := range h.registry.ListEnabled() {
		if seen[name] {
			continue
		}
		state := stateResults[name]
		info := &protocol.PluginInfo{
			Name:  name,
			State: string(state),
		}
		if errMsg, ok := h.registry.GetError(name); ok {
			info.Message = errMsg
		}
		seen[name] = true
		info.Repo, info.Enabled = known[name].Repo, known[name].Enabled
		answer.Plugins = append(answer.Plugins, info)
	}

	// Every added plugin the registry does not hold: disabled, or enabled and
	// not loaded, with why.
	for _, name := range slices.Sorted(maps.Keys(known)) {
		if seen[name] {
			continue
		}
		record := known[name]
		info := &protocol.PluginInfo{Name: name, State: "disabled", Repo: record.Repo, Enabled: record.Enabled}
		if record.Enabled {
			info.State = string(plugin.StateFailed)
			if errMsg, ok := h.registry.GetError(name); ok {
				info.Message = errMsg
			}
		}
		answer.Plugins = append(answer.Plugins, info)
	}

	// Health here is a probe with an age. Saying when it was taken is what keeps
	// a stale answer from reading as a current one.
	if !probedAt.IsZero() {
		answer.Health = &protocol.PluginHealthProbe{
			ProbedAt: probedAt.UTC().Format(time.RFC3339),
			AgeMs:    float64(time.Since(probedAt).Milliseconds()),
		}
	}
	answer.HealthProbeFailure = probeFailure
	return answer
}

// healthDetails is what a plugin's health said besides, as gRPC carries it:
// each value formatted the way the plugin's side formats it (grpc/server.go).
func healthDetails(details map[string]any) map[string]string {
	said := make(map[string]string, len(details))
	for key, value := range details {
		said[key] = fmt.Sprintf("%v", value)
	}
	return said
}

// routes is what each running plugin serves: plugins_routes's answer.
func (h *PluginHandler) routes() (*protocol.PluginRoutes, error) {
	answer := &protocol.PluginRoutes{Routes: []*protocol.PluginRoute{}}

	for _, name := range h.registry.List() {
		p, ok := h.registry.Get(name)
		if !ok {
			continue
		}

		route := &protocol.PluginRoute{
			Name: name,
			Http: "/api/" + name + "/",
		}

		// A plugin's socket is the handler it serves at /ws/{name}, where the
		// node looks for it.
		wsHandlers, err := p.RegisterWebSocket()
		if err != nil {
			return nil, errors.Wrapf(err, "plugin %s did not say what it serves over WebSocket", name)
		}
		if _, serves := wsHandlers["/ws/"+name]; serves {
			route.Ws = "/ws/" + name
		}

		// Check capabilities via type assertion to ExternalDomainProxy
		if proxy, ok := p.(*plugingrpc.ExternalDomainProxy); ok {
			if proxy.IsLLMProvider() {
				route.Roles = append(route.Roles, "llm-provider")
				route.Endpoints = append(route.Endpoints, &protocol.RouteEndpoint{
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
			route.Schedules = uint32(len(proxy.GetSchedules()))
			route.Watchers = uint32(len(proxy.GetWatchers()))
		}

		answer.Routes = append(answer.Routes, route)
	}

	return answer, nil
}

// elements is the element definitions running plugins make: plugins_elements's
// answer. A sigil gives an object, so the rows are under elements.
func (h *PluginHandler) elements(ctx context.Context) *protocol.PluginElements {
	answer := &protocol.PluginElements{Elements: []*protocol.PluginElement{}}

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

			answer.Elements = append(answer.Elements, &protocol.PluginElement{
				Plugin:        name,
				Symbol:        def.Symbol,
				Title:         def.Title,
				Label:         def.Label,
				ContentUrl:    contentURL,
				CssUrl:        cssURL,
				ModuleUrl:     moduleURL,
				DefaultWidth:  def.DefaultWidth,
				DefaultHeight: def.DefaultHeight,
			})
		}
	}

	return answer
}
