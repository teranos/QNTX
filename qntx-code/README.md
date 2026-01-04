# QNTX Code Domain Plugin

**Status:** Bundled (temporary) - Will be extracted to separate repository

Domain plugin providing software development tools for QNTX.

## Features

- **Git Ingestion** (`ixgest/git/`) - Ingest git repository history into attestation system
- **GitHub Integration** (`vcs/github/`) - PR review, fix suggestions, workflow integration
- **Language Server** (`langserver/gopls/`) - Go code intelligence via gopls
- **AST Transformations** (`ast/`) - Code transformation and analysis

## HTTP Endpoints

- `/api/code` - Code file tree
- `/api/code/` - File content
- `/api/code/github/pr` - PR suggestions

## WebSocket Endpoints

- `/gopls` - Language server protocol

## Future

This plugin will be extracted to its own repository:
- Repository: `github.com/teranos/qntx-code`
- Binary: `qntx-code-plugin`
- Installation: `~/.qntx/plugins/qntx-code-plugin`

See [External Plugin Development Guide](../docs/development/external-plugin-guide.md)
