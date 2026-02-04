# ADR-002: Plugin Configuration Management

**Status:** Accepted (Updated 2026-01-04)
**Date:** 2026-01-04
**Deciders:** QNTX Core Team

## Context

Domain plugins need configuration for:
1. **Discovery**: Where to find plugin binaries
2. **Selection**: Which plugins to load (explicit opt-in)
3. **Plugin-specific settings**: API keys, workspace paths, feature flags

Requirements:
- Works with QNTX's "minimal core" philosophy (no plugins by default)
- Simple, centralized configuration in `am.toml`
- Supports plugin discovery from filesystem
- Plugin-specific config without bloating main config

## Decision

### Configuration Model: Whitelist + Discovery Paths

Plugins are configured via a single `[plugin]` section in `am.toml`:

```toml
[plugin]
enabled = ["code"]                      # Whitelist of plugins to load
paths = ["~/.qntx/plugins", "./plugins"] # Where to search for binaries
```

**Key principles:**
- **No plugins by default**: Empty `enabled` list means minimal core mode
- **Explicit opt-in**: Users must add plugin name to `enabled` list
- **Automatic discovery**: QNTX searches configured paths for binaries
- **Fail-soft**: Missing plugins log warning, don't prevent startup

### Plugin Discovery

QNTX searches for plugin binaries using common naming conventions:

```
~/.qntx/plugins/qntx-code-plugin    # Preferred naming
~/.qntx/plugins/qntx-code           # Alternative
~/.qntx/plugins/code                # Fallback
./plugins/qntx-code-plugin          # Project-level plugins
```

Discovery algorithm:
1. For each plugin in `enabled` list (e.g., `"code"`)
2. Search each path in `paths` for binaries matching:
   - `qntx-{name}-plugin`
   - `qntx-{name}`
   - `{name}`
3. Verify binary is executable
4. Load first match via gRPC

### Plugin-Specific Configuration

Plugin-specific settings remain in `am.toml` under domain namespace:

```toml
# Core QNTX configuration
[database]
path = "qntx.db"

[server]
port = 877

[pulse]
workers = 4

# Plugin configuration
[plugin]
enabled = ["code"]
paths = ["~/.qntx/plugins"]

# Code plugin specific settings
[code.gopls]
enabled = true
workspace_root = "."

[code.github]
# API token preferably from environment: QNTX_CODE_GITHUB_TOKEN
```

### Configuration Access in Plugins

Plugins receive `Config` interface via `ServiceRegistry`:

```go
func (p *Plugin) Initialize(ctx context.Context, services ServiceRegistry) error {
    config := services.Config("code")  // Gets [code.*] section from am.toml

    // Provide sensible defaults
    workspace := config.GetString("gopls.workspace_root")
    if workspace == "" {
        workspace = "."  // Default to current directory
    }

    // Optional features degrade gracefully
    apiToken := config.GetString("github.api_token")
    if apiToken == "" {
        p.logger.Warn("GitHub API token not configured, PR integration disabled")
        // Plugin still initializes, feature disabled
    }
}
```

### Environment Variable Overrides

Sensitive values should prefer environment variables:

```bash
# .env or shell
export QNTX_CODE_GITHUB_TOKEN="ghp_..."
export QNTX_DATABASE_PATH="custom.db"
```

Environment variables follow pattern: `QNTX_{DOMAIN}_{KEY}`

Configuration precedence:
1. Environment variables (highest priority)
2. `am.toml` values
3. Plugin defaults (lowest priority)

## Configuration Examples

### Minimal Core (No Plugins)

```toml
# am.toml - minimal QNTX
[database]
path = "qntx.db"

[server]
port = 877

# No [plugin] section = no plugins loaded
```

QNTX runs with only:
- ATS (attestation system)
- Database (⊔)
- Pulse (꩜ async jobs)
- Server (graph visualization)

### Code Plugin Enabled

```toml
[plugin]
enabled = ["code"]
paths = ["~/.qntx/plugins", "./plugins"]

[code.gopls]
enabled = true
workspace_root = "."
```

### Multiple Plugins

```toml
[plugin]
enabled = ["code", "finance", "biotech"]
paths = ["~/.qntx/plugins"]

[code.gopls]
workspace_root = "/workspace/main-repo"

[finance]
api_key = "${FINANCE_API_KEY}"

[biotech.ncbi]
api_key = "${NCBI_API_KEY}"
email = "researcher@example.com"
```

## Consequences

### Positive

