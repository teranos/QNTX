# am - QNTX Core Configuration

**"I am"** - Core being/state configuration for the QNTX attestation system.

## Philosophy

### Naming Etymology

Part of QNTX's semantic naming philosophy:
- **`as`** - "as I am" (attestation assertions about being)
- **`ax`** - "ask" (queries about being)
- **`ix`** - "index/ingest" (intake of being)
- **`am`** - "I am" (configuration of being)

The `am` package defines the foundational state - the core "being" that enables attestation flow.

### Configuration as Being

Configuration is not mere settings - it defines the system's state of existence. The database path, server ports, async job workers - these determine *how the system is*. Attestations flow through this configured being, queries execute within this state, ingestion happens through this infrastructure.

**QNTX as I am being configured** - the `am` package defines that configured existence.

## Core vs Domain Separation

**What belongs in `am`:**
- Infrastructure: database, server, async jobs (Pulse)
- Core interfaces: REPL, code review, attestation queries (ax)
- Generic capabilities: local LLM inference, HTTP rate limiting

**What does NOT belong:**
- Domain entities: contacts, organizations, roles, candidates
- Business logic: scoring algorithms, matching weights
- External integrations: social media APIs, cloud services
- Application-specific features: recruitment workflows, event management

The `am` package is domain-agnostic infrastructure. Applications extend it for their needs.

## Configuration Sections

- **`database`** - SQLite storage path and settings
- **`server`** - Web UI ports, CORS origins, log themes
- **`pulse`** - Async job workers, scheduling, HTTP rate limiting
- **`repl`** - Interactive REPL search, display, timeouts, history
- **`code`** - Code review system (GitHub, gopls integration)
- **`local_inference`** - Local LLM support (Ollama, LocalAI)
- **`ax`** - Attestation query defaults

See `am.go` for complete type definitions.

## Configuration Precedence

Configuration loads in order (lowest to highest priority):

1. System config: `/etc/qntx/am.toml` (or `config.toml`)
2. User config: `~/.qntx/am.toml` (or `config.toml`)
3. Project config: `./am.toml` (or `config.toml`) - searches parent directories
4. Environment variables: `QNTX_*` prefix (e.g., `QNTX_DATABASE_PATH`)

Sensitive values (API keys, tokens) should use environment variables.

## Extending in Applications

Applications extend `am.Config` for domain-specific needs through dual loading:

1. Load QNTX core config (`am.Load()`)
2. Load application domain config (custom loader)
3. Merge at runtime into unified config structure

This preserves clean separation: QNTX provides infrastructure, applications add domain logic. Both can read from the same `config.toml` file - `am` extracts core keys, applications extract domain keys. Unknown keys are ignored for compatibility.

## Backward Compatibility

Works with existing `config.toml` files unchanged:
- Reads from both `am.toml` (new) and `config.toml` (backward compat)
- Ignores unknown keys (domain fields won't break am)
- Same TOML structure, just extracts core subset

## Related Packages

- **[ats](../ats/)** - Attestation system (uses am for database config)
- **[logger](../logger/)** - Structured logging (uses am for theme config)
- **[sym](../sym/)** - Unicode symbols (am symbol: ≡)
- **[am/geotime](./geotime/)** - Geographic/timezone utilities for configuration defaults

**Note**: The `geotime` package provides timezone and location utilities for configuration. Geographic defaults (like "Europe/Amsterdam") belong in `am/geotime`, not hardcoded in core config - this maintains domain-agnostic design while providing geographic capabilities when needed.
