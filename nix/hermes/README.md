# Hermes runtime provisioning

Hermes v0.22.5 is pinned in `server/parity/hermes_0.22.5_bf0203a/`.
The QNTX flake exposes `packages.<system>.hermes` for Linux x86_64,
Linux aarch64, and macOS aarch64.

```sh
nix build .#hermes
./result/bin/hermes --help
```

This consumes NousResearch/hermes-agent at commit
`bf0203a794a07aac3ff764972ab9087baa4e4f4e`, using its upstream
`flake.lock`, `uv.lock` and Nix packaging (`packages.<system>.minimal`).
It is **not** the Pyre Python execution provider; QNTX's agent-runtime
integration, session lifecycle, and authentication are separate work.

The upstream flake has its own pinned nixpkgs and Python interpreter;
QNTX's Python 3.13 PyO3 environment is deliberately not reused.
Nix builds may fetch and compile upstream native dependencies.

Validation needed in CI or on a Nix host:
- `nix flake show --no-write-lock-file`
- `nix build .#hermes`
- `./result/bin/hermes --help`

These commands have not been run in the authoring environment (Nix unavailable).
