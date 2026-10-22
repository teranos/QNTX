-- A job is enqueued in one context and run later by a worker in another, so the
-- span that caused it is gone by the time it runs. These columns are how the
-- trace survives that gap, and they are two different questions.
--
-- trace_context is the job's cause: what to continue from, written where the
-- job is created and read where it is executed, so a child job's span lands in
-- its parent's trace instead of arriving as a root with nothing above it.
--
-- exec_trace_context is the job's own execution span, written when it starts
-- running. A child is often created in another process — a plugin calling back
-- over gRPC — where the parent's span is not reachable and only its id is. The
-- parent's row is where that child goes to find what to hang from, so the
-- parent has to have left it there.
--
-- All four are the headers Sentry propagates by, kept as their wire form rather
-- than parsed apart: the SDK writes them with ToSentryTrace and ToBaggage and
-- reads them with ContinueFromHeaders, and a column holding anything else would
-- have to be right about a format that is not ours.
--
-- Empty is a job with no cause to inherit, which is every job enqueued outside
-- a span and every row written before this migration.
ALTER TABLE async_ix_jobs ADD COLUMN trace_context TEXT NOT NULL DEFAULT '';
ALTER TABLE async_ix_jobs ADD COLUMN trace_baggage TEXT NOT NULL DEFAULT '';
ALTER TABLE async_ix_jobs ADD COLUMN exec_trace_context TEXT NOT NULL DEFAULT '';
ALTER TABLE async_ix_jobs ADD COLUMN exec_trace_baggage TEXT NOT NULL DEFAULT '';
