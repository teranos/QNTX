# ADR-002: Plugin Configuration Management

Plugins are added, configured, enabled and disabled at runtime.

"I log in to QNTX, open the plugin element, press +, enter a repository URL and confirm. The plugin starts disabled; I edit its config and enable it, and I don't think about it anymore."

"i expect it to be a bigger + button as part of the list, like an empty plugin ready to become something."

"it should have been two stage, first stage is check if its even possible, and 2nd is to commit to adding it for real"

"it is qntx that actually owns this"

A plugin is its record on the node: its repository, whether it is enabled, and its config. The plugin element and the `plugins` signum write the record. This document is the reference for how a plugin gets onto a node and stays running there.

## 1. The plugin element

The + is an empty plugin at the end of the list. Press it and enter the plugin's repository URL. Check asks whether the repo resolves; Add commits it. It is added disabled. Its config is edited in the element, and Enable starts it.

## 2. Sigils and API

| Sigil | HTTP |
|---|---|
| `plugins_check` | `POST /api/plugins/check` body: repo |
| `plugins_add` | `POST /api/plugins` body: repo |
| `plugins_enable` | `POST /api/plugins/{name}/enable` |
| `plugins_disable` | `POST /api/plugins/{name}/disable` |
| `plugins_restart` | `POST /api/plugins/{name}/restart` |
| `plugins_list` | `GET /api/plugins` |
| | `GET /api/plugins/{name}/config` |
| | `PUT /api/plugins/{name}/config` body: config |

Enable and disable return JSON with the plugin's new state:

```json
{"action": "enable", "name": "myplugin", "state": "running"}
{"action": "disable", "name": "myplugin", "state": "stopped"}
```

See [API reference](https://github.com/teranos/QNTX/blob/main/server/openapi/openapi.json) for all plugin endpoints.

## 3. The name

The last segment of the URL is the plugin's name: `https://github.com/owner/repo` is `repo`, and `https://github.com/owner/repo/tree/<branch>/path/to/inbox` is `inbox`. That name is the plugin's `Metadata().Name`, its routes are under `/api/<name>/`, and its binary is `qntx-<name>-plugin`.

## 4. The record's config

The plugin's own keys, which it reads through `config.GetString(key)`, and QNTX's:

| Key | |
|---|---|
| `namespace` | The namespace the plugin stands in ([ADR-046](./ADR-046-a-plugin-stands-in-a-namespace.md)). |
| `build.core` | The source that is built: `owner/repo@branch`. |
| `build.inputs` | Files from other repositories the build takes: `owner/repo@branch:path`, space-separated. |
| `build.inputs_env` | The environment variable the build reads the input files' paths from. |
| `build.packages` | nixpkgs packages the build runs with, space-separated. |
| `build.command` | The build, run with `sh -c` at the root of `build.core`. |
| `build.output` | The binary the command makes, relative to that root. |

## 5. The build

QNTX builds a plugin itself ([server/plugin_build.go](../../server/plugin_build.go)). The node is a GitHub App, and a push to a repository the App is installed on reaches its webhook. For each enabled plugin whose `build.*` sources the push moves, QNTX fetches the sources at their branches through the node's GitHub, runs `build.command` in `nix shell --inputs-from <core> nixpkgs#<package>...`, installs `build.output` as `qntx-<name>-plugin` and, when what it built differs from what is installed and the plugin is enabled, restarts it. A build that fails is a mail to ROOT, naming every source with the rev it was built from and why it failed.

ROOT generates the webhook's secret with the `github` signum's webhook sigil, sets its path in the GitHub element, and pastes the URL the element shows into the App's settings.

## What happens

"I expected to also see the plugin README if there is one."

**Check:** GitHubService is asked, as the node, whether the repository and the plugin's directory in it are there, and for its README ([ADR-043](./ADR-043-github-service.md)). Check only reads.

**Add:** the plugin is recorded with its repository URL, disabled, in the system store.

**Enable:** recorded as enabled, then discovered from search paths, loaded, gRPC connected, registered, initialized, provider services wired, async handlers and watchers registered. Same sequence as boot, but for one plugin. Enable probes the plugin's health before it answers; a plugin started after the last probe reports as unprobed.

**Disable:** recorded as disabled, then gRPC shutdown sent, process killed, unregistered from domain registry, watchers pruned, async handlers removed, HTTP mux cleared.

Each change acts on the one plugin it names. Both transitions emit a colored banner in the log.

## Requirements

- The plugin's binary is in the configured search paths; a build QNTX made is installed there.
- The server is past initialization (services and registry available).

## Route discovery

A plugin is its own signum, and the routes it declares are its sigils (ADR-001). Each is also an MCP tool. A Go plugin declares them with `DeclaredRoutes()`.

`GET /api/plugins/routes` also maps provider roles to core invocation endpoints (e.g. an `llm-provider` plugin includes `POST /api/prompt/direct` with the provider name).

## Read at start

- Plugin search paths are read when the node starts.
- Plugins load in alphabetical order, same as boot.

## Related

- [ADR-001: Domain Plugin Architecture](./ADR-001-domain-plugin-architecture.md)
- [ADR-018: Plugin Lifecycle, Watchers, and Developer Experience](./ADR-018-watcher-lifecycle.md)
- [ADR-043: GitHubService](./ADR-043-github-service.md)
- [ADR-046: A plugin stands in a namespace](./ADR-046-a-plugin-stands-in-a-namespace.md)
- [Plugin API reference](https://github.com/teranos/QNTX/blob/main/server/openapi/openapi.json)
