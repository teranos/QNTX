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
	action := func(verb, does string) *protocol.Sigil {
		return &protocol.Sigil{
			Name:   verb,
			Does:   does,
			Takes:  []*protocol.Param{name},
			Answer: "protocol.PluginAction",
			Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/plugins/{name}/" + verb},
		}
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "plugins",
			Description: "The plugins this node runs: what each serves and how healthy it is; added from a repository after it is checked, enabled, disabled, paused, resumed and restarted.",
			Tags:        []string{"plugins", "extensions", "health"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "Every plugin this node knows: its metadata, its last probed health, its state and the sigils it serves, and every enabled one that failed to load.",
					Answer: "protocol.PluginsList",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins"},
				},
				{
					Name:   "routes",
					Does:   "What each running plugin serves: its HTTP and socket paths, the roles it plays, its handlers, schedules and watchers, and the core endpoints its roles are asked through.",
					Answer: "protocol.PluginRoutes",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins/routes"},
				},
				{
					Name:   "elements",
					Does:   "The canvas elements running plugins define, with where each one's content, style and module are served.",
					Answer: "protocol.PluginElements",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/plugins/elements"},
				},
				{
					Name: "add",
					Does: "Add a plugin by its repository URL. It starts disabled; its config is set and it is enabled from the plugin element.",
					Takes: []*protocol.Param{{Name: "repo", Required: true,
						Says: "The repository URL, or a tree URL naming a plugin inside one. Its last segment is the plugin's name."}},
					Answer: "protocol.PluginAdded",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/plugins"},
				},
				{
					Name: "check",
					Does: "Whether a repository URL is there on GitHub, and the plugin's directory in it, asked as the node, before adding it; with its README when it has one. Nothing is written or installed.",
					Takes: []*protocol.Param{{Name: "repo", Required: true,
						Says: "The repository URL, or a tree URL naming a plugin inside one."}},
					Answer: "protocol.PluginChecked",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/plugins/check"},
				},
				action("pause", "Pause a running plugin. Paused is intended, so it stays healthy."),
				action("resume", "Resume a paused plugin."),
				action("restart", "Restart an enabled plugin with its config read again. It answers at once; the restart completes after."),
				action("enable", "Enable a plugin and start it from its build. With no build yet, it says why, and it starts when its build lands under the runner."),
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
				return &protocol.PluginAdded{Name: added.Name, Repo: added.Repo, Enabled: added.Enabled}, nil
			},
			"check": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				checked, err := s.checkPlugin(ctx, sent["repo"])
				if err != nil {
					return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "repo", Says: err.Error()}
				}
				return checked, nil
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
