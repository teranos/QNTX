# Health & Status

<!-- Written by hand since typegen was removed. The Go source in server/ and
     the protos in plugin/grpc/protocol/ are what this describes. -->

| Method | Endpoint | Handler |
|--------|----------|----------|
| GET | `/api/debug` | HandleDebug |
| GET | `/api/dev` | HandleDevMode |
| GET | `/api/timeseries/usage` | HandleUsageTimeSeries |
| GET | `/logs/download` | HandleLogDownload |

---

### `GET` /api/debug

HandleDebug handles browser console debugging endpoint
POST: Add console log to buffer
GET: Retrieve all console logs from buffer

**Handler**: `HandleDebug`

---

### `GET` /api/dev

HandleDevMode returns whether the server is in dev mode (plain text: "true" or "false")

**Handler**: `HandleDevMode`

---

### `GET` /api/timeseries/usage

HandleUsageTimeSeries serves time-series usage data for charting

**Handler**: `HandleUsageTimeSeries`

---

### `GET` /logs/download

HandleLogDownload serves the log file for download.
Deprecated: log download UI has been removed. Scheduled for deletion.

**Handler**: `HandleLogDownload`

---

[← Back to API Index](./README.md)
