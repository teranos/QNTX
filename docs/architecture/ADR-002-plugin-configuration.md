# ADR-002: Plugin Configuration Management

**Status:** Accepted
**Date:** 2026-01-04
**Deciders:** QNTX Core Team

## Context

Domain plugins need configuration (API keys, workspace paths, feature flags). We need a strategy that:

1. Keeps plugin config isolated from core QNTX config
2. Works with QNTX's "works out of the box" philosophy (sensible defaults)
3. Supports both built-in and external plugins
4. Allows per-plugin configuration without bloating main config file

## Decision

### Configuration File Structure

Each plugin gets its own configuration file:

```
~/.qntx/am.toml           # Core QNTX config (server, database, pulse)
~/.qntx/am.code.toml      # Code domain plugin config
~/.qntx/am.finance.toml   # Finance domain plugin (future)
~/.qntx/am.biotech.toml   # Biotech domain plugin (future)
```

### Configuration Scope

**Core `am.toml`** (QNTX infrastructure):
```toml
[database]
path = "qntx.db"

[server]
port = 877
dev_mode = false

[pulse]
workers = 4
daily_budget_usd = 10.0
```

**Plugin `am.<domain>.toml`** (domain-specific):
```toml
# am.code.toml
[gopls]
enabled = true
workspace_root = "."

[github]
api_token = "ghp_..."  # Optional, from environment preferred

[editor]
max_file_size_mb = 10
```

### Configuration Loading

```go
// Plugin receives Config interface
type Config interface {
    GetString(key string) string
    GetInt(key string) int
    GetBool(key string) bool
    GetStringSlice(key string) []string
    Get(key string) interface{}
    Set(key string, value interface{})  // Runtime overrides
}

// Implementation reads from am.<domain>.toml
config := services.Config("code")
workspace := config.GetString("gopls.workspace_root")  // Reads am.code.toml
```

### Key Namespacing

Within plugin config file, use flat dot notation:
```toml
# am.code.toml
gopls.workspace_root = "."           # Not [code.gopls]
github.api_token = "ghp_..."         # Not [code.github]
```

Rationale: Domain is already clear from filename, nesting is redundant.

### Default Values

Plugins must work without configuration file:

```go
func (p *Plugin) Initialize(ctx context.Context, services ServiceRegistry) error {
    config := services.Config("code")

    // Provide sensible defaults
    workspace := config.GetString("gopls.workspace_root")
    if workspace == "" {
        workspace = "."  // Current directory default
    }

    // Optional features gracefully degrade
    apiToken := config.GetString("github.api_token")
    if apiToken == "" {
        p.logger.Warn("GitHub API token not configured, PR integration disabled")
        // Plugin still initializes, feature disabled
    }
}
```

### Environment Variable Override

Sensitive values (API keys, tokens) should prefer environment variables:

```go
// Check env first, fall back to config file
apiToken := os.Getenv("GITHUB_API_TOKEN")
if apiToken == "" {
    apiToken = config.GetString("github.api_token")
}
```

Config file pattern:
```toml
# am.code.toml
github.api_token = "${GITHUB_API_TOKEN}"  # Reference env var
```

## Consequences

### Positive

✅ **Isolation**: Plugin config changes don't clutter core `am.toml`
✅ **Discovery**: `ls ~/.qntx/am.*.toml` shows all installed plugins
✅ **Optional**: Plugins work without config (zero-config philosophy maintained)
✅ **Security**: Sensitive tokens in separate files (easier to `.gitignore` per plugin)

### Negative

⚠️ **File proliferation**: Multiple config files instead of one
⚠️ **Discovery complexity**: Need to document where each setting lives

### Neutral

- Config file precedence: `am.<domain>.toml` > `am.toml` > defaults
- Built-in plugins (code) follow same pattern as external plugins

## Implementation

### Configuration Provider

```go
// server/init.go
type simpleConfigProvider struct{}

func (p *simpleConfigProvider) GetPluginConfig(domain string) domains.Config {
    return &simpleConfig{domain: domain}
}

type simpleConfig struct {
    domain string
}

func (c *simpleConfig) GetString(key string) string {
    // Try am.<domain>.toml first
    domainFile := fmt.Sprintf("am.%s.toml", c.domain)
    if fileExists(domainFile) {
        if val := readFromFile(domainFile, key); val != "" {
            return val
        }
    }

    // Fall back to am.toml with domain prefix
    return am.GetString(c.domain + "." + key)
}
```

### Configuration Discovery

Plugins can be discovered by scanning for `am.*.toml` files:

```bash
$ ls ~/.qntx/am.*.toml
am.code.toml
am.finance.toml

$ qntx plugin list
code     (v0.1.0) - Software development domain [enabled]
finance  (v0.2.0) - Financial analysis domain [enabled]
```

## Alternatives Considered

### Single am.toml with [plugins.*] sections
- **Rejected**: Bloats main config, unclear ownership

### Plugin-managed config (no am integration)
- **Rejected**: Breaks QNTX's unified config philosophy

### Database-stored config
- **Rejected**: Can't configure DB access itself, chicken-egg problem

## Related

- [ADR-001: Domain Plugin Architecture](./ADR-001-domain-plugin-architecture.md)
- [QNTX Configuration Guide](../../CLAUDE.md#configuration-am-package)
