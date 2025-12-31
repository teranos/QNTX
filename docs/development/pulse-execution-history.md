# Pulse Execution History

> **Related**: [`pulse-async-ix.md`](../architecture/pulse-async-ix.md) - Core Pulse architecture

## Overview

Execution history tracking for Pulse scheduled jobs, enabling users to view past job runs with logs, timing data, and status information.

## Features

- **Execution Records**: Track every scheduled job run with unique execution ID
- **Timing Data**: Start/completion timestamps and duration in milliseconds
- **Status Tracking**: Running, completed, failed states
- **Log Capture**: Store execution logs for debugging
- **Error Tracking**: Capture error messages on failures
- **Result Summaries**: Brief overview of execution results

## Database Schema

### `pulse_executions` Table

```sql
CREATE TABLE IF NOT EXISTS pulse_executions (
    id TEXT PRIMARY KEY,                    -- ASID format: PX_{random}_{timestamp}
    scheduled_job_id TEXT NOT NULL,         -- FK to scheduled_jobs(id)
    async_job_id TEXT,                      -- Optional FK to async_jobs(id)

    -- Execution status
    status TEXT NOT NULL,                   -- 'running', 'completed', 'failed'

    -- Timing
    started_at TEXT NOT NULL,               -- RFC3339 timestamp
    completed_at TEXT,                      -- RFC3339 timestamp (null if running)
    duration_ms INTEGER,                    -- Milliseconds (null if running)

    -- Output capture
    logs TEXT,                              -- Captured stdout/stderr
    result_summary TEXT,                    -- Brief summary
    error_message TEXT,                     -- Error if failed

    -- Metadata
    created_at TEXT NOT NULL,               -- RFC3339 timestamp
    updated_at TEXT NOT NULL,               -- RFC3339 timestamp

    FOREIGN KEY (scheduled_job_id) REFERENCES scheduled_jobs(id)
);

CREATE INDEX idx_pulse_executions_job ON pulse_executions(scheduled_job_id);
CREATE INDEX idx_pulse_executions_status ON pulse_executions(status);
CREATE INDEX idx_pulse_executions_started ON pulse_executions(started_at);
```

## API Endpoints

### List Executions

```http
GET /api/pulse/jobs/{job_id}/executions?limit=50&offset=0&status=completed
```

**Response:**
```json
{
  "executions": [
    {
      "id": "PX_abc123_1733450123",
      "scheduled_job_id": "SPJ_1733450000",
      "status": "completed",
      "started_at": "2025-12-06T02:00:00Z",
      "completed_at": "2025-12-06T02:00:12Z",
      "duration_ms": 12340,
      "result_summary": "Processed successfully",
      "error_message": null
    }
  ],
  "count": 1,
  "total": 47,
  "has_more": true
}
```

> **Type Reference**: See [Execution](../types/schedule.md#execution) and [ListExecutionsResponse](../types/server.md#listexecutionsresponse) type definitions.

### Get Execution Details

```http
GET /api/pulse/executions/{execution_id}
```

**Response:**
```json
{
  "id": "PX_abc123_1733450123",
  "status": "completed",
  "started_at": "2025-12-06T02:00:00Z",
  "completed_at": "2025-12-06T02:00:12Z",
  "duration_ms": 12340,
  "logs": "[2025-12-06T02:00:00Z] Starting job\n[2025-12-06T02:00:12Z] Completed",
  "result_summary": "Processed successfully"
}
```

### Get Execution Logs

```http
GET /api/pulse/executions/{execution_id}/logs
```

**Response:** Plain text logs

## Implementation

### Ticker Integration

Applications using Pulse should integrate execution tracking in their ticker:

```go
// Create execution record before job runs
execution := &models.PulseExecution{
    ID:             id.GenerateExecutionID(),
    ScheduledJobID: job.ID,
    Status:         "running",
    StartedAt:      time.Now().Format(time.RFC3339),
}
db.CreatePulseExecution(execution)

// Execute job
result, err := executeJob(job)

// Update execution with results
execution.CompletedAt = timePtr(time.Now())
execution.DurationMs = intPtr(durationMs)

if err != nil {
    execution.Status = "failed"
    execution.ErrorMessage = stringPtr(err.Error())
} else {
    execution.Status = "completed"
    execution.ResultSummary = stringPtr(result.Summary)
}

db.UpdatePulseExecution(execution)
```

### Storage Layer

**Files:**
- `internal/pulse/schedule/execution_store.go` - Execution persistence
- `internal/models/pulse_execution.go` - Execution model

**Key Methods:**
- `CreatePulseExecution(exec *PulseExecution) error`
- `UpdatePulseExecution(exec *PulseExecution) error`
- `GetPulseExecution(id string) (*PulseExecution, error)`
- `ListPulseExecutions(jobID string, limit, offset int, status string) ([]*PulseExecution, int, error)`

### API Handlers

**File:** `internal/server/pulse_execution_handlers.go`

Implements REST endpoints for:
- Listing executions with pagination and filtering
- Getting execution details
- Retrieving execution logs

## Frontend Integration

### TypeScript Types

```typescript
export type ExecutionStatus = "running" | "completed" | "failed";

export interface PulseExecution {
  id: string;
  scheduled_job_id: string;
  async_job_id?: string;
  status: ExecutionStatus;
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
  result_summary?: string;
  error_message?: string;
}
```

### API Client

```typescript
export async function listExecutions(
  jobId: string,
  limit = 50,
  offset = 0,
  status?: ExecutionStatus
): Promise<ListExecutionsResponse>

export async function getExecutionLogs(
  executionId: string
): Promise<string>
```

### UI Components

**Job Detail Panel** - Displays execution history:
- Execution list with status badges
- Duration tracking
- Result summaries
- Expandable log viewer

**Styling:**
- Status color coding (running: purple, completed: green, failed: red)
- Relative timestamps
- Responsive pagination

## Real-time Updates

### WebSocket Messages

Applications can broadcast execution status changes:

```typescript
// Execution started
{
  "type": "pulse_execution_started",
  "execution_id": "PX_abc123",
  "job_id": "SPJ_123"
}

// Execution completed
{
  "type": "pulse_execution_completed",
  "execution_id": "PX_abc123",
  "job_id": "SPJ_123",
  "duration_ms": 12340
}

// Execution failed
{
  "type": "pulse_execution_failed",
  "execution_id": "PX_abc123",
  "job_id": "SPJ_123",
  "error": "Operation failed"
}
```

## Testing

### Unit Tests

- Execution store CRUD operations
- API handler pagination and filtering
- Ticker integration (execution record lifecycle)
- Duration tracking accuracy
- Error capture validation

### Integration Tests

- Complete job execution with history tracking
- Failed execution error message capture
- WebSocket broadcast verification
- Log retrieval with large outputs

## Future Enhancements

- **Retention Policy**: Auto-delete executions older than N days
- **Execution Statistics**: Success rate, average duration, trends
- **Manual Re-run**: Trigger immediate execution from history
- **Export**: Download execution history as CSV/JSON
- **Alerting**: Notifications on execution failures

## Related Documentation

- **Core Pulse Architecture**: [`pulse-async-ix.md`](../architecture/pulse-async-ix.md)
- **Task Logging**: [`task-logging.md`](task-logging.md) - Log capture mechanism
- **Application Integration**: See application-specific documentation for implementation examples
