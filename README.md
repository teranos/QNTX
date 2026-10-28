# [+](ats/README.md)  [=](docs/predicates.md)  [꩜](pulse/README.md)  [≡](internal/config/README.md)  [⋈](ats/ax/README.md)  ⟶

[![Go Tests](https://github.com/teranos/QNTX/actions/workflows/go.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/go.yml)
[![Rust](https://github.com/teranos/QNTX/actions/workflows/rs.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/rs.yml)
[![TypeScript](https://github.com/teranos/QNTX-Web/actions/workflows/ts.yml/badge.svg)](https://github.com/teranos/QNTX-Web/actions/workflows/ts.yml)
[![Nix Image](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml/badge.svg)](https://github.com/teranos/QNTX/actions/workflows/nix-image.yml)

## Installation

See [Installation Guide](docs/installation.md) for all installation methods including Nix, Docker, and building from source.

## Configuration

See [config package](internal/config/README.md) for details on multi-source configuration and precedence.

## Testing

```bash
# first make ats
make ats
# go, fast tests during development.
make test
```

The frontend is [QNTX-Web](https://github.com/teranos/QNTX-Web); the phone and desktop shell is [QNTX-App](https://github.com/teranos/QNTX-App).

Works with:

- [Nix](https://nixos.org/)
- [Obsidian](https://obsidian.md/)
- [Sentry](https://sentry.io/)
- [AWS](https://aws.amazon.com/)
- [GitHub](https://github.com/)
- [Claude](https://claude.com/product/claude-code)
- [Codex](https://github.com/openai/codex)
- [Agy](https://antigravity.google/)
- [Jev](https://typesafe.ai/blog/introducing-system-one-models-and-jev)
- [Pi](https://pi.dev/)
- [OpenRouter](https://openrouter.ai/)
- [Supabase](https://supabase.com/)
- [DuckDB](https://duckdb.org/)
- [ground](https://github.com/teranos/ground)
- [elements](https://github.com/teranos/elements)
