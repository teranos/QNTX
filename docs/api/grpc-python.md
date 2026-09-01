# PythonService gRPC API

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

PythonService executes Python code. Plugins declaring python_provider=true implement this service.

**Proto file**: [`plugin/grpc/protocol/python.proto`](https://github.com/teranos/QNTX/blob/main/plugin/grpc/protocol/python.proto)

## Service Methods

| Method | Request | Response | Streaming |
|--------|---------|----------|-----------|
| Execute | PythonExecuteRequest | PythonExecuteResponse | No |

### Execute

- **Request**: `PythonExecuteRequest`
- **Response**: `PythonExecuteResponse`

---

## Message Types

### PythonExecuteRequest

| Field | Type | Description |
|-------|------|-------------|
| code | string | - |
| glyph_id | string | - |
| upstream_attestation | bytes | - |

### PythonExecuteResponse

| Field | Type | Description |
|-------|------|-------------|
| success | bool | - |
| output | string | - |
| error | string | - |
| result | bytes | - |

[← Back to API Index](./README.md)
