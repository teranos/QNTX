# ADR-037: The operational db

Date: 2026-09-12
Status: Accepted

## Context

A parquet deployment opens two stores, not one. Attestations go to S3; watchers, canvas, aliases, schedules, jobs and WebAuthn credentials go to a local SQLite db that `sqlitecgo.NewFileStore` migrates at boot. That db is already local and already in the process, and its migrations already build the index the read path wants: `attestation_subjects`, `attestation_actors`, `attestation_contexts` and `attestation_predicates`, with an `idx_junc_*` on each.

Reads of attestations do not use it. Every query calls `parquet_file_count()`, which is a live ListObjectsV2, then `read_parquet` over the glob, which opens every file under the prefix. There is no cache and no index on the four LIST columns a filter names.

The target is 2 attestations written per second and 1200 read per second, so 600 reads for every write. [ADR-024](ADR-024-parquet-storage-backend.md) records what the read path measures against that on the production node: a one-row query answered in 3 to 4 seconds.

## Decision

"operational db authoritative while running writes land there first"

"S3 as the record it's the rebuild source and protects against host loss, not process crash"

"reconcile-on-open by watermark is approved"

The operational db is a namespace's, not the node's. One file per namespace, and a namespace is the unit of failure:

"i want to be able to go crazy"
"on one namespace"
"and murder it"
"while the system keeps being okay"

Nothing here is built yet, and the index cannot be chosen until the read mix is known. `QueryFilter` admits three shapes — point lookups by id through `get_many`, filters on subjects, predicates, contexts or actors, and time ranges — and 1200 reads per second of each is a different index.

What bounds the store is this ADR's to answer, and it has not. [ADR-024](ADR-024-parquet-storage-backend.md) hands the question here: a store that is read from rather than archived to is bounded by what the node can hold, and whether that bound is distillation, eviction or a serving window belongs to whoever decides what the operational db is. Distillation is not free to pick. A sigma inherits the actors it folded, and [ADR-020](ADR-020-attestation-distillation.md) makes one that does not a 1.0.0 blocker — so choosing distillation here is choosing to do that first.

## Users, done first

"we need to keep users in mem"

"why not just implement the pending operational db work"

"do the user slice now"

Every gated request read the User from S3: one LIST and one GET per User, under one
lock for the node, once at the gate and once more for standing. Ten Users made that a
quarter second per request and four requests a second for the whole node. A User now
lives in a `users` table in the operational db, on every backend, the way passkeys do.
Writing a User writes the table first, then its S3 object on parquet; S3 is the record
for host loss, not for reading. The node reads S3 for Users once, when it opens: a User
S3 holds and the table lacks is taken in, and a User the table holds is the truth,
written back when the record differs. No `updated_at` exists on a User, so the record
itself is the watermark. No gated request touches S3 for a User.

## Attestations, sentence 1

"i want sentence 1 to be true"

An attestation written through this node lands in the operational db first, then in
S3. S3 is the record, not what is read. The operational db is one file for the node and
its attestations table carries no namespace, so the write lands in a file of the
namespace's own, `namespaces/<slug>.db` beside `qntx-operational.db`, opened when the
namespace is.

"and i would like sentence 2 to be true as well"

Every read is answered from that file, and none from S3. For that to hold, opening a
namespace reads the record once, from a mark on, and takes in what the file lacks. The
mark is how far the last take-in reached, kept beside the file in `<slug>.db.taken-in`
as one RFC3339 instant, written only after every row landed; a person deletes it to
make the next open take in the whole record. It is not the file's newest row: the
first landing files took writes for a quarter hour before they ever read the record,
and their newest row said nothing about what they lacked. No mark, or a file holding
nothing, is a first open and takes in the whole record. Each landing file keeps a WAL of
its own, checkpointed after a take-in and by the same pulse that checkpoints the
operational db; the first take-in left `default.db-wal` at 680 MB before that was so.

"thats an issue, fix now"

A landing file is the namespace's, so it goes when the namespace does: deleting a namespace
removes its landing file, its WAL and its mark, and a namespace made later under the name
starts empty. Closing a namespace waits for its last flush and closes every store it opened,
so a delete drains what that flush wrote.

"be smarter"

Opening a namespace is one open per name, and none of it holds the node: a take-in copies a
namespace's history, and default and system are served while it does.
The db glyph's numbers are
not back yet: its stats read `qntx-operational.db` through the one registered driver,
and the attestations now sit in the namespace's own file.

"we do store in s3 right? but like, how do i say, after a certain point, and not for all
data"

Everything is written to S3. The operational db is not for all data: it holds what is
recent, and after a certain point a row lives in S3 alone. What the point is, an age or
a count, is open, and nothing enforces one yet: the whole record today is 75 objects and
4.4 MB.

## Consequences

[ADR-023](ADR-023-storage-backend-selection.md) used to say a running QNTX has exactly one backend and forbid dual-backend operation. It now says parquet is optional persistence and the operational db keeps running either way, which is what made this ADR possible.

One file per namespace is what makes a namespace destroyable. Today `OpenNamespace` hands every universe the same `h.operational`, so schedules, canvas, embeddings, rich fields, executions, prompts, aliases and queries are one node-wide store that only a `WHERE` separates. Four things follow from splitting it, and none of them is a storage cost — the operational db is local SQLite, so this buys isolation without buying requests or objects:

- `muWrite` in `ats/storage/sqlitecgo/storage_cgo.go` is per-`RustStore`, and every write takes it. One store means one lock for the node, so a namespace holding it stalls every other namespace.
- Migrations run once against one file, so a schema that will not apply takes the node's boot rather than one namespace's.
- `canvas_glyphs` has `canvas_id` for subcanvas nesting and no namespace column at all, so one canvas is every namespace's canvas.
- Deleting a namespace becomes removing its file and its prefix, rather than a `DELETE` across a dozen tables that has to be right every time.

Canvas following the rectangle is three changes, not one. Splitting the db gives
each namespace its own canvas tables, and that is the only part this ADR does.
`CanvasHandler` holds a single `*CanvasStore` captured at construction
(`server/sub_canvas.go:24`) and never resolves one per request — it never sees
the admission, so it cannot ask which namespace — and it has to start asking the
way `storeFor` does. Then the reach lines change — glyphs, compositions,
minimized windows and the two exports are five lines and eight paths, all of
them ROOT's today:

"i want this:  the canvas becomes reachable to whoever the namespace is for"

Which makes a canvas the namespace's rather than ROOT's, and which one a caller
gets is the namespace they are standing in.

The db glyph gets its numbers back. `dimensionsDescribeTheCount` in `server/db_stats_cache.go` is set only when the count falls through to the operational tables, which on parquet it does not, so `unique_actors`, `unique_subjects`, `unique_contexts`, `distillation` and `predicate_histograms` are left out of the response rather than sent as zero. Attestations in the operational db set that flag, and the five return with no frontend change: `web/ts/db-glyph.ts` already reads an absent key as a backend that does not answer, and a zero as an answer of none.
