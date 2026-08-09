# [+](ats/README.md)  [=](docs/predicates.md)  [꩜](pulse/README.md)  [≡](internal/config/README.md)  [⋈](ats/ax/README.md)  ⟶

[![Go Tests](https://github.com/teranos/QNTX/actions/workflows/go.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/go.yml)
[![Rust](https://github.com/teranos/QNTX/actions/workflows/rs.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/rs.yml)
[![TypeScript](https://github.com/teranos/QNTX/actions/workflows/ts.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/ts.yml)
[![Nix Image](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml)

## Installation

See [Installation Guide](docs/installation.md) for all installation methods including Nix, Docker, and building from source.

## Configuration

**QNTX works out of the box without configuration.** See [config package](internal/config/README.md) for details on multi-source configuration and precedence.

## Testing

```bash
# first make ats
make ats
# go and typescript, fast tests during development.
make test
```
