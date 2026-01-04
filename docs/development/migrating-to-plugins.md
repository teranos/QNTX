# Migrating Existing Features to Domain Plugins

Guide for extracting existing QNTX features into domain plugins.

## Table of Contents

- [When to Extract a Plugin](#when-to-extract-a-plugin)
- [Migration Strategy](#migration-strategy)
- [Code Domain Case Study](#code-domain-case-study)
- [Step-by-Step Migration](#step-by-step-migration)
- [Testing Migrated Plugins](#testing-migrated-plugins)
- [Rollback Strategy](#rollback-strategy)

## When to Extract a Plugin

Extract a feature into a plugin when:

✅ **Domain Cohesion**: Feature belongs to a distinct functional domain (code, finance, biotech, legal)
✅ **Independent Evolution**: Feature needs to evolve separately from core QNTX
✅ **Third-Party Use Case**: External developers might want to customize/replace this domain
✅ **Size**: Feature is substantial enough to justify plugin overhead (>500 LOC, multiple files)

Do **not** extract when:

❌ **Core Infrastructure**: Feature is fundamental to QNTX (attestation system, database, Ax query)
❌ **Cross-Cutting**: Feature is used by multiple domains (logger, config)
❌ **Too Small**: Feature is a single function/utility (creates unnecessary overhead)

## Migration Strategy

### Phase 1: Internal Plugin (No Breaking Changes)

Move code to `domains/<name>/` but keep it built-in:

```
Before:                     After:
code/                       domains/code/
├── github/           →     ├── vcs/github/
├── gopls/            →     ├── langserver/gopls/
└── ast/              →     ├── ast/
                            └── plugin.go (new)
```

**Benefits**:
- Validates plugin API before external release
- No user-facing changes (still one binary)
- Easier to fix API issues

### Phase 2: External Plugin (gRPC)

Extract to separate repository/binary:

```
QNTX Repository:            External Plugin Repository:
main                        main
├── domains/                └── qntx-code-plugin/
│   └── grpc/                   ├── main.go (gRPC server)
│       └── protocol/           ├── plugin.go (DomainPlugin impl)
│           └── domain.proto    └── go.mod
└── cmd/qntx/
```

**Benefits**:
- Process isolation
- Independent versioning
- Private plugin development

## Code Domain Case Study

The code domain migration (PR #130) demonstrates the internal plugin phase:

### What Was Moved

**Before** (scattered across codebase):
```
code/
├── github/           # GitHub PR integration
├── gopls/            # Go language server
├── ast/              # AST transformations
└── ixgest/git/       # Git ingestion (was in ixgest/git/)
cmd/qntx/commands/
├── code.go           # CLI commands
└── ixgest_git.go
server/
├── code_handler.go   # HTTP handlers
└── gopls_handler.go
```

**After** (cohesive plugin):
```
domains/code/
├── plugin.go              # DomainPlugin implementation
├── commands.go            # CLI command builders
├── handlers.go            # HTTP handlers
├── vcs/github/            # GitHub integration
├── langserver/gopls/      # gopls language server
├── ast/                   # AST utilities
└── ixgest/git/            # Git repository ingestion
```

### What Changed

1. **Interface Implementation**: Added `plugin.go` implementing `DomainPlugin`
2. **CLI Integration**: Commands moved from `cmd/` to `plugin.Commands()`
3. **HTTP Integration**: Handlers moved from `server/` to `plugin.RegisterHTTP()`
4. **Initialization**: Explicit `Initialize()` instead of package-level init

### What Stayed The Same

- User commands: `qntx code ix git <repo>` (identical)
- HTTP endpoints: `/api/code/github/pr` (identical)
- Configuration: `am.code.*` settings (identical)
- Database schema: No changes

## Step-by-Step Migration

### Step 1: Identify Plugin Boundary

Determine what belongs in the plugin:

```
Domain: finance

Includes:
✅ finance/stocks/        # Stock price ingestion
✅ finance/analysis/      # Financial analysis
✅ finance/reporting/     # Report generation

Excludes:
❌ ats/                   # Core attestation system (used by all domains)
❌ pulse/                 # Job system (infrastructure)
```

### Step 2: Create Plugin Structure

```bash
mkdir -p domains/finance
touch domains/finance/plugin.go
```

**`domains/finance/plugin.go`**:
```go
package finance

import (
    "context"
    "net/http"

    "github.com/spf13/cobra"
    "github.com/teranos/QNTX/domains"
)

type Plugin struct {
    services domains.ServiceRegistry
}

func NewPlugin() *Plugin {
    return &Plugin{}
}

func (p *Plugin) Metadata() domains.Metadata {
    return domains.Metadata{
        Name:        "finance",
        Version:     "0.1.0",
        QNTXVersion: ">= 0.1.0",
        Description: "Financial analysis and reporting domain",
        Author:      "Your Organization",
        License:     "MIT",
    }
}

func (p *Plugin) Initialize(ctx context.Context, services domains.ServiceRegistry) error {
    p.services = services
    logger := services.Logger("finance")
    logger.Info("Finance domain plugin initialized")
    return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
    if p.services != nil {
        logger := p.services.Logger("finance")
        logger.Info("Finance domain plugin shutting down")
    }
    return nil
}

func (p *Plugin) Commands() []*cobra.Command {
    // TODO: Implement
    return nil
}

func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
    // TODO: Implement
    return nil
}

func (p *Plugin) RegisterWebSocket() (map[string]domains.WebSocketHandler, error) {
    return nil, nil
}

func (p *Plugin) Health(ctx context.Context) domains.HealthStatus {
    return domains.HealthStatus{
        Healthy: true,
        Message: "Finance domain operational",
        Details: make(map[string]interface{}),
    }
}
```

### Step 3: Move Source Files

```bash
# Move existing code
mv finance/ domains/finance/analysis/
mv cmd/qntx/commands/finance.go domains/finance/commands.go
mv server/finance_handler.go domains/finance/handlers.go
```

Update import paths:
```go
// Before
import "github.com/teranos/QNTX/finance/analysis"

// After
import "github.com/teranos/QNTX/domains/finance/analysis"
```

### Step 4: Implement CLI Commands

**`domains/finance/commands.go`**:
```go
func (p *Plugin) Commands() []*cobra.Command {
    financeCmd := &cobra.Command{
        Use:   "finance",
        Short: "Financial analysis tools",
    }

    financeCmd.AddCommand(&cobra.Command{
        Use:   "analyze <company>",
        Short: "Analyze company financials",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            return p.analyzeCompany(args[0])
        },
    })

    return []*cobra.Command{financeCmd}
}
```

### Step 5: Implement HTTP Handlers

**`domains/finance/handlers.go`**:
```go
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
    mux.HandleFunc("/api/finance/stocks", p.handleStocks)
    mux.HandleFunc("/api/finance/reports/", p.handleReports)
    return nil
}

func (p *Plugin) handleStocks(w http.ResponseWriter, r *http.Request) {
    logger := p.services.Logger("finance")

    stocks, err := p.fetchStockData()
    if err != nil {
        logger.Errorw("Failed to fetch stocks", "error", err)
        http.Error(w, "Failed to fetch stocks", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(stocks)
}
```

### Step 6: Register Plugin

**`cmd/qntx/main.go`**:
```go
import "github.com/teranos/QNTX/domains/finance"

func initializePluginRegistry() {
    registry := domains.NewRegistry("0.1.0")
    domains.SetDefaultRegistry(registry)

    // Register built-in plugins
    registry.Register(code.NewPlugin())
    registry.Register(finance.NewPlugin())  // Add new plugin
}
```

### Step 7: Create Configuration File

**`~/.qntx/am.finance.toml`**:
```toml
# Finance domain configuration

# API configuration
api.key = "${FINANCE_API_KEY}"  # Read from env
api.endpoint = "https://api.example.com"

# Analysis settings
analysis.update_interval_minutes = 60
analysis.cache_results = true
```

### Step 8: Test Plugin Integration

```bash
# Build
make

# Test CLI
./bin/qntx finance analyze AAPL

# Test HTTP (with server running)
curl http://localhost:877/api/finance/stocks

# Test initialization
./bin/qntx server
# Should see: "Finance domain plugin initialized"
```

## Testing Migrated Plugins

### Unit Tests

Test plugin in isolation:

```go
// domains/finance/plugin_test.go
func TestFinancePlugin_Initialize(t *testing.T) {
    db := qntxtest.CreateTestDB(t)
    logger := zaptest.NewLogger(t).Sugar()
    store := storage.NewSQLStore(db, logger)
    config := &mockConfig{}

    services := domains.NewServiceRegistry(db, logger, store, config)

    plugin := NewPlugin()
    err := plugin.Initialize(context.Background(), services)

    assert.NoError(t, err)
    assert.NotNil(t, plugin.services)
}
```

### Integration Tests

Test plugin with QNTX server:

```go
// server/server_test.go
func TestServer_WithFinancePlugin(t *testing.T) {
    db := qntxtest.CreateTestDB(t)
    server, err := NewQNTXServer(db, "test.db", 0)
    require.NoError(t, err)

    // Verify plugin loaded
    registry := domains.GetDefaultRegistry()
    plugin, ok := registry.Get("finance")
    assert.True(t, ok)
    assert.NotNil(t, plugin)
}
```

### HTTP Tests

Test HTTP endpoints:

```go
func TestFinancePlugin_StocksEndpoint(t *testing.T) {
    // Create test server with plugin
    mux := http.NewServeMux()
    plugin := NewPlugin()
    plugin.Initialize(ctx, services)
    plugin.RegisterHTTP(mux)

    // Test request
    req := httptest.NewRequest("GET", "/api/finance/stocks", nil)
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
}
```

## Rollback Strategy

If migration causes issues:

### Quick Rollback (Keep Old Code)

During migration, temporarily keep old code:

```
domains/finance/         # New plugin code
legacy/finance/          # Old code (temporary)
```

Build flags to toggle:
```go
//go:build !use_finance_plugin

// Use legacy finance code
```

### Git Revert

Migration should be in single PR:
```bash
git revert <migration-commit>
git push
```

### Feature Flag

Make plugin optional:
```toml
# am.toml
[plugins]
finance.enabled = false  # Disable plugin, use legacy code
```

## Best Practices

✅ **Atomic Migration**: Migrate entire domain at once (don't split across PRs)
✅ **Backward Compatibility**: Maintain same CLI/HTTP interfaces
✅ **Comprehensive Tests**: Test all plugin entry points
✅ **Configuration Migration**: Document config changes in migration guide
✅ **Gradual Rollout**: Test internally before external release

## Next Steps

After successful internal plugin migration:

1. **Validate**: Run in production for 1-2 weeks
2. **Document**: Create plugin-specific README
3. **Externalize**: Implement gRPC protocol (see ADR-001)
4. **Release**: Publish external plugin binary

## References

- [ADR-001: Domain Plugin Architecture](../architecture/ADR-001-domain-plugin-architecture.md)
- [Domain Plugin API Reference](./domain-plugin-api-reference.md)
- [Code Domain Plugin](../../domains/code/plugin.go) (reference implementation)