✅ **Minimal by default**: No plugins loaded unless explicitly configured
✅ **Simple discovery**: Just drop binary in `~/.qntx/plugins/` and add to enabled list
✅ **Centralized config**: All configuration in one `am.toml` file
✅ **Flexible paths**: Support both user-level (`~/.qntx/plugins`) and project-level (`./plugins`)
✅ **Optional**: QNTX works without any plugins (minimal core mode)
✅ **Standard naming**: Common conventions make plugin binaries discoverable

### Negative

⚠️ **Manual installation**: Users must download/build plugin binaries
⚠️ **Path management**: Users must ensure binaries are in configured paths
⚠️ **No version management**: No automatic plugin updates (manual for now)

### Neutral

- Plugin configuration lives in same file as core config (domain-namespaced)
- Discovery is filesystem-based (simple but requires manual binary management)
- Future: Could add plugin registry/marketplace for automatic installation

## Implementation

### Configuration Schema

```go
// am/am.go
type Config struct {
    Plugin PluginConfig `mapstructure:"plugin"`
    // ... other config sections
}

type PluginConfig struct {
    Enabled []string `mapstructure:"enabled"` // Whitelist of plugins
    Paths   []string `mapstructure:"paths"`   // Search paths
}
```

### Plugin Discovery

```go
// plugin/grpc/loader.go
func LoadPluginsFromConfig(ctx context.Context, cfg *am.Config, logger *zap.SugaredLogger) (*PluginManager, error) {
    manager := NewPluginManager(logger)

    if len(cfg.Plugin.Enabled) == 0 {
        logger.Infow("No plugins enabled - QNTX running in minimal core mode")
        return manager, nil
    }

    // Discover plugins from configured paths
    for _, pluginName := range cfg.Plugin.Enabled {
        pluginConfig, err := discoverPlugin(pluginName, cfg.Plugin.Paths, logger)
        if err != nil {
            logger.Warnw("Failed to discover plugin", "plugin", pluginName, "error", err)
            continue
        }

        // Load plugin via gRPC
        if err := manager.LoadPlugins(ctx, []PluginConfig{pluginConfig}); err != nil {
            return nil, err
        }
    }

    return manager, nil
}
```

### Binary Naming Conventions

Plugins should use these naming conventions for discoverability:

| Pattern | Example | Priority |
|---------|---------|----------|
| `qntx-{domain}-plugin` | `qntx-code-plugin` | Preferred |
| `qntx-{domain}` | `qntx-code` | Alternative |
| `{domain}` | `code` | Fallback |

All binaries must be:
- Executable (`chmod +x`)
- Located in one of the configured search paths
- Implement gRPC plugin protocol

## Migration Path

### From Phase 2 (Built-in Plugins)

Before Phase 3:
```go
// main.go - hardcoded
codePlugin := qntxcode.NewPlugin()
registry.Register(codePlugin)
```

After Phase 3:
```toml
# am.toml
[plugin]
enabled = ["code"]
paths = ["~/.qntx/plugins"]
```

```go
// main.go - discovery
manager, _ := grpc.LoadPluginsFromConfig(ctx, cfg, logger)
for _, plugin := range manager.GetAllPlugins() {
    registry.Register(plugin)
}
```

### Future: Plugin Marketplace (Phase 5+)

Potential future enhancements:
```bash
$ qntx plugin install code
Downloading qntx-code-plugin v0.2.0...
Installing to ~/.qntx/plugins/qntx-code-plugin
Added to am.toml: plugin.enabled = ["code"]

$ qntx plugin list
code     v0.2.0  [enabled]   Software development domain
finance  v0.1.0  [available] Financial analysis domain
biotech  -       [available] Bioinformatics domain
```

## Alternatives Considered

### Individual am.<domain>.toml Files
**Rejected**: File proliferation, unclear which plugins are enabled, harder to manage

### Plugin Registry Service
**Rejected**: Too complex for Phase 3, adds external dependency

### Automatic Plugin Discovery (No Whitelist)
**Rejected**: Security risk (auto-loading unknown binaries), against minimal core principle

### Go Plugin (.so files)
**Rejected**: Platform-specific, fragile across Go versions, build complexity

## Related

- [ADR-001: Domain Plugin Architecture](./ADR-001-domain-plugin-architecture.md)
- [ADR-003: Plugin Communication Patterns](./ADR-003-plugin-communication.md)
- PR #136: Phase 3 - Plugin Discovery and Optional Loading
- Issue #135: Phase 4 - Extract qntx-code to Separate Repository
