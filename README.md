# [꩜](pulse/README.md)  [≡](am/README.md)  [⨳](ats/ix/README.md)  [⋈](ats/ax/README.md)  +  =  ✦  ⟶

[![Go Tests](https://github.com/teranos/QNTX/actions/workflows/go.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/go.yml)
[![Nix Image](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml)

**QNTX: Continuous Intelligence**

A new paradigm where systems continuously evolve their understanding through verifiable attestations. Always-on, always-learning - scaling from a Raspberry Pi to distributed clusters without configuration.

```
Data → Graph → Knowledge → Intelligence → Action
  ↑                                         ↓
  └───────── Continuous Learning ──────────┘
```

## What is QNTX?

QNTX provides a **domain-agnostic foundation** for building knowledge systems. At its core is the **[Attestation Type System (ATS)](ats/README.md)** - a way to track who said what, when, and in what context.

### Quick Example
```bash
# Record an attestation
qntx as USER-123 is member of TEAM-ENGINEERING

# Query attestations
qntx ax member of TEAM-ENGINEERING

# Continuous updates via Pulse
qntx pulse start  # Keeps your data current automatically
```

## Installation

See [Installation Guide](docs/installation.md) for all installation methods including Nix, Docker, and building from source.

## Segments

QNTX uses semantic symbols as a namespace system. See [GLOSSARY.md](docs/GLOSSARY.md) for complete definitions.

## Configuration

**QNTX works out of the box without configuration.** Sensible defaults are provided for all settings - you can start using QNTX immediately without creating any config files.

Configuration is managed by the `am` package, which provides:

- Multi-source config loading (system, user, project, environment variables)
- Backward compatibility with existing `config.toml` files
- Preference for `am.toml` (new format) over `config.toml`

Only create a configuration file if you need to override defaults:

```bash
# View current configuration (all defaults applied)
qntx am show

# Get a specific value
qntx am get database.path

# Validate configuration
qntx am validate
```

See the [am package documentation](am/README.md) for details on configuration structure and precedence.

## Testing

Run the full test suite:

```bash
make test
```
