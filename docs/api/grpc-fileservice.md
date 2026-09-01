# FileService gRPC API

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

FileService provides file access for gRPC plugins. Plugins use this to read files stored on the core server's filesystem.

**Proto file**: [`plugin/grpc/protocol/fileservice.proto`](https://github.com/teranos/QNTX/blob/main/plugin/grpc/protocol/fileservice.proto)

## Service Methods

| Method | Request | Response | Streaming |
|--------|---------|----------|-----------|
| ReadFileBase64 | ReadFileRequest | ReadFileResponse | No |

### ReadFileBase64

ReadFileBase64 reads a stored file and returns its MIME type and base64-encoded content.

- **Request**: `ReadFileRequest`
- **Response**: `ReadFileResponse`

---

## Message Types

### ReadFileRequest

| Field | Type | Description |
|-------|------|-------------|
| auth_token | string | - |
| file_id | string | - |

### ReadFileResponse

| Field | Type | Description |
|-------|------|-------------|
| success | bool | - |
| error | string | - |
| mime_type | string | - |
| base64_data | string | - |

[← Back to API Index](./README.md)
