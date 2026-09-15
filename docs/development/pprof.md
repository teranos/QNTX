# pprof

`net/http/pprof` runs on its own listener, bound to `127.0.0.1` whatever `bind_address` is, on `server.pprof_port` (default 8771). Set it to 0 and there is no listener; the node logs that profiling is off.

A reverse proxy makes every public request arrive from 127.0.0.1, so the separate port is what tells a stranger from the machine itself.

| Endpoint | Use |
|----------|-----|
| `/debug/pprof/goroutine?debug=2` | Full goroutine stacks with mutex wait times — primary deadlock diagnostic |
| `/debug/pprof/mutex` | Mutex contention profile |
| `/debug/pprof/heap` | Memory allocation profile |
| `/debug/pprof/profile?seconds=30` | CPU profile (30s sample) |

**Example**: to diagnose a hanging ATS store, hit `http://127.0.0.1:{pprof_port}/debug/pprof/goroutine?debug=2` and look for goroutines blocked on mutex acquisition.

### Mutex watchdog

The RustStore shared mutex (`ats/storage/sqlitecgo/storage_cgo.go`) serializes all SQLite access. A leaked transaction or slow CGO call can hold this mutex indefinitely, deadlocking all attestation operations.

A background goroutine periodically attempts to acquire the mutex with a timeout. If acquisition takes longer than the threshold, it logs a warning with the current goroutine stacks. This provides early warning before a full deadlock develops.
