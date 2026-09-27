package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Plugins is the signum of the plugins this node runs (ADR-039): what they
// are, what they serve, and bringing one up or down.

func (s *QNTXServer) pluginsSignum() sigil.Signum {
	name := &protocol.Param{Name: "name", Required: true, Says: "The plugin, by the name it reports."}
	acted := []*protocol.Field{
		{Name: "name", Says: "The plugin acted on."},
		{Name: "state", Says: "Its state after the action. A restart says restarting: the outcome is seen in its health."},
		{Name: "action", Says: "The action taken."},
	}
	action := func(verb, does string) *protocol.Sigil {
		return &protocol.Sigil{
			Name:  verb,
			Does:  does,
			Takes: []*protocol.Param{name},
			Gives: acted,
			Http:  &protocol.Endpoint{Method: http.MethodPost, Path: "/api/plugins/{name}/" + verb},
		}
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "plugins",
			Sigils: []*protocol.Sigil{
				{
					Name: "list",
					Does: "Every plugin this node knows: its metadata, its last probed health, its state and the sigils it serves, and every enabled one that failed to load.",
					Gives: []*protocol.Field{
						{Name: "plugins", Says: "One row per plugin."},
						{Name: "health_probed_at", Says: "When the health in the rows was probed. Absent before the first probe."},
						{Name: "health_age_ms", Says: "How old that probe is. Absent before the first probe."},
						{Name: "health_probe_failure", Says: "Why the last probe did not complete, when it did not."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins"},
				},
				{
					Name:  "routes",
					Does:  "What each running plugin serves: its HTTP and socket paths, the roles it plays, its handlers, schedules, watchers and endpoints.",
					Gives: []*protocol.Field{{Name: "routes", Says: "One row per running plugin."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins/routes"},
				},
				{
					Name:  "elements",
					Does:  "The canvas elements running plugins define, with where each one's content, style and module are served.",
					Gives: []*protocol.Field{{Name: "elements", Says: "One row per element definition."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins/elements"},
				},
				action("pause", "Pause a running plugin. Paused is intended, so it stays healthy."),
				action("resume", "Resume a paused plugin."),
				action("restart", "Restart a plugin that am.toml enables, with am.toml read again. It answers at once; the restart completes after."),
				action("enable", "Load and start a plugin at runtime."),
				action("disable", "Stop a plugin at runtime and remove its handlers and watchers."),
			},
		},
		Answers: map[string]sigil.Answer{
			"list":     func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return s.pluginHandler.list(), nil },
			"routes":   func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return s.pluginHandler.routes(), nil },
			"elements": func(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) { return s.pluginHandler.elements(ctx), nil },
			"pause":    s.pluginActionAnswer("pause"),
			"resume":   s.pluginActionAnswer("resume"),
			"restart":  s.pluginActionAnswer("restart"),
			"enable":   s.pluginActionAnswer("enable"),
			"disable":  s.pluginActionAnswer("disable"),
		},
	}
}

func (s *QNTXServer) pluginActionAnswer(action string) sigil.Answer {
	return func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
		return s.pluginAction(ctx, sent["name"], action)
	}
}
