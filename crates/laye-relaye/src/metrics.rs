pub trait Metrics: Send + Sync {
    fn gauge(&self, name: &str, value: f64);
    fn counter(&self, name: &str, delta: u64);
}

pub struct StdoutSink;

impl Metrics for StdoutSink {
    fn gauge(&self, name: &str, value: f64) {
        tracing::info!(metric = name, value, kind = "gauge");
    }
    fn counter(&self, name: &str, delta: u64) {
        tracing::info!(metric = name, delta, kind = "counter");
    }
}

/// Process RSS in bytes from /proc/self/statm on Linux. Returns 0
/// on platforms without /proc.
pub fn read_proc_memory_rss() -> u64 {
    let Ok(statm) = std::fs::read_to_string("/proc/self/statm") else {
        return 0;
    };
    let mut parts = statm.split_whitespace();
    let _vms = parts.next();
    let rss_pages: u64 = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0);
    rss_pages * 4096
}
