package server

import (
	"context"
	"net/http"
	"strings"

	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
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
					Does:  "What each running plugin serves: its HTTP and socket paths, the roles it plays, its handlers, schedules and watchers, and the core endpoints its roles are asked through.",
					Gives: []*protocol.Field{{Name: "routes", Says: "One row per running plugin."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins/routes"},
				},
				{
					Name:  "elements",
					Does:  "The canvas elements running plugins define, with where each one's content, style and module are served.",
					Gives: []*protocol.Field{{Name: "elements", Says: "One row per element definition."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins/elements"},
				},
				{
					Name: "add",
					Does: "Add a plugin by its repository URL. It starts disabled; its config is set and it is enabled from the plugin element.",
					Takes: []*protocol.Param{{Name: "repo", Required: true,
						Says: "The repository URL, or a tree URL naming a plugin inside one. Its last segment is the plugin's name."}},
					Gives: []*protocol.Field{
						{Name: "name", Says: "The plugin's name."},
						{Name: "repo", Says: "The repository it was added from."},
						{Name: "enabled", Says: "An added plugin starts disabled."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/plugins"},
				},
				{
					Name: "check",
					Does: "Whether a repository URL resolves to a release of a plugin this node could install, before adding it. Nothing is written or installed.",
					Takes: []*protocol.Param{{Name: "repo", Required: true,
						Says: "The repository URL, or a tree URL naming a plugin inside one."}},
					Gives: []*protocol.Field{
						{Name: "name", Says: "The plugin's name."},
						{Name: "repo", Says: "The repository URL checked."},
						{Name: "release", Says: "The newest release carrying the plugin for this node's platform."},
						{Name: "asset", Says: "The asset in that release this node would install."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/plugins/check"},
				},
				action("pause", "Pause a running plugin. Paused is intended, so it stays healthy."),
				action("resume", "Resume a paused plugin."),
				action("restart", "Restart an enabled plugin. It answers at once; the restart completes after."),
				action("enable", "Enable a plugin and start it."),
				action("disable", "Disable a plugin, stop it, and remove its handlers and watchers."),
			},
		},
		Answers: map[string]sigil.Answer{
			"list":   func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return s.pluginHandler.list(), nil },
			"routes": func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return s.pluginHandler.routes(), nil },
			"elements": func(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
				return s.pluginHandler.elements(ctx), nil
			},
			"add": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				added, err := s.pluginRecords().AddPlugin(actorOf(ctx), sent["repo"])
				if err != nil {
					return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "repo", Says: err.Error()}
				}
				return added, nil
			},
			"check": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				resolveCtx, cancel := context.WithTimeout(ctx, plugingrpc.PluginDigestTimeout)
				defer cancel()
				resolved, err := plugingrpc.ResolvePlugin(resolveCtx, sent["repo"])
				if err != nil {
					says := err.Error()
					if hints := errors.GetAllHints(err); len(hints) > 0 {
						says += "\n" + strings.Join(hints, "\n")
					}
					return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "repo", Says: says}
				}
				return resolved, nil
			},
			"pause":   s.pluginActionAnswer("pause"),
			"resume":  s.pluginActionAnswer("resume"),
			"restart": s.pluginActionAnswer("restart"),
			"enable":  s.pluginActionAnswer("enable"),
			"disable": s.pluginActionAnswer("disable"),
		},
	}
}

func (s *QNTXServer) pluginActionAnswer(action string) sigil.Answer {
	return func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
		return s.pluginAction(ctx, sent["name"], action)
	}
}
