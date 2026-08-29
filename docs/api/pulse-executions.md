# Pulse Executions

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET | `/api/pulse/executions/` | HandlePulseExecution |

---

### `GET` /api/pulse/executions/

HandlePulseExecution handles requests for individual execution
GET /api/pulse/executions/{execution_id}
GET /api/pulse/executions/{execution_id}/logs

**Handler**: `HandlePulseExecution`

---

[← Back to API Index](./README.md)
