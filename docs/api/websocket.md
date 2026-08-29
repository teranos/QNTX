# WebSocket Protocol

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

Real-time communication protocol for the QNTX web interface.

## Endpoints

| Path | Purpose |
|------|--------|
| `/ws` | Main WebSocket (graph updates, job status, logs) |

## Message Types

All messages are JSON objects with a `type` field indicating the message type.

### Server → Client

#### `job_update`

Async job status update

| Field | Description |
|-------|-------------|
| job | Job object with status, progress, etc. |
| metadata | Additional metadata about the update |

#### `daemon_status`

Pulse daemon status broadcast

| Field | Description |
|-------|-------------|
| active_jobs | Number of active jobs |
| load_percent | Current load percentage |
| running | Whether daemon is running |

#### `usage_update`

AI usage statistics update

| Field | Description |
|-------|-------------|
| requests | Number of requests |
| tokens | Total tokens used |
| total_cost | Total cost in USD |

#### `llm_stream`

Streaming LLM response chunks

| Field | Description |
|-------|-------------|
| content | Text content chunk |
| done | Whether streaming is complete |
| job_id | Associated job ID |

#### `plugin_health`

Plugin health status update

| Field | Description |
|-------|-------------|
| healthy | Health status |
| name | Plugin name |
| state | Plugin state (running/paused) |

## Type References

Full message type definitions are in `server/types.go`, and the shapes the browser reads in `web/types/websocket.ts`.

[← Back to API Index](./README.md)
