# [꩜](pulse/README.md)  [≡](am/README.md)  [⨳](ats/ix/README.md)  [⋈](ats/ax/README.md)  +  =  ✦  ⟶

[![Go Tests](https://github.com/teranos/QNTX/actions/workflows/go.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/go.yml)
[![Rust](https://github.com/teranos/QNTX/actions/workflows/rs.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/rs.yml)
[![Nix Image](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml)

## What is QNTX?

QNTX provides a **domain-agnostic foundation** for building knowledge systems. At its core is the **[Attestation Type System (ATS)](ats/README.md)** - a way to track who said what, when, and in what context. For architecture and philosophy, see [Understanding QNTX](docs/understanding-qntx.md). For the full architectural overview, see [Architecture (arc42)](docs/arc42.md). For symbol definitions, see [GLOSSARY.md](docs/GLOSSARY.md).

## Installation

See [Installation Guide](docs/installation.md) for all installation methods including Nix, Docker, and building from source.

## Configuration

**QNTX works out of the box without configuration.** See [am package](am/README.md) for details on multi-source configuration and precedence.

## Testing

```bash
# first make wasm
make wasm
# go and typescript, fast tests during development.
make test
```
