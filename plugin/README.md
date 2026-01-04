# QNTX Plugin System

Infrastructure for extending QNTX with domain-specific functionality through plugins.

## Overview

The plugin system allows QNTX to be extended with domain-specific features (code analysis, financial modeling, legal document processing, etc.) without coupling them to the core system.

## Architecture

- **`interface.go`** - `DomainPlugin` interface that all plugins implement
- **`registry.go`** - Plugin registration, initialization, and lifecycle management
- **`services.go`** - `ServiceRegistry` providing plugins access to core QNTX services (database, logger, config, ATS, Pulse queue)
- **`grpc/`** - gRPC transport layer for external (out-of-process) plugins

## Plugin Types

### Built-in Plugins (temporary)
Currently `qntx-code/` is bundled at the repository root for convenience during development. This will be extracted to a separate repository (`github.com/teranos/qntx-code`) and become an external plugin.

### External Plugins (future)
Separate repositories that communicate via gRPC. Loaded dynamically at runtime from binaries in `~/.qntx/plugins/`.

Examples:
- `github.com/teranos/qntx-code` - Code domain (git, GitHub, gopls)
- `github.com/teranos/qntx-finance` - Financial analysis domain
- Community plugins

## Design Philosophy

**QNTX core is minimal.** Domain-specific functionality lives in plugins, keeping the core focused on:
- Attestation system (ATS)
- Configuration (am)
- Database (db)
- Async job processing (Pulse)
- Query system (ax)

Plugins handle everything else: code analysis, GitHub integration, language servers, domain-specific visualizations, etc.

## Documentation

- [Domain Plugin API Reference](../docs/development/domain-plugin-api-reference.md)
- [External Plugin Development Guide](../docs/development/external-plugin-guide.md)
- [Migrating to Plugins](../docs/development/migrating-to-plugins.md)
