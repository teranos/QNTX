//! Flight recorder for FFI boundary observability.
//!
//! Records the last FFI operation per thread to a file so it survives crashes.
//! Does NOT install signal handlers — Go's runtime handles signals
//! and produces the goroutine dump. We just ensure the context is
//! available in a file after the crash.
//!
//! Each thread gets its own file: `{db}.flight.{thread_id}`
//! so concurrent operations don't overwrite each other.
//!
//! Caller attribution: Go sets a thread-local caller tag via
//! `flight_recorder_set_caller` before issuing FFI calls. The tag
//! is included in every recorded entry so crashes can be traced
//! back to the originating component (watcher engine, plugin, Pulse, etc).

use std::cell::RefCell;
use std::fs::OpenOptions;
use std::io::Write;
use std::sync::OnceLock;
use std::time::Instant;

static RECORDER_DIR: OnceLock<String> = OnceLock::new();
static START_TIME: OnceLock<Instant> = OnceLock::new();

thread_local! {
    static THREAD_FILE: RefCell<Option<String>> = const { RefCell::new(None) };
    static CALLER: RefCell<[u8; 64]> = const { RefCell::new([0u8; 64]) };
    static CALLER_LEN: RefCell<usize> = const { RefCell::new(0) };
}

/// Set the base path for flight recorder files.
/// Called once at store initialization.
pub fn init(base_path: &str) {
    let _ = RECORDER_DIR.set(base_path.to_string());
    let _ = START_TIME.set(Instant::now());
}

/// Set the caller tag for the current thread.
/// Called from Go before issuing FFI calls.
pub fn set_caller(caller: &str) {
    CALLER.with(|c| {
        let mut buf = c.borrow_mut();
        let len = caller.len().min(64);
        buf[..len].copy_from_slice(&caller.as_bytes()[..len]);
        CALLER_LEN.with(|cl| *cl.borrow_mut() = len);
    });
}

fn get_caller() -> Option<String> {
    CALLER_LEN.with(|cl| {
        let len = *cl.borrow();
        if len == 0 {
            return None;
        }
        CALLER.with(|c| {
            let buf = c.borrow();
            Some(String::from_utf8_lossy(&buf[..len]).into_owned())
        })
    })
}

fn get_thread_file() -> Option<String> {
    THREAD_FILE.with(|tf| {
        let mut tf = tf.borrow_mut();
        if tf.is_none() {
            if let Some(base) = RECORDER_DIR.get() {
                let tid = std::thread::current().id();
                let path = format!("{}.{:?}", base, tid);
                *tf = Some(path);
            }
        }
        tf.clone()
    })
}

fn uptime_secs() -> u64 {
    START_TIME.get().map_or(0, |t| t.elapsed().as_secs())
}

/// Record the current FFI operation.
pub fn record(op: &str) {
    if let Some(path) = get_thread_file() {
        let secs = uptime_secs();
        let entry = match get_caller() {
            Some(caller) => format!("t={}s caller={} {}", secs, caller, op),
            None => format!("t={}s {}", secs, op),
        };
        write_to_file(&path, &entry);
    }
}

/// Record with context detail.
pub fn record_fmt(op: &str, detail: &str) {
    if let Some(path) = get_thread_file() {
        let secs = uptime_secs();
        let entry = match get_caller() {
            Some(caller) => format!("t={}s caller={} {}: {}", secs, caller, op, detail),
            None => format!("t={}s {}: {}", secs, op, detail),
        };
        write_to_file(&path, &entry);
    }
}

fn write_to_file(path: &str, entry: &str) {
    if let Ok(mut f) = OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(true)
        .open(path)
    {
        let _ = writeln!(f, "{}", entry);
        let _ = f.flush();
    }
}

/// Install crash handler — no-op. Kept for API compatibility.
/// Go's runtime handles SIGBUS/SIGSEGV and produces goroutine dumps.
pub fn install_crash_handler() {
    // Intentionally empty — we don't intercept signals.
}
