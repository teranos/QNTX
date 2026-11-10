# ats-dev bench

What the store takes and where it falls, as raw numbers. It drives
`ats-sqlite`'s `SqliteStore` the way the node's library path does and prints
CSV. Nothing is summarised.

```text
ats-dev --db FILE bench load N [--step S] [--batch K] [--replace]
ats-dev --db FILE bench probe N
ats-dev --db FILE bench query
```

- `load` writes N synthetic attestations into a fresh database and prints a line
  every S puts (default N/10). Without `--batch` each put commits by itself,
  which is `SqliteStore::put` as the library has it. `--batch K` puts K per
  transaction. It deletes the database file (and `-wal`, `-shm`), so it refuses
  one that exists unless you say `--replace`.
- `probe` does N per-put writes onto an existing database, then the queries.
- `query` runs the queries on an existing database.
- `--sync full|normal|off` sets `PRAGMA synchronous` first.

The workload is fixed: the same attestations in the same order on every run,
unsigned, `source = "bench"`, one subject/predicate/context/actor each (50
predicates, 20 contexts, 5 actors, N/10 subjects), two attributes, a timestamp a
second after the last. Two runs, two disks or two versions compare.

## Columns

`load`: `n_done, put_per_s, p50_ms, p99_ms, max_ms, db_MB, wal_MB,
write_MB_total, write_syscalls_total`. Latencies are per put (per transaction
with `--batch`). `write_MB_total` and `write_syscalls_total` are
`/proc/self/io` `write_bytes` and `syscw` since the start; `-` where there is no
`/proc`. `write_syscalls_total` counts write calls, not fsyncs; the shim below
counts fsyncs.

Queries: `label, rows, first_ms, second_ms, phys_read_KB_first,
read_syscalls_first`. The first run of each query is the "first"; this is cold
only if you made it so (`echo 3 > /proc/sys/vm/drop_caches` as root, before
`bench query`).

## A slower disk than the one you have

`fsyncdelay.c` makes every `fsync`/`fdatasync` cost `FSYNC_US` microseconds
(the real call still runs after) and counts them:

```sh
cc -shared -fPIC -O2 -o fsyncdelay.so crates/ats-dev/bench/fsyncdelay.c -ldl
LD_PRELOAD=$PWD/fsyncdelay.so FSYNC_US=8000 FSYNC_REPORT=1 \
  ats-dev --db /tmp/b.db bench load 20000 --replace
# fsyncdelay: fsync=<n> fdatasync=<n> delay_us=<n>   (on stderr, at exit)
```

It models one thing: how long a commit waits for the disk. A 5400 RPM platter
turns once in 60/5400 s = 11.1 ms, so a commit that waits for it costs on that
order; measure your disk's real fsync latency (`fio --fsync=1`, or
`dd if=/dev/zero of=f bs=4k count=1000 oflag=dsync`) and use that. It does not
model seeks, throughput or read latency: the read columns are what your own
disk did. It is Linux glibc `LD_PRELOAD` only.

The first open runs the migrations and costs fsyncs of its own (91 on 2026-11-10,
visible as the whole `fsync` count of `--sync off bench load 300`). Subtract that
before dividing by N.

## One run, to show the shape

In the sandbox this was written in (not a spinning disk), 300 autocommit puts:

| | fsyncs | puts/s | p50 |
|---|---|---|---|
| no delay | 1593 | 331 | 2.88 ms |
| `FSYNC_US=2000` | 1593 | 61 | 16.1 ms |
| `--sync off`, `FSYNC_US=2000` | 91 | 942 | 0.92 ms |

(1593 − 91) / 300 = 5.0 fsyncs per autocommit put. At `FSYNC_US=2000` that is 10
ms of the 16 ms p50.
