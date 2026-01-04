# Domain Plugin API Reference

Complete reference for the DomainPlugin interface and ServiceRegistry.

## Table of Contents

- [DomainPlugin Interface](#domainplugin-interface)
- [ServiceRegistry Interface](#serviceregistry-interface)
- [Plugin Lifecycle](#plugin-lifecycle)
- [HTTP Handlers](#http-handlers)
- [WebSocket Handlers](#websocket-handlers)
- [CLI Commands](#cli-commands)
- [Error Handling](#error-handling)

## DomainPlugin Interface

```go
package domains

import (
    "context"
    "net/http"

    "github.com/spf13/cobra"
)

type DomainPlugin interface {
    // Metadata returns plugin information
    Metadata() Metadata

    // Initialize prepares the plugin for use
    Initialize(ctx context.Context, services ServiceRegistry) error

    // Shutdown cleans up plugin resources
    Shutdown(ctx context.Context) error

    // Commands returns CLI commands for this domain
    Commands() []*cobra.Command

    // RegisterHTTP registers HTTP handlers for this domain
    RegisterHTTP(mux *http.ServeMux) error

    // RegisterWebSocket registers WebSocket handlers for this domain
    RegisterWebSocket() (map[string]WebSocketHandler, error)

    // Health returns current health status
    Health(ctx context.Context) HealthStatus
}
```

### Metadata

Returns plugin identification and version information.

```go
type Metadata struct {
    Name        string // Plugin name (lowercase, e.g., "code")
    Version     string // Plugin version (semver, e.g., "0.1.0")
    QNTXVersion string // Required QNTX version (semver constraint, e.g., ">= 0.1.0")
    Description string // Human-readable description
    Author      string // Author or organization
    License     string // License (e.g., "MIT", "Apache-2.0")
}
```

**Example**:
```go
func (p *Plugin) Metadata() domains.Metadata {
    return domains.Metadata{
        Name:        "code",
        Version:     "0.1.0",
        QNTXVersion: ">= 0.1.0",
        Description: "Software development domain (git, GitHub, gopls, code editor)",
        Author:      "QNTX Team",
        License:     "MIT",
    }
}
```

**Version Validation**:
- Registry validates `QNTXVersion` constraint using [Masterminds/semver](https://github.com/Masterminds/semver)
- Plugin fails to register if QNTX version doesn't satisfy constraint
- Use `>= X.Y.Z` for minimum version, `~X.Y.Z` for patch-level compatibility

### Initialize

Called once during server startup. Plugins receive services and prepare for use.

```go
func (p *Plugin) Initialize(ctx context.Context, services ServiceRegistry) error
```

**Parameters**:
- `ctx`: Cancellation context (server shutdown triggers cancellation)
- `services`: Interface to QNTX core services (DB, logger, config, ATS store)

**Responsibilities**:
1. Store `services` reference for later use
2. Load configuration via `services.Config()`
3. Initialize domain-specific resources (connections, caches, etc.)
4. Validate requirements (API tokens, workspace paths, etc.)

**Return**:
- `nil` on success
- `error` on failure (server will refuse to start - fail-fast policy)

**Example**:
```go
func (p *Plugin) Initialize(ctx context.Context, services ServiceRegistry) error {
    p.services = services
    logger := services.Logger("code")

    config := services.Config("code")
    workspace := config.GetString("gopls.workspace_root")
    if workspace == "" {
        workspace = "."
    }

    // Initialize gopls language server
    goplsService, err := gopls.NewService(gopls.Config{
        WorkspaceRoot: workspace,
        Logger:        logger,
    })
    if err != nil {
        return fmt.Errorf("failed to initialize gopls: %w", err)
    }

    if err := goplsService.Initialize(ctx); err != nil {
        return fmt.Errorf("gopls initialization failed: %w", err)
    }

    p.goplsService = goplsService
    logger.Info("Code domain plugin initialized")

    return nil
}
```

### Shutdown

Called during graceful server shutdown. Clean up resources.

```go
func (p *Plugin) Shutdown(ctx context.Context) error
```

**Parameters**:
- `ctx`: Shutdown deadline context (typically 30s timeout)

**Responsibilities**:
1. Close connections (DB clients, API connections)
2. Stop background goroutines
3. Flush pending writes
4. Release file handles

**Return**:
- `nil` on success
- `error` logged but shutdown continues

**Example**:
```go
func (p *Plugin) Shutdown(ctx context.Context) error {
    if p.services != nil {
        logger := p.services.Logger("code")
        logger.Info("Code domain plugin shutting down")
    }

    // Stop gopls language server
    if p.goplsService != nil {
        if err := p.goplsService.Shutdown(ctx); err != nil {
            return fmt.Errorf("gopls shutdown failed: %w", err)
        }
    }

    return nil
}
```

### Commands

Returns CLI commands for this domain. Commands are automatically registered under `qntx <domain>`.

```go
func (p *Plugin) Commands() []*cobra.Command
```

**Return**:
- Slice of `*cobra.Command` (typically one root command with subcommands)

**Conventions**:
- Root command: `qntx <domain>` (e.g., `qntx code`)
- Subcommands: `qntx <domain> <feature>` (e.g., `qntx code ix git`)

**Example**:
```go
func (p *Plugin) Commands() []*cobra.Command {
    // Root command for code domain
    codeCmd := &cobra.Command{
        Use:   "code",
        Short: "Software development tools",
        Long:  "Code domain provides git ingestion, GitHub integration, language servers, and code editing",
    }

    // IX subcommand group
    ixCmd := &cobra.Command{
        Use:   "ix",
        Short: "Data ingestion commands",
    }
    ixCmd.AddCommand(p.buildIxGitCommand())
    codeCmd.AddCommand(ixCmd)

    return []*cobra.Command{codeCmd}
}

func (p *Plugin) buildIxGitCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "git <repository-url>",
        Short: "Ingest git repository",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation
            return nil
        },
    }
}
```

### RegisterHTTP

Registers HTTP handlers for this domain. Routes must be namespaced under `/api/<domain>/`.

```go
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error
```

**Parameters**:
- `mux`: HTTP request multiplexer (plugin-specific, not global)

**Route Constraints**:
- All routes MUST start with `/api/<domain>/`
- Example: `/api/code/`, `/api/code/github/pr`
- **Enforcement**: Routes outside namespace will be rejected in future versions

**Return**:
- `nil` on success
- `error` if registration fails

**Example**:
```go
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
    // Code file tree and content
    mux.HandleFunc("/api/code", p.handleCodeTree)
    mux.HandleFunc("/api/code/", p.handleCodeContent)

    // GitHub integration
    mux.HandleFunc("/api/code/github/pr/", p.handlePRSuggestions)
    mux.HandleFunc("/api/code/github/pr", p.handlePRList)

    return nil
}

func (p *Plugin) handleCodeTree(w http.ResponseWriter, r *http.Request) {
    logger := p.services.Logger("code")

    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    tree, err := p.buildCodeTree()
    if err != nil {
        logger.Errorw("Failed to build code tree", "error", err)
        http.Error(w, "Failed to load code tree", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(tree)
}
```

**Security Best Practices**:
1. **Path validation**: Always validate user-provided paths
2. **Error logging**: Log errors with context before returning generic HTTP errors
3. **Dev mode checks**: Restrict dangerous operations (file writes) to dev mode

### RegisterWebSocket

Registers WebSocket handlers for real-time features.

```go
func (p *Plugin) RegisterWebSocket() (map[string]WebSocketHandler, error)
```

**Return**:
- `map[string]WebSocketHandler`: Path → handler mapping
- `error` if registration fails

**Example**:
```go
func (p *Plugin) RegisterWebSocket() (map[string]domains.WebSocketHandler, error) {
    handlers := make(map[string]domains.WebSocketHandler)

    // Language server protocol endpoint
    handlers["/gopls"] = p.handleGoplsWebSocket

    return handlers, nil
}

type WebSocketHandler func(conn *websocket.Conn) error

func (p *Plugin) handleGoplsWebSocket(conn *websocket.Conn) error {
    // Handle LSP messages via WebSocket
    return p.goplsService.HandleConnection(conn)
}
```

**Note**: WebSocket integration is planned but not yet wired into server (tracked in issue #127).

### Health

Returns current plugin health status for monitoring.

```go
func (p *Plugin) Health(ctx context.Context) HealthStatus
```

**Return**:
```go
type HealthStatus struct {
    Healthy bool                   // Overall health
    Message string                 // Human-readable status
    Details map[string]interface{} // Component-specific details
}
```

**Example**:
```go
func (p *Plugin) Health(ctx context.Context) domains.HealthStatus {
    details := make(map[string]interface{})

    // Check gopls service
    if p.goplsService != nil {
        if err := p.goplsService.Ping(ctx); err != nil {
            return domains.HealthStatus{
                Healthy: false,
                Message: "gopls service unavailable",
                Details: map[string]interface{}{
                    "gopls_error": err.Error(),
                },
            }
        }
        details["gopls"] = "ok"
    }

    // Check database access
    db := p.services.Database()
    if err := db.PingContext(ctx); err != nil {
        return domains.HealthStatus{
            Healthy: false,
            Message: "database connection failed",
            Details: map[string]interface{}{
                "db_error": err.Error(),
            },
        }
    }
    details["database"] = "ok"

    return domains.HealthStatus{
        Healthy: true,
        Message: "Code domain operational",
        Details: details,
    }
}
```

## ServiceRegistry Interface

Plugins access QNTX services via `ServiceRegistry` interface.

```go
type ServiceRegistry interface {
    Database() *sql.DB
    Logger(domain string) *zap.SugaredLogger
    Config(domain string) Config
    ATSStore() *storage.SQLStore
    Queue() *async.Queue
}
```

### Database()

Returns direct SQL database connection.

```go
func (s ServiceRegistry) Database() *sql.DB
```

**Use Cases**:
- Custom queries beyond ATS store
- Bulk operations
- Schema migrations (plugins can create their own tables)

**Example**:
```go
func (p *Plugin) storeCustomData(ctx context.Context, data string) error {
    db := p.services.Database()

    _, err := db.ExecContext(ctx, `
        INSERT INTO plugin_code_cache (key, value, created_at)
        VALUES (?, ?, ?)
    `, "cache_key", data, time.Now())

    return err
}
```

### Logger(domain string)

Returns domain-scoped structured logger.

```go
func (s ServiceRegistry) Logger(domain string) *zap.SugaredLogger
```

**Parameters**:
- `domain`: Plugin domain name (usually plugin's own name)

**Example**:
```go
logger := p.services.Logger("code")
logger.Infow("Processing repository", "url", repoURL, "branch", "main")
logger.Errorw("Failed to clone", "url", repoURL, "error", err)
```

### Config(domain string)

Returns domain-specific configuration.

```go
func (s ServiceRegistry) Config(domain string) Config

type Config interface {
    GetString(key string) string
    GetInt(key string) int
    GetBool(key string) bool
    GetStringSlice(key string) []string
    Get(key string) interface{}
    Set(key string, value interface{})
}
```

**Configuration File**: `am.<domain>.toml`

**Example**:
```go
config := p.services.Config("code")

workspace := config.GetString("gopls.workspace_root")  // Reads am.code.toml
enabled := config.GetBool("gopls.enabled")
maxSize := config.GetInt("editor.max_file_size_mb")
```

### ATSStore()

Returns attestation storage interface (CRUD for attestations).

```go
func (s ServiceRegistry) ATSStore() *storage.SQLStore
```

**Example**:
```go
store := p.services.ATSStore()

// Create attestation
attestation := &types.As{
    Actor:   "ixgest-git@user",
    Context: "repository_ingested",
    Entity:  "github.com/teranos/QNTX",
    Payload: json.RawMessage(`{"commit_count": 150}`),
}
err := store.Create(ctx, attestation)

// Query attestations
filter := &types.AxFilter{
    Context: ptr("repository_ingested"),
}
results, err := store.Query(ctx, filter)
```

### Queue()

Returns the Pulse async job queue for enqueueing background jobs.

```go
func (s ServiceRegistry) Queue() *async.Queue
```

**Use Cases**:
- Queue long-running operations (git ingestion, analysis tasks)
- Defer work to background workers with progress tracking
- Integrate with Pulse job system instead of direct database manipulation

**Example**:
```go
queue := p.services.Queue()

// Create job
job := &async.Job{
    ID:          fmt.Sprintf("job_%d", time.Now().UnixNano()),
    HandlerName: "ixgest.git",
    Payload:     payloadJSON,
    Source:      fmt.Sprintf("cli:ix-git:%s", repoURL),
    Status:      async.JobStatusQueued,
    Progress: async.Progress{
        Current: 0,
        Total:   100,
    },
    CreatedAt: time.Now(),
    UpdatedAt: time.Now(),
}

// Enqueue job via Pulse API
if err := queue.Enqueue(job); err != nil {
    return fmt.Errorf("failed to queue job: %w", err)
}
```

**Important**: Always use `Queue()` instead of direct SQL manipulation of `pulse_jobs` table. This ensures proper job lifecycle management, subscriber notifications, and integration with the Pulse system.

## Plugin Lifecycle

```
Server Startup:
  1. Registry.InitializeAll()
  2. For each plugin (sorted by name):
     a. plugin.Initialize(ctx, services)
     b. If error: panic (fail-fast)
  3. plugin.RegisterHTTP(mux)
  4. plugin.Commands() → registered with CLI

Server Running:
  - Plugins handle HTTP/WebSocket requests
  - Plugins execute CLI commands
  - Plugin errors broadcast to UI (no crash)

Server Shutdown:
  1. Registry.ShutdownAll()
  2. For each plugin (reverse order):
     a. plugin.Shutdown(ctx)
     b. Log errors but continue
```

## Error Handling

### Initialization Errors

```go
// Fail-fast: Return error, server won't start
func (p *Plugin) Initialize(ctx context.Context, services ServiceRegistry) error {
    if critical := p.validateCriticalConfig(); critical != nil {
        return fmt.Errorf("missing critical config: %w", critical)
    }

    // Graceful degradation: Log warning, feature disabled
    if optional := p.initOptionalFeature(); optional != nil {
        logger.Warnw("Optional feature disabled", "error", optional)
        p.optionalFeature = nil  // Feature remains disabled
    }

    return nil
}
```

### Runtime Errors

```go
// HTTP handler errors: Log with context, return generic error
func (p *Plugin) handleRequest(w http.ResponseWriter, r *http.Request) {
    logger := p.services.Logger("code")

    result, err := p.processRequest(r)
    if err != nil {
        logger.Errorw("Request processing failed",
            "path", r.URL.Path,
            "method", r.Method,
            "error", err,
        )
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(result)
}
```

## Complete Example

See [`domains/code/plugin.go`](../../domains/code/plugin.go) for full reference implementation.
