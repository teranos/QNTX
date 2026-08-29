# Prose (Documents)

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET | `/api/prose` | HandleProse |
| GET, PUT | `/api/prose/` | HandleProseContent |

---

### `GET` /api/prose

HandleProse returns the prose content tree structure

**Handler**: `HandleProse`

---

### `GET | PUT` /api/prose/

HandleProseContent returns the content of a specific prose file

**Handler**: `HandleProseContent`

---

[← Back to API Index](./README.md)
