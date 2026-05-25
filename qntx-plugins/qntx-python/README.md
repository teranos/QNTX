# Pyre

Python runtime engine for [QNTX](https://github.com/teranos/QNTX). Embeds Python via PyO3 in a Rust gRPC process.

Migrating to [teranos/pyre](https://github.com/teranos/pyre).

## Why

Full control over the Python execution environment. The Rust binary is the chassis — identical code for all Python plugins. Nix is the configuration surface: each domain gets its own `withPackages` set, its own process, its own port. Same binary, same gRPC protocol, different Python environments.

The `@watch` decorator and handler discovery give Python scripts first-class participation in the attestation pipeline without writing Rust or Go.

See [ADR-022](https://github.com/teranos/QNTX/blob/main/docs/adr/ADR-022-python-as-plugin-provided-service.md) and [Python Plugin User Guide](https://github.com/teranos/QNTX/blob/main/docs/development/python-plugin.md).

## What it does

- Executes Python code, expressions, and files via gRPC/HTTP
- `attest()` built-in for creating attestations from Python
- Discovers handlers from ATS (predicate=handler, context=plugin-name)
- `@watch` decorator — handlers fire automatically on upstream attestations
- Package management via uv with pip fallback
- Captures stdout/stderr and variable extraction

## Building

```bash
# Build release binary
make rust-python

# Run tests
make rust-python-test

# Check code style
make rust-python-check

# Install to ~/.qntx/plugins/
make rust-python-install
```

## Usage

### Standalone

```bash
# Start plugin on default port 9000
qntx-python-plugin

# Start on custom port
qntx-python-plugin --port 9001

# With debug logging
qntx-python-plugin --log-level debug
```

### With QNTX

Add to your `am.toml`:

```toml
[plugin]
enabled = ["python"]
```

## HTTP Endpoints

### POST /execute

Execute Python code.

```json
{
  "code": "print('Hello, World!')",
  "timeout_secs": 30,
  "capture_variables": false
}
```

Response:
```json
{
  "success": true,
  "stdout": "Hello, World!\n",
  "stderr": "",
  "result": null,
  "error": null,
  "duration_ms": 5
}
```

### POST /evaluate

Evaluate a Python expression and return its value.

```json
{
  "expr": "1 + 2 * 3"
}
```

Response:
```json
{
  "success": true,
  "result": 7,
  "duration_ms": 1
}
```

### POST /execute-file

Execute a Python file.

```json
{
  "path": "/path/to/script.py",
  "capture_variables": false
}
```

### GET /version

Get Python and plugin version info.

```json
{
  "python_version": "3.13.0",
  "plugin_version": "0.3.7"
}
```

## Configuration

The plugin accepts configuration via the `config` map in the `InitializeRequest`:

- `python_paths`: Colon-separated list of additional Python paths to add to `sys.path`

## Architecture

- **PyO3**: Rust bindings to Python, providing safe embedded Python execution
- **tonic**: gRPC framework for Rust
- **tokio**: Async runtime for concurrent request handling

The plugin implements the standard QNTX `DomainPluginService` interface, allowing it to be discovered and managed by the QNTX core like any other domain plugin.

## Requirements

- Python 3.8+ installed on the system (3.13 recommended via Nix)
- Rust 1.70+ for building
- protoc for proto compilation during build

**Note:** The Nix build (`make rust-python`) provides Python 3.13 deterministically and is the recommended build method. Plain `cargo build` may have Python linking issues depending on your system.
