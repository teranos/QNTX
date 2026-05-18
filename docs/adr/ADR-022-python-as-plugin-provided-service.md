# ADR-022: Python as Plugin-Provided Service

## Status

Implemented

## Context

Python execution was hardcoded to a single plugin named "python". The watcher engine checked `if p == "python"` at boot to register the "py" glyph type, then executed Python code via HTTP POST to `/api/python/execute`. This coupled the execution path to a specific plugin name and used HTTP instead of gRPC — unlike every other plugin-provided service (LLM, search, embedding).

The real value of Python integration is the ecosystem — the libraries available in the runtime, not the language itself. A single monolithic Python plugin that bundles all packages is the wrong abstraction. Different domains need different Python environments: bioinformatics needs biopython/dnachisel/numpy, data analysis needs pandas/scipy, ML needs torch/transformers. These are separate concerns that belong in separate plugins.

## Decision

### PythonService gRPC

Add `PythonService` following the established provider pattern (ADR-014, ADR-015, ADR-017). A plugin declares `python_provider = true` in `InitializeResponse` and implements `PythonService.Execute` via gRPC. QNTX discovers the capability dynamically and routes "py" glyph execution to the provider — no name checks.

### Protocol

`python.proto`:

```proto
service PythonService {
  rpc Execute(PythonExecuteRequest) returns (PythonExecuteResponse);
}

message PythonExecuteRequest {
  string code = 1;
  string glyph_id = 2;
  bytes upstream_attestation = 3;
}

message PythonExecuteResponse {
  bool success = 1;
  string output = 2;
  string error = 3;
  bytes result = 4;
}
```

`domain.proto`: `bool python_provider = 9` on `InitializeResponse`.

### Core side

- `PythonExecutor` interface on the watcher engine, satisfied by `grpcPythonExecutor` (gRPC client wrapper)
- `AddPythonProvider(client)` stores the `PythonServiceClient` on the server, registers the "py" glyph type, and sets the watcher executor
- `onPythonProviderReady` callback in plugin discovery, same pattern as embedding/search
- Server-side `/api/python/execute` handler bridges the frontend's HTTP POST to the provider's gRPC `PythonService.Execute` — decouples the frontend from the plugin name
- Plugin HTTP handlers (`/api/{name}/execute`, `/api/{name}/pip/*`, etc.) remain available for direct plugin access

### Plugin side

- `PythonPluginService` implements both `DomainPluginService` and `PythonService`
- `--name` flag controls plugin identity (metadata, handler prefixes, ATS context)
- Binary name derivation: `qntx-{name}-plugin` → `{name}` as default

### Specialized Python plugins via Nix

The Rust binary (`qntx-python-plugin`) is the chassis — identical code for all Python plugins. The Nix flake is the configuration surface. Each specialized plugin is a separate Nix derivation that:

1. Fetches `qntx-python-plugin` source from the QNTX repo
2. Builds it against a curated `python313.withPackages` environment
3. Renames the binary to `qntx-{name}-plugin`
4. Declares `python_provider: true` with its own name

Example — a bioinformatics plugin:

```nix
pythonWithPackages = python313.withPackages (ps: with ps; [
  dnachisel numpy biopython
]);
```

A different plugin for data analysis:

```nix
pythonWithPackages = python313.withPackages (ps: with ps; [
  pandas scipy matplotlib
]);
```

Same Rust binary, same gRPC protocol, different Python environments. Each runs as a separate process with its own port. QNTX sees them as independent plugins.

### Development workflow

- `nix build` — hermetic build for deployment, recompiles all crates (~20 min)
- `nix develop` + `cargo build` — incremental compilation for iteration (~seconds after first build)

## Consequences

- Python execution is a capability, not a name — any plugin can provide it
- Multiple Python plugins can coexist with different library sets
- New Python environments are defined entirely in Nix — no Rust changes needed
- Same hot-reload semantics as all other plugins: edit am.toml, plugin restarts
- Interactive execution (py glyph run button) goes through server-side handler → gRPC; watcher execution goes through `PythonExecutor` → gRPC. Both paths are plugin-name-agnostic
- PyO3 pins Python to 3.13 (nix-provided); system Python version is irrelevant

## Open question: multiple python providers

`AddPythonProvider` is last-writer-wins — a single `pythonClient` on the server, a single `PythonExecutor` on the watcher engine. The Nix specialization pattern enables multiple Python plugins with different library sets, but the "py" glyph can only route to one provider at a time. If multiple plugins declare `python_provider = true`, which one handles execution?

Options not yet decided:
- **Typed glyph variants** (`py:bio`, `py:data`) — each variant routes to a specific provider
- **Per-glyph provider selection** — the glyph stores which provider it targets
- **Priority/ordering** — first or last registered wins, configured in am.toml
- **Single provider constraint** — enforce exactly one python provider, reject duplicates

This doesn't need resolution now (there's one provider), but will when a second Python plugin appears.
