# 「[Sonnet 5.5] ats-dev bench」

「[Sonnet 5.5] What the store takes and where it falls, as raw numbers. It drives」
「[Sonnet 5.5] `ats-sqlite`'s `SqliteStore` the way the node's library path does and prints」
「[Sonnet 5.5] CSV. Nothing is summarised.」

```text
「[Sonnet 5.5] ats-dev --db FILE bench load N [--step S] [--batch K] [--replace]」
「[Sonnet 5.5] ats-dev --db FILE bench probe N」
「[Sonnet 5.5] ats-dev --db FILE bench query」
```

- 「[Sonnet 5.5] `load` writes N synthetic attestations into a fresh database and prints a line」
  「[Sonnet 5.5] every S puts (default N/10). Without `--batch` each put commits by itself,」
  「[Sonnet 5.5] which is `SqliteStore::put` as the library has it. `--batch K` puts K per」
  「[Sonnet 5.5] transaction. It deletes the database file (and `-wal`, `-shm`), so it refuses」
  「[Sonnet 5.5] one that exists unless you say `--replace`.」
- 「[Sonnet 5.5] `probe` does N per-put writes onto an existing database, then the queries.」
  「[Sonnet 5.5] Its rows continue the load's series (ids, timestamps, subject pool), but are」
  「[Sonnet 5.5] drawn from a fresh random stream, so after `load 2000` + `probe 20` the」
  「[Sonnet 5.5] time-window and actor queries return what `load 2020` returns, and the」
  「[Sonnet 5.5] subject and predicate queries return rows of the same kind, not the same」
  「[Sonnet 5.5] count (17 against 12 for `by_subject(rare)` in one measurement).」
- 「[Sonnet 5.5] `query` runs the queries on an existing database.」
- 「[Sonnet 5.5] `--sync full|normal|off` sets `PRAGMA synchronous` first.」

「[Sonnet 5.5] The workload is fixed: the same attestations in the same order on every run,」
「[Sonnet 5.5] unsigned, `source = "bench"`, one subject/predicate/context/actor each (50」
「[Sonnet 5.5] predicates, 20 contexts, 5 actors, N/10 subjects), two attributes, a timestamp a」
「[Sonnet 5.5] second after the last. Two runs, two disks or two versions compare.」

## 「[Sonnet 5.5] Columns」

「[Sonnet 5.5] `load`: `n_done, put_per_s, p50_ms, p99_ms, max_ms, db_MB, wal_MB,」
「[Sonnet 5.5] write_MB_total, write_syscalls_total`. Latencies are per put (per transaction」
「[Sonnet 5.5] with `--batch`). `write_MB_total` and `write_syscalls_total` are」
「[Sonnet 5.5] `/proc/self/io` `write_bytes` and `syscw` since the start; `-` where there is no」
「[Sonnet 5.5] `/proc`. `write_syscalls_total` counts write calls, not fsyncs; the shim below」
「[Sonnet 5.5] counts fsyncs.」

「[Sonnet 5.5] Queries: `label, rows, first_ms, second_ms, phys_read_KB_first,」
「[Sonnet 5.5] read_syscalls_first`. The first run of each query is the "first"; this is cold」
「[Sonnet 5.5] only if you made it so (`echo 3 > /proc/sys/vm/drop_caches` as root, before」
「[Sonnet 5.5] `bench query`).」

## 「[Sonnet 5.5] A slower disk than the one you have」

「[Sonnet 5.5] `fsyncdelay.c` makes every `fsync`/`fdatasync` cost `FSYNC_US` microseconds」
「[Sonnet 5.5] (the real call still runs after) and counts them:」

```sh
「[Sonnet 5.5] cc -shared -fPIC -O2 -o fsyncdelay.so crates/ats-dev/bench/fsyncdelay.c -ldl」
「[Sonnet 5.5] LD_PRELOAD=$PWD/fsyncdelay.so FSYNC_US=8000 FSYNC_REPORT=1 \」
  「[Sonnet 5.5] ats-dev --db /tmp/b.db bench load 20000 --replace」
# 「[Sonnet 5.5] fsyncdelay: fsync=<n> fdatasync=<n> delay_us=<n>   (on stderr, at exit)」
```

「[Sonnet 5.5] It models one thing: how long a commit waits for the disk. A 5400 RPM platter」
「[Sonnet 5.5] turns once in 60/5400 s = 11.1 ms, so a commit that waits for it costs on that」
「[Sonnet 5.5] order; measure your disk's real fsync latency (`fio --fsync=1`, or」
「[Sonnet 5.5] `dd if=/dev/zero of=f bs=4k count=1000 oflag=dsync`) and use that. It does not」
「[Sonnet 5.5] model seeks, throughput or read latency: the read columns are what your own」
「[Sonnet 5.5] disk did. It is Linux glibc `LD_PRELOAD` only.」

「[Sonnet 5.5] The first open runs the migrations and costs fsyncs of its own (91 on 2026-11-10,」
「[Sonnet 5.5] visible as the whole `fsync` count of `--sync off bench load 300`). Subtract that」
「[Sonnet 5.5] before dividing by N.」

## 「[Sonnet 5.5] One run, to show the shape」

「[Sonnet 5.5] In the sandbox this was written in (not a spinning disk), 300 autocommit puts:」

| | fsyncs | puts/s | p50 |
|---|---|---|---|
| no delay | 1593 | 331 | 2.88 ms |
| `FSYNC_US=2000` | 1593 | 61 | 16.1 ms |
| `--sync off`, `FSYNC_US=2000` | 91 | 942 | 0.92 ms |

「[Sonnet 5.5] (1593 − 91) / 300 = 5.0 fsyncs per autocommit put. At `FSYNC_US=2000` that is 10」
「[Sonnet 5.5] ms of the 16 ms p50.」
