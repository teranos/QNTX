# Plugin Hot-Swap

Edit `[plugin] enabled` in am.toml while the server is running. The config watcher detects the change, diffs the enabled list against currently loaded plugins, and starts or stops plugins accordingly.

## Behavior

- Plugin added to `enabled`: discovered from search paths, loaded, registered, initialized, provider services wired. Same sequence as boot, but for one plugin.
- Plugin removed from `enabled`: gRPC shutdown sent, process killed, unregistered from domain registry, HTTP mux cleared.
- Plugins not mentioned in the change: untouched.
- Failed hot-loads retry in background with the same exponential backoff as boot.

## Requirements

- Config watcher must be active (requires a project-level am.toml on disk).
- Plugin binary must be discoverable in the configured search paths.
- The server must be past initialization (services and registry available).

## Not supported

- Changing plugin search paths at runtime. Restart required.
- Reordering plugins. Order is alphabetical, same as boot.
