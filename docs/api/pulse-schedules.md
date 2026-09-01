# Pulse Schedules

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET, POST | `/api/pulse/schedules` | HandlePulseSchedules |
| GET, PATCH, DELETE | `/api/pulse/schedules/` | HandlePulseSchedule |

---

### `GET | POST` /api/pulse/schedules

HandlePulseSchedules handles requests to /api/pulse/schedules
GET: List all schedules
POST: Create a new schedule

**Handler**: `HandlePulseSchedules`

---

### `GET | PATCH | DELETE` /api/pulse/schedules/

HandlePulseSchedule handles requests to /api/pulse/schedules/{id}
GET: Get schedule details
PATCH: Update schedule (pause/resume/change interval)
DELETE: Remove schedule

**Handler**: `HandlePulseSchedule`

---

[← Back to API Index](./README.md)
