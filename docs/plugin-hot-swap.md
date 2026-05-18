# Plugin Hot-Swap

Plugins can be enabled and disabled at runtime without restarting the server. Two ways to do it:

## 1. Edit am.toml

Edit `[plugin] enabled` while the server is running. The config watcher detects the change, diffs the enabled list against currently loaded plugins, and starts or stops plugins accordingly.

```toml
[plugin]
enabled = [
  "reduce",
  "spindle",
  #"myplugin",    # comment out to disable
]
```

## 2. API

```
POST /api/plugins/{name}/enable
POST /api/plugins/{name}/disable
```

Both return JSON with the plugin's new state:

```json
{"action": "enable", "name": "myplugin", "state": "running"}
{"action": "disable", "name": "myplugin", "state": "stopped"}
```

See [API reference](api/plugins.md) for all plugin endpoints.

## What happens

**Enable:** discovered from search paths, loaded, gRPC connected, registered, initialized, provider services wired, async handlers and watchers registered. Same sequence as boot, but for one plugin.

**Disable:** gRPC shutdown sent, process killed, unregistered from domain registry, watchers pruned, async handlers removed, HTTP mux cleared.

Plugins not mentioned in the change are untouched. Both transitions emit a colored banner in the log.

## Requirements

- Config watcher must be active (requires a project-level am.toml on disk).
- Plugin binary must be discoverable in the configured search paths.
- The server must be past initialization (services and registry available).

## Route discovery

Plugins can advertise their HTTP endpoints by setting `http_routes` in `InitializeResponse`. These show up in the plugin banner at startup and are queryable via `GET /api/plugins/routes`. This is optional — plugins that don't set it get a nudge in the banner.

`GET /api/plugins/routes` also maps provider roles to core invocation endpoints (e.g. an `llm-provider` plugin includes `POST /api/prompt/direct` with the provider name).

## Not supported

- Changing plugin search paths at runtime. Restart required.
- Reordering plugins. Order is alphabetical, same as boot.

## Related

- [ADR-001: Domain Plugin Architecture](adr/ADR-001-domain-plugin-architecture.md)
- [ADR-002: Plugin Configuration Management](adr/ADR-002-plugin-configuration.md)
- [ADR-018: Plugin Lifecycle, Watchers, and Developer Experience](adr/ADR-018-watcher-lifecycle.md)
- [Plugin API reference](api/plugins.md)
