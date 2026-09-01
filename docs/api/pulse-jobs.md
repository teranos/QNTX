# Pulse Jobs

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET | `/api/pulse/jobs` | HandlePulseJobs |
| GET | `/api/pulse/jobs/` | HandlePulseJob |

---

### `GET` /api/pulse/jobs

HandlePulseJobs handles requests to /api/pulse/jobs
GET: List all async jobs (active, completed, failed)

**Handler**: `HandlePulseJobs`

---

### `GET` /api/pulse/jobs/

HandlePulseJob handles requests to /api/pulse/jobs/{id}
GET: Get async job details
Sub-resources: /api/pulse/jobs/{id}/executions, /api/pulse/jobs/{id}/children, /api/pulse/jobs/{id}/stages, /api/pulse/jobs/{id}/tasks/:task_id/logs

**Handler**: `HandlePulseJob`

---

[← Back to API Index](./README.md)
