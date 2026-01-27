# QNTX Python Plugin

A Rust-based gRPC plugin for executing Python code within QNTX.

## Why Rust + gRPC

Go-to-Python integration via CGo is fragile and version-dependent. This plugin runs Python in a separate process to avoid:

- Python version coupling with QNTX builds
- Interpreter crashes affecting the main process
- FFI incompatibilities across systems

Uses PyO3 for memory-safe Python embedding and Nix for reproducible Python environments.

See [External Plugin Development Guide](https://github.com/teranos/QNTX/blob/main/docs/development/external-plugin-guide.md) for plugin architecture details and [Python Plugin User Guide](https://github.com/teranos/QNTX/blob/main/docs/development/python-plugin.md) for API usage examples.

## Features

- Execute Python code via gRPC/HTTP
- Evaluate Python expressions
- Execute Python files
- Capture stdout/stderr output
- `attest()` function for creating attestations from Python code

### Planned Features

- uv-based package management (not yet implemented)
- Module availability checking (not yet implemented)

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
