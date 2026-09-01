# Configuration

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET, POST, PATCH | `/api/config` | HandleConfig |

---

### `GET | POST | PATCH` /api/config

HandleConfig serves configuration endpoint
Supports GET (retrieve config) and POST/PATCH (update config)
Query parameters:
  - ?introspection=true - Returns detailed config with sources

**Handler**: `HandleConfig`

---

[← Back to API Index](./README.md)
