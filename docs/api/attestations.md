# Attestations

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET, POST | `/api/attestations` | HandleAttestations |

---

### `GET | POST` /api/attestations

HandleAttestations routes GET (query) and POST (create) for /api/attestations.
GET returns attestations matching optional filters (JSON array).
Query parameters:
  - ?subject=x    — filter by subject(s), comma-separated
  - ?predicate=y  — filter by predicate(s), comma-separated
  - ?context=z    — filter by context(s), comma-separated
  - ?actor=a      — filter by actor(s), comma-separated
  - ?source=s     — filter by source (exact match, e.g. "cli", "distill")
  - ?limit=N      — max results (default 100, max 1000)

**Handler**: `HandleAttestations`

---

[← Back to API Index](./README.md)
