# ADR-028: Schedules

**Status:** Accepted
**Date:** 2026-08-06
**Related:** ADR-018 (Watcher Lifecycle), ADR-024 (Parquet Storage Backend)

## Context

> `scheduled_pulse_jobs` reads `YES NO` in `make parity`. No ADR covers schedules.
>
> A schedule kept running after the decorator that declared it was removed.

## Decision

"nothing ever gets removed bro, it gets superseded"

"make it YES YES"

> a watcher is cold declaration plus hot tally

"is OK"

> tally private stream

"is OK"

> Schedules take the shape watchers took: the declaration is cold and one
> object per schedule, the ticks are a private stream, and what changes on
> every tick is derived from it rather than stored.
