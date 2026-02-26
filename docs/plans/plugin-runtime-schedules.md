# Plugin Runtime Schedule Management

**Status:** Planning
**Context:** ix-json plugin needs per-glyph polling schedules created at runtime, not just at init

## Problem

Plugins can only announce Pulse schedules during `Initialize` via `GetSchedules()`. This works for static schedules known at startup, but fails when schedules are user-driven:

- A user opens an ix-json glyph, configures an API URL, and clicks Activate
- That glyph needs its own Pulse schedule with its own interval
- Other glyphs of the same plugin type may have different URLs and intervals
- Schedules must be created/paused/deleted at runtime, not predicted at init

Currently the ix-json plugin works around this with internal goroutine tickers that enqueue one-shot Pulse jobs. This works but bypasses Pulse's schedule infrastructure — no visibility in the Pulse panel, no persistence across restarts, no state management.

## Proposal: ScheduleService gRPC API

Expose `schedule.Store` operations to gRPC plugins through a new service, following the same pattern as `ATSStoreService` and `QueueService`.

### Proto

```protobuf
service ScheduleService {
  rpc CreateSchedule(CreateScheduleRequest) returns (CreateScheduleResponse);
  rpc PauseSchedule(PauseScheduleRequest) returns (PauseScheduleResponse);
  rpc ResumeSchedule(ResumeScheduleRequest) returns (ResumeScheduleResponse);
  rpc DeleteSchedule(DeleteScheduleRequest) returns (DeleteScheduleResponse);
  rpc GetSchedule(GetScheduleRequest) returns (GetScheduleResponse);
}
```

### Plumbing (follows ATSStore/Queue pattern)

1. **`plugin/grpc/protocol/schedule.proto`** — service + messages
2. **`plugin/grpc/schedule_server.go`** — server wrapping `schedule.Store`
3. **`plugin/grpc/remote_schedule.go`** — client implementing plugin-side interface
4. **`plugin/grpc/services_manager.go`** — start ScheduleService, expose endpoint
5. **`plugin/grpc/protocol/domain.proto`** — add `schedule_endpoint` to `InitializeRequest`
6. **`plugin/grpc/remote_services.go`** — add schedule client to `RemoteServiceRegistry`
7. **`plugin/services.go`** — add `Schedule() ScheduleService` to `ServiceRegistry`

### Plugin interface

```go
type ScheduleService interface {
    Create(handlerName string, intervalSecs int, payload []byte, metadata map[string]string) (scheduleID string, err error)
    Pause(scheduleID string) error
    Resume(scheduleID string) error
    Delete(scheduleID string) error
}
```

### ix-json usage after implementation

Activate clicks:
```go
id, err := p.services.Schedule().Create("ix-json.poll", intervalSecs, payload, metadata)
// Store schedule ID in glyph's attestation for later pause/delete
```

Pause clicks:
```go
err := p.services.Schedule().Pause(scheduleID)
```

The goroutine ticker, `startPoller`, `stopPoller`, and `enqueuePollJob` all get deleted. Handler registration via `GetHandlerNames` stays — Pulse still needs to know where to route `ix-json.poll` jobs.

## What this unlocks

Any plugin can create recurring Pulse schedules at runtime based on user actions. The pattern applies beyond ix-json — any plugin with user-configured periodic work benefits.
