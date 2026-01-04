# External Plugin Development Guide

This guide explains how to develop external QNTX domain plugins that run as separate processes and communicate via gRPC.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Plugin Structure](#plugin-structure)
- [gRPC Protocol](#grpc-protocol)
- [Building and Deploying](#building-and-deploying)
- [Testing](#testing)
- [Best Practices](#best-practices)

## Overview

There is **one plugin interface**: `DomainPlugin`. Both built-in and external plugins implement the same interface:

- **Built-in plugins**: Implement `DomainPlugin` directly (e.g., `code.Plugin`)
- **External plugins**: `ExternalDomainProxy` implements `DomainPlugin` by proxying gRPC calls to a sidecar process

From the Registry's perspective, there is no difference. This enables:

- **Process isolation**: Plugin crashes don't crash QNTX
- **Language agnostic**: External plugins can be written in any gRPC-compatible language
- **Independent deployment**: Update plugins without rebuilding QNTX
- **Unified API**: Same interface for all plugins

## Architecture

```
                        ┌─────────────────────────────────────┐
                        │           domains.Registry          │
                        │   (treats all plugins identically)  │
                        └─────────────────────────────────────┘
                                         │
                    ┌────────────────────┼────────────────────┐
                    │                    │                    │
                    ▼                    ▼                    ▼
         ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
         │   code.Plugin    │  │ finance.Plugin   │  │ExternalDomainProxy│
         │   (built-in)     │  │   (built-in)     │  │    (adapter)      │
         │                  │  │                  │  │                   │
         │ implements       │  │ implements       │  │ implements        │
         │ DomainPlugin     │  │ DomainPlugin     │  │ DomainPlugin      │
         └──────────────────┘  └──────────────────┘  └─────────┬─────────┘
                                                               │ gRPC
                                                               ▼
                                                     ┌──────────────────┐
                                                     │  External Plugin │
                                                     │ (sidecar process)│
                                                     │                  │
                                                     │ PluginServer     │
                                                     │ wraps DomainPlugin│
                                                     └──────────────────┘
```

The `ExternalDomainProxy` is simply an adapter that:
1. Implements `DomainPlugin` interface
2. Translates method calls to gRPC requests
3. Sends requests to a sidecar process running `PluginServer`

## Quick Start

### 1. Create Plugin Directory

```bash
mkdir my-plugin
cd my-plugin
go mod init github.com/myorg/qntx-myplugin
```

### 2. Add Dependencies

```bash
go get github.com/teranos/QNTX/domains
go get github.com/teranos/QNTX/domains/grpc
go get google.golang.org/grpc
```

### 3. Implement DomainPlugin Interface

```go
// plugin.go
package main

import (
    "context"
    "net/http"

    "github.com/spf13/cobra"
    "github.com/teranos/QNTX/domains"
)

type MyPlugin struct {
    services domains.ServiceRegistry
}

func NewMyPlugin() *MyPlugin {
    return &MyPlugin{}
}

func (p *MyPlugin) Metadata() domains.Metadata {
    return domains.Metadata{
        Name:        "myplugin",
        Version:     "1.0.0",
        QNTXVersion: ">= 0.1.0",
        Description: "My custom QNTX plugin",
        Author:      "Your Name",
        License:     "MIT",
    }
}

func (p *MyPlugin) Initialize(ctx context.Context, services domains.ServiceRegistry) error {
    p.services = services
    logger := services.Logger("myplugin")
    logger.Info("MyPlugin initialized")
    return nil
}

func (p *MyPlugin) Shutdown(ctx context.Context) error {
    return nil
}

func (p *MyPlugin) Commands() []*cobra.Command {
    return []*cobra.Command{
        {
            Use:   "myplugin",
            Short: "My plugin commands",
            Run: func(cmd *cobra.Command, args []string) {
                cmd.Println("Hello from MyPlugin!")
            },
        },
    }
}

func (p *MyPlugin) RegisterHTTP(mux *http.ServeMux) error {
    mux.HandleFunc("/api/myplugin/hello", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello from MyPlugin!"))
    })
    return nil
}

func (p *MyPlugin) RegisterWebSocket() (map[string]domains.WebSocketHandler, error) {
    return nil, nil
}

func (p *MyPlugin) Health(ctx context.Context) domains.HealthStatus {
    return domains.HealthStatus{
        Healthy: true,
        Message: "MyPlugin is healthy",
    }
}
```

### 4. Create Main Entry Point

```go
// main.go
package main

import (
    "context"
    "flag"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    plugingrpc "github.com/teranos/QNTX/domains/grpc"
    "go.uber.org/zap"
)

var port = flag.Int("port", 9000, "gRPC server port")

func main() {
    flag.Parse()

    logger, _ := zap.NewProduction()
    sugar := logger.Sugar()
    defer logger.Sync()

    plugin := NewMyPlugin()
    server := plugingrpc.NewPluginServer(plugin, sugar)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigChan
        cancel()
    }()

    addr := fmt.Sprintf(":%d", *port)
    sugar.Infow("Starting plugin", "address", addr)

    if err := server.Serve(ctx, addr); err != nil {
        sugar.Fatalw("Server error", "error", err)
    }
}
```

### 5. Build and Run

```bash
go build -o qntx-myplugin .
./qntx-myplugin --port 9001
```

### 6. Register with QNTX

The `PluginManager` loads external plugins and returns `DomainPlugin` instances that can be registered with the Registry:

```go
// In QNTX server initialization
manager := grpc.NewPluginManager(logger)

// Load external plugins from config
configs := []grpc.PluginConfig{
    {Name: "myplugin", Enabled: true, Address: "localhost:9001"},
}
manager.LoadPlugins(ctx, configs)

// Get plugins as DomainPlugin instances and register them
for _, plugin := range manager.GetAllPlugins() {
    registry.Register(plugin)  // Same API as built-in plugins
}
```

Or configure via `~/.qntx/am.plugins.toml`:

```toml
[[plugins]]
name = "myplugin"
enabled = true
address = "localhost:9001"
```

For auto-start:

```toml
[[plugins]]
name = "myplugin"
enabled = true
binary = "qntx-myplugin"
auto_start = true
```

## Plugin Structure

### Recommended Directory Layout

```
qntx-myplugin/
├── main.go              # Entry point with gRPC server
├── plugin.go            # DomainPlugin implementation
├── commands.go          # CLI command definitions
├── handlers.go          # HTTP handler implementations
├── go.mod
├── go.sum
├── README.md
└── config/
    └── am.myplugin.toml # Default configuration
```

### DomainPlugin Interface

Every plugin must implement:

```go
type DomainPlugin interface {
    Metadata() Metadata
    Initialize(ctx context.Context, services ServiceRegistry) error
    Shutdown(ctx context.Context) error
    Commands() []*cobra.Command
    RegisterHTTP(mux *http.ServeMux) error
    RegisterWebSocket() (map[string]WebSocketHandler, error)
    Health(ctx context.Context) HealthStatus
}
```

## gRPC Protocol

The gRPC protocol is defined in `domains/grpc/protocol/domain.proto`:

```protobuf
service DomainPluginService {
    rpc Metadata(Empty) returns (MetadataResponse);
    rpc Initialize(InitializeRequest) returns (Empty);
    rpc Shutdown(Empty) returns (Empty);
    rpc Commands(Empty) returns (CommandsResponse);
    rpc ExecuteCommand(ExecuteCommandRequest) returns (ExecuteCommandResponse);
    rpc HandleHTTP(HTTPRequest) returns (HTTPResponse);
    rpc HandleWebSocket(stream WebSocketMessage) returns (stream WebSocketMessage);
    rpc Health(Empty) returns (HealthResponse);
}
```

### HTTP Proxying

HTTP requests to `/api/<plugin-name>/*` are forwarded to the plugin via `HandleHTTP`:

1. QNTX receives HTTP request
2. Request is serialized to `HTTPRequest` protobuf
3. Sent to plugin via gRPC
4. Plugin processes and returns `HTTPResponse`
5. QNTX writes response to client

### Command Execution

CLI commands are executed via `ExecuteCommand`:

1. User runs `qntx <plugin> <subcommand>`
2. QNTX sends `ExecuteCommandRequest` with args/flags
3. Plugin executes command
4. Returns stdout/stderr/exit code

## Building and Deploying

### Building

```bash
# Build for current platform
go build -o qntx-myplugin .

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o qntx-myplugin-linux .
```

### Installing

Install to the QNTX plugins directory:

```bash
mkdir -p ~/.qntx/plugins
cp qntx-myplugin ~/.qntx/plugins/
chmod +x ~/.qntx/plugins/qntx-myplugin
```

### Configuration

Create plugin configuration at `~/.qntx/am.myplugin.toml`:

```toml
# MyPlugin configuration
api_key = "${MYPLUGIN_API_KEY}"
endpoint = "https://api.example.com"
cache_ttl_seconds = 300
```

## Testing

### Unit Tests

```go
func TestMyPlugin_Initialize(t *testing.T) {
    plugin := NewMyPlugin()

    // Create mock service registry
    logger := zaptest.NewLogger(t).Sugar()
    services := &mockServiceRegistry{logger: logger}

    err := plugin.Initialize(context.Background(), services)
    assert.NoError(t, err)
}
```

### Integration Tests

Use the provided test helpers:

```go
func TestPluginIntegration(t *testing.T) {
    logger := zaptest.NewLogger(t).Sugar()
    plugin := NewMyPlugin()
    server := plugingrpc.NewPluginServer(plugin, logger)

    // Start server on random port
    listener, _ := net.Listen("tcp", "localhost:0")
    defer listener.Close()

    grpcServer := grpc.NewServer()
    protocol.RegisterDomainPluginServiceServer(grpcServer, server)
    go grpcServer.Serve(listener)
    defer grpcServer.Stop()

    // Connect client
    client, err := plugingrpc.NewPluginClient(listener.Addr().String(), logger)
    require.NoError(t, err)

    // Test plugin via client
    meta := client.Metadata()
    assert.Equal(t, "myplugin", meta.Name)
}
```

## Best Practices

### Error Handling

- Return descriptive errors from `Initialize` (causes QNTX to fail-fast)
- Log errors before returning them
- Use structured logging with context

```go
func (p *MyPlugin) Initialize(ctx context.Context, services domains.ServiceRegistry) error {
    logger := services.Logger("myplugin")

    if err := p.connectToAPI(); err != nil {
        logger.Errorw("Failed to connect to API", "error", err)
        return fmt.Errorf("API connection failed: %w", err)
    }

    return nil
}
```

### Health Checks

Implement meaningful health checks:

```go
func (p *MyPlugin) Health(ctx context.Context) domains.HealthStatus {
    details := make(map[string]interface{})

    // Check API connection
    if err := p.api.Ping(ctx); err != nil {
        return domains.HealthStatus{
            Healthy: false,
            Message: "API unreachable",
            Details: map[string]interface{}{
                "api_error": err.Error(),
            },
        }
    }
    details["api"] = "connected"

    return domains.HealthStatus{
        Healthy: true,
        Message: "All systems operational",
        Details: details,
    }
}
```

### HTTP Route Namespacing

All routes must be under `/api/<plugin-name>/`:

```go
func (p *MyPlugin) RegisterHTTP(mux *http.ServeMux) error {
    // ✅ Correct: namespaced routes
    mux.HandleFunc("/api/myplugin/", p.handleRoot)
    mux.HandleFunc("/api/myplugin/data", p.handleData)

    // ❌ Wrong: will conflict with other plugins
    // mux.HandleFunc("/data", p.handleData)

    return nil
}
```

### Graceful Shutdown

Handle shutdown signals properly:

```go
func (p *MyPlugin) Shutdown(ctx context.Context) error {
    logger := p.services.Logger("myplugin")

    // Stop background workers
    if p.worker != nil {
        p.worker.Stop()
    }

    // Close connections
    if p.apiClient != nil {
        if err := p.apiClient.Close(); err != nil {
            logger.Warnw("API client close error", "error", err)
        }
    }

    logger.Info("Plugin shutdown complete")
    return nil
}
```

### Version Compatibility

Specify QNTX version constraints:

```go
func (p *MyPlugin) Metadata() domains.Metadata {
    return domains.Metadata{
        Name:        "myplugin",
        Version:     "1.0.0",
        QNTXVersion: ">= 0.1.0, < 2.0.0", // Semver constraint
        // ...
    }
}
```

## Reference Implementation

See the code domain plugin for a complete reference:

- **Source**: `cmd/plugins/code/main.go`
- **Plugin**: `domains/code/plugin.go`
- **Build**: `make plugins`

## Related Documentation

- [ADR-001: Domain Plugin Architecture](../architecture/ADR-001-domain-plugin-architecture.md)
- [ADR-002: Plugin Configuration](../architecture/ADR-002-plugin-configuration.md)
- [ADR-003: Plugin Communication](../architecture/ADR-003-plugin-communication.md)
- [Domain Plugin API Reference](./domain-plugin-api-reference.md)
- [Migrating to Plugins](./migrating-to-plugins.md)
