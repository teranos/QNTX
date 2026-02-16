# Pulse Subsystem Survey — February 2026

Post-recluster-schedule integration. Covers documentation gaps, architectural observations, and concrete improvement areas.

## System Map

```
pulse/
  async/      Job execution engine (queue, workers, budget gates, handler registry)
  schedule/   Recurring job scheduler (store, ticker @ 1s, execution tracking)
  budget/     Cost enforcement (daily/weekly/monthly limits, rate limiter)
  progress.go Domain-agnostic progress interface

server/pulse_*.go    REST API (schedules, jobs, executions, stages, task logs)
web/ts/pulse/        Frontend (22 modules — panels, cards, real-time handlers)
```

**ID conventions:** `SP_*` = scheduled job, `JB_*` = async job, execution IDs link the two.

**Data flow:** Schedule ticker → creates async job → worker picks up → handler executes → execution record + task_logs written → poller broadcasts completion → WebSocket → UI updates.

## Documentation Gaps

### Missing entirely
- **Top-level Pulse guide** — no single doc explaining how async jobs, scheduled jobs, executions, handlers, and budget relate to each other. New contributors must read 5+ docs and infer the connections.
- **Frontend architecture** — `web/ts/pulse/` has 22 files, no README. Module boundaries, data flow, and real-time subscription model undocumented.
- **Stages and tasks concept** — `task_logs` table has `stage`/`task_id` columns used throughout the UI but nowhere defines what these represent.
- **Handler registration walkthrough** — scattered across plugin integration docs but no end-to-end example of "how to add a new Pulse handler."

### Incomplete
- **API docs** (`docs/api/pulse-*.md`) — auto-generated stubs with endpoint names only. No request/response schemas, no examples.
- **Glossary** — Pulse entry is one line ("Async operations, always prefix Pulse-related logs"). Doesn't distinguish the three layers.

### Stale or proposal-only
- `pulse-resource-coordination.md` — design proposal (Issue #50), not implemented. Should be marked as such.

## Code Observations

### Frontend: 3 different fetch patterns
- `active-queue.ts` uses `apiFetch()` (central wrapper with backend URL)
- `api.ts` uses raw `fetch()` with `getBaseUrl()`
- `execution-api.ts` uses `safeFetch()` (custom error classification)

Each has different error handling, timeout, and retry behavior. Should converge on one pattern.

### Frontend: duplicated utilities
`formatDuration()` is defined in both `panel.ts` and `execution-api.ts` with identical logic, imported by different consumers. `buildExecutionError()` in `schedules.ts` reimplements error classification that `execution-api.ts` also does.

### Backend: store instantiated per request
`s.newScheduleStore()` / `s.newExecutionStore()` called ~13 times across pulse handlers. Lightweight but unnecessary — could be a field on `QNTXServer`.

### Force trigger bypasses rate limiter
Manual triggers via REST API go straight to the queue without rate limit check. Only budget is checked. The worker checks rate limits before execution, but the job is already queued.

### Execution poller: fixed 3s interval, no jitter
`pulse_execution_poller.go` polls every 3 seconds with no jitter. Multiple servers could poll simultaneously. No backoff for expensive queries on large databases.

### Schedule creation: no handler existence validation
Creating a schedule with a nonexistent handler silently succeeds. Fails at execution time with "handler not found" — could be caught at creation.

## Open TODOs in Code

| Location | Issue | Description |
|----------|-------|-------------|
| `ticker.go:120` | #478 | Health check: sync tree size vs attestation count |
| `worker.go:479` | #70 | System load gate (3rd gate before job execution) |
| `grace_test.go:143,489` | #71 | Executor injection during WorkerPool creation |
| `migration 003` | — | Rename `created_from_doc_id` → `created_from` |
| `realtime-handlers.ts:16` | #30 | Execution progress, cancellation, batch updates |
| `async.ts`, `schedule.ts` | — | Migrate generated types to proto generation |

## Quick Wins

1. **Unify fetch pattern** — pick `apiFetch` or `safeFetch`, use everywhere
2. **Extract shared formatting** — `formatDuration`, `formatRelativeTime`, `escapeHtml` into one utility module
3. **Store as server field** — instantiate schedule/execution stores once, not per request
4. **Validate handler at schedule creation** — check registry before persisting
5. **Add jitter to poller** — `3s ± 500ms` prevents synchronized polling
