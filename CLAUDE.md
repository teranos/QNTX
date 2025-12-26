# QNTX Development Guide

## Working with this Project

**Read, don't infer:**
- Stick to what's explicitly stated in code and documentation
- Don't add features, explanations, or context that weren't requested
- If something is unclear, ask - don't assume or fill in gaps
- State only what you can directly verify

## Segments

QNTX uses segments across the project:

- **꩜** (Pulse) - Async operations, rate limiting, job processing
- **⌬** (Actor/Agent) - of Actors/agents in the attestation system
- **≡** (Configuration) - am Configuration and system settings
- **⨳** (Ingestion) - ix Data ingestion operations
- **⋈** (Join/Merge) - ax Entity merging and relationship operations
- **⊔** (Square Cup) - Database/storage; material retention substrate

**Note:** These symbols are defined in the `sym` package for consistent use across QNTX.

**For Claude**: Use these segments consistently when referencing system components.

## Configuration (am package)

**Core Principle: QNTX works out of the box without configuration.**

The `am` package ("I am" - core being/state) manages all QNTX configuration:

- **Location**: `github.com/teranos/QNTX/am`
- **Philosophy**: Sensible defaults for immediate use; configuration optional for customization
- **Precedence**: System < User < Project < Environment Variables
- **File naming**: Prefers `am.toml` (new) but supports `config.toml` (backward compat)
- **Scope**: Core infrastructure only (Pulse, REPL, Server, Code, LocalInference, Ax, Database)

### Key Design Decisions

- **Zero values have meaning**: `0` workers = disabled, `0` rate limit = unlimited
- **Empty is valid**: Empty database path defaults to `qntx.db`
- **Multi-source merge**: All config files merge; later sources override earlier ones
- **No domain entities**: Contact, Organization, Role belong in ExpGraph, not am

**For Claude**: When adding config options, ensure sensible defaults exist in `am/defaults.go`. Only require configuration when truly necessary.

## Go Development Standards

### Code Quality

- **Deterministic operations**: Use sorted map keys, consistent error patterns, predictable behavior

### Error Handling

- **Context in errors**: Errors should provide sufficient context for debugging
