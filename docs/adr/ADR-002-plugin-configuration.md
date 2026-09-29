# ADR-002: Plugin Configuration Management

Plugins are added, configured, enabled and disabled at runtime.

"I log in to QNTX, open the plugin element, press +, enter a repository URL and confirm. The plugin starts disabled; I edit its config and enable it, and I don't think about it anymore."

## 1. The plugin element

Press + and enter the plugin's repository URL. It is added disabled. Its config is edited in the element, and Enable starts it.

## 2. API

```
POST /api/plugins                    body: repo
GET  /api/plugins/{name}/config
PUT  /api/plugins/{name}/config      body: config
POST /api/plugins/{name}/enable
POST /api/plugins/{name}/disable
```

Both return JSON with the plugin's new state:

```json
{"action": "enable", "name": "myplugin", "state": "running"}
{"action": "disable", "name": "myplugin", "state": "stopped"}
```

See [API reference](https://github.com/teranos/QNTX/blob/main/server/openapi/openapi.json) for all plugin endpoints.

## What happens

**Add:** the plugin is recorded with its repository URL, disabled, in the system store.

**Enable:** recorded as enabled, then discovered from search paths, loaded, gRPC connected, registered, initialized, provider services wired, async handlers and watchers registered. Same sequence as boot, but for one plugin.

**Disable:** recorded as disabled, then gRPC shutdown sent, process killed, unregistered from domain registry, watchers pruned, async handlers removed, HTTP mux cleared.

Plugins not mentioned in the change are untouched. Both transitions emit a colored banner in the log.

## Requirements

- Plugin binary must be discoverable in the configured search paths.
- The server must be past initialization (services and registry available).

## Route discovery

A plugin is its own signum, and the routes it declares are its sigils (ADR-001).

`GET /api/plugins/routes` also maps provider roles to core invocation endpoints (e.g. an `llm-provider` plugin includes `POST /api/prompt/direct` with the provider name).

## Not supported

- Changing plugin search paths at runtime. Restart required.
- Reordering plugins. Order is alphabetical, same as boot.

## Related

- [ADR-001: Domain Plugin Architecture](./ADR-001-domain-plugin-architecture.md)
- [ADR-018: Plugin Lifecycle, Watchers, and Developer Experience](./ADR-018-watcher-lifecycle.md)
- [Plugin API reference](https://github.com/teranos/QNTX/blob/main/server/openapi/openapi.json)
