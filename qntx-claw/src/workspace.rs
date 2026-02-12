//! OpenClaw workspace discovery, snapshot, and file watching.
//!
//! OpenClaw stores its state as plain Markdown files under `~/.openclaw/workspace/`:
//!
//! ```text
//! ~/.openclaw/workspace/
//! ├── AGENTS.md      # Operating instructions (the "system prompt")
//! ├── SOUL.md        # Behavioral traits, personality
//! ├── IDENTITY.md    # Core persona
//! ├── USER.md        # What the agent knows about the user
//! ├── TOOLS.md       # Tool conventions
//! ├── MEMORY.md      # Curated long-term memory
//! ├── HEARTBEAT.md   # Heartbeat checklist
//! ├── BOOT.md        # Startup checklist
//! ├── BOOTSTRAP.md   # First-run ritual
//! └── memory/
//!     ├── 2026-02-11.md
//!     └── 2026-02-12.md
//! ```

use notify::{Event, EventKind, RecommendedWatcher, RecursiveMode, Watcher as NotifyWatcher};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::SystemTime;
use tokio::sync::{mpsc, RwLock};
use tracing::{info, warn};

/// Bootstrap files tracked by the watcher.
pub const BOOTSTRAP_FILES: &[&str] = &[
    "AGENTS.md",
    "SOUL.md",
    "IDENTITY.md",
    "USER.md",
    "TOOLS.md",
    "MEMORY.md",
    "HEARTBEAT.md",
    "BOOT.md",
    "BOOTSTRAP.md",
];

/// A tracked workspace file.
#[derive(Debug, Clone, serde::Serialize)]
pub struct TrackedFile {
    /// Filename (e.g. "AGENTS.md")
    pub name: String,
    /// Absolute path
    pub path: PathBuf,
    /// File content (UTF-8)
    pub content: String,
    /// SHA-256 hex digest (first 16 chars)
    pub content_sha: String,
    /// Last modification time (unix seconds)
    pub mod_time: i64,
    /// Whether the file exists on disk
    pub exists: bool,
}

/// A daily memory log file.
#[derive(Debug, Clone, serde::Serialize)]
pub struct DailyMemory {
    /// Date string (YYYY-MM-DD)
    pub date: String,
    /// Absolute path
    pub path: PathBuf,
    /// File content
    pub content: String,
    /// SHA-256 hex digest (first 16 chars)
    pub content_sha: String,
    /// Last modification time (unix seconds)
    pub mod_time: i64,
}

/// Point-in-time view of the entire OpenClaw workspace.
#[derive(Debug, Clone, serde::Serialize)]
pub struct Snapshot {
    /// Absolute path to workspace root
    pub workspace_path: PathBuf,
    /// Bootstrap files, keyed by filename
    pub bootstrap_files: HashMap<String, TrackedFile>,
    /// Daily memory logs, sorted by date descending
    pub daily_memories: Vec<DailyMemory>,
    /// When the snapshot was taken (unix seconds)
    pub taken_at: i64,
}

/// Emitted when a workspace file changes.
#[derive(Debug, Clone)]
pub struct ChangeEvent {
    /// Relative path within workspace (e.g. "AGENTS.md" or "memory/2026-02-12.md")
    pub file: String,
    /// "bootstrap" or "memory"
    pub category: String,
    /// "created", "modified", or "deleted"
    pub operation: String,
}

/// Discovers the OpenClaw workspace directory.
///
/// Checks in order:
/// 1. Explicit path (if provided)
/// 2. `$OPENCLAW_WORKSPACE` environment variable
/// 3. `~/.openclaw/workspace`
pub fn discover(explicit_path: Option<&str>) -> Result<PathBuf, String> {
    let mut candidates: Vec<PathBuf> = Vec::new();

    if let Some(p) = explicit_path {
        candidates.push(PathBuf::from(p));
    }

    if let Ok(env_path) = std::env::var("OPENCLAW_WORKSPACE") {
        if !env_path.is_empty() {
            candidates.push(PathBuf::from(env_path));
        }
    }

    if let Some(home) = dirs_next_home() {
        candidates.push(home.join(".openclaw").join("workspace"));
    }

    for candidate in &candidates {
        if candidate.is_dir() {
            return Ok(candidate.clone());
        }
    }

    Err(format!(
        "OpenClaw workspace not found (checked: {})",
        candidates
            .iter()
            .map(|p| p.display().to_string())
            .collect::<Vec<_>>()
            .join(", ")
    ))
}

/// Takes a snapshot of the workspace at the given path.
pub fn take_snapshot(workspace_path: &Path) -> Result<Snapshot, String> {
    let mut bootstrap_files = HashMap::new();

    // Read bootstrap files
    for &name in BOOTSTRAP_FILES {
        let path = workspace_path.join(name);
        let tf = read_tracked_file(name, &path);
        bootstrap_files.insert(name.to_string(), tf);
    }

    // Read daily memory files
    let mut daily_memories = Vec::new();
    let memory_dir = workspace_path.join("memory");
    if memory_dir.is_dir() {
        if let Ok(entries) = std::fs::read_dir(&memory_dir) {
            for entry in entries.flatten() {
                let file_name = entry.file_name();
                let name = file_name.to_string_lossy();
                if !name.ends_with(".md") || entry.file_type().map_or(true, |ft| !ft.is_file()) {
                    continue;
                }

                let path = entry.path();
                let date = name.trim_end_matches(".md").to_string();

                match std::fs::read_to_string(&path) {
                    Ok(content) => {
                        let content_sha = sha256_short(&content);
                        let mod_time = file_mod_time(&path);
                        daily_memories.push(DailyMemory {
                            date,
                            path,
                            content,
                            content_sha,
                            mod_time,
                        });
                    }
                    Err(e) => {
                        warn!("Failed to read daily memory {}: {}", path.display(), e);
                    }
                }
            }
        }
    }

    // Sort by date descending (most recent first)
    daily_memories.sort_by(|a, b| b.date.cmp(&a.date));

    let taken_at = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);

    Ok(Snapshot {
        workspace_path: workspace_path.to_path_buf(),
        bootstrap_files,
        daily_memories,
        taken_at,
    })
}

/// Watches the workspace directory for changes and sends events to a channel.
///
/// Returns a channel receiver that yields `ChangeEvent`s and a handle to stop watching.
pub async fn watch(
    workspace_path: PathBuf,
    snapshot: Arc<RwLock<Snapshot>>,
) -> Result<(mpsc::UnboundedReceiver<ChangeEvent>, WatchHandle), String> {
    let (tx, rx) = mpsc::unbounded_channel();
    let (stop_tx, mut stop_rx) = mpsc::channel::<()>(1);

    let ws_path = workspace_path.clone();
    let tx_clone = tx.clone();

    // Create notify watcher with debounced events
    let (notify_tx, mut notify_rx) = mpsc::unbounded_channel();

    let mut watcher: RecommendedWatcher =
        notify::recommended_watcher(move |res: Result<Event, notify::Error>| {
            if let Ok(event) = res {
                let _ = notify_tx.send(event);
            }
        })
        .map_err(|e| format!("failed to create file watcher for {}: {}", workspace_path.display(), e))?;

    // Watch workspace root (non-recursive, bootstrap files live here)
    watcher
        .watch(&ws_path, RecursiveMode::NonRecursive)
        .map_err(|e| format!("failed to watch {}: {}", ws_path.display(), e))?;

    // Watch memory/ subdirectory if it exists
    let memory_dir = ws_path.join("memory");
    if memory_dir.is_dir() {
        watcher
            .watch(&memory_dir, RecursiveMode::NonRecursive)
            .map_err(|e| format!("failed to watch {}: {}", memory_dir.display(), e))?;
    }

    info!("Watching OpenClaw workspace at {}", ws_path.display());

    // Spawn the event loop
    tokio::spawn(async move {
        // Keep watcher alive
        let _watcher = watcher;

        // Track content hashes for dedup
        let mut hashes: HashMap<PathBuf, String> = {
            let snap = snapshot.read().await;
            let mut h = HashMap::new();
            for tf in snap.bootstrap_files.values() {
                if tf.exists {
                    h.insert(tf.path.clone(), tf.content_sha.clone());
                }
            }
            for dm in &snap.daily_memories {
                h.insert(dm.path.clone(), dm.content_sha.clone());
            }
            h
        };

        // Debounce: collect events over 300ms windows
        let mut pending: HashMap<PathBuf, EventKind> = HashMap::new();

        loop {
            tokio::select! {
                _ = stop_rx.recv() => {
                    info!("Workspace watcher stopping");
                    break;
                }
                Some(event) = notify_rx.recv() => {
                    for path in &event.paths {
                        if is_relevant_file(path, &ws_path) {
                            pending.insert(path.clone(), event.kind);
                        }
                    }

                    // Drain any remaining events within the debounce window
                    let deadline = tokio::time::Instant::now() + tokio::time::Duration::from_millis(300);
                    loop {
                        tokio::select! {
                            Some(event) = notify_rx.recv() => {
                                for path in &event.paths {
                                    if is_relevant_file(path, &ws_path) {
                                        pending.insert(path.clone(), event.kind);
                                    }
                                }
                            }
                            _ = tokio::time::sleep_until(deadline) => {
                                break;
                            }
                        }
                    }

                    // Process debounced events
                    let events: Vec<(PathBuf, EventKind)> = pending.drain().collect();
                    for (path, kind) in events {
                        if let Some(change) = process_event(
                            &path,
                            kind,
                            &ws_path,
                            &mut hashes,
                        ) {
                            info!(
                                "OpenClaw workspace change: {} {} ({})",
                                change.operation, change.file, change.category
                            );

                            // Refresh snapshot
                            match take_snapshot(&ws_path) {
                                Ok(new_snap) => {
                                    let mut snap = snapshot.write().await;
                                    *snap = new_snap;
                                }
                                Err(e) => {
                                    warn!("Failed to refresh snapshot after change: {}", e);
                                }
                            }

                            let _ = tx_clone.send(change);
                        }
                    }
                }
            }
        }
    });

    Ok((rx, WatchHandle { stop: stop_tx }))
}

/// Handle to stop the watcher.
pub struct WatchHandle {
    stop: mpsc::Sender<()>,
}

impl WatchHandle {
    pub async fn stop(self) {
        let _ = self.stop.send(()).await;
    }
}

fn process_event(
    path: &Path,
    kind: EventKind,
    workspace_path: &Path,
    hashes: &mut HashMap<PathBuf, String>,
) -> Option<ChangeEvent> {
    let rel_path = path
        .strip_prefix(workspace_path)
        .ok()?
        .to_string_lossy()
        .to_string();

    let category = if rel_path.starts_with("memory/") || rel_path.starts_with("memory\\") {
        "memory"
    } else {
        "bootstrap"
    };

    let operation = match kind {
        EventKind::Remove(_) => {
            hashes.remove(path);
            "deleted"
        }
        EventKind::Create(_) => {
            if let Ok(content) = std::fs::read_to_string(path) {
                let new_hash = sha256_short(&content);
                hashes.insert(path.to_path_buf(), new_hash);
            }
            "created"
        }
        EventKind::Modify(_) => {
            if let Ok(content) = std::fs::read_to_string(path) {
                let new_hash = sha256_short(&content);
                let old_hash = hashes.get(path).cloned().unwrap_or_default();
                if old_hash == new_hash {
                    return None; // Content unchanged
                }
                hashes.insert(path.to_path_buf(), new_hash);
            }
            "modified"
        }
        _ => return None,
    };

    Some(ChangeEvent {
        file: rel_path,
        category: category.to_string(),
        operation: operation.to_string(),
    })
}

fn is_relevant_file(path: &Path, workspace_path: &Path) -> bool {
    let name = match path.file_name() {
        Some(n) => n.to_string_lossy(),
        None => return false,
    };

    // Must be a .md file
    if !name.ends_with(".md") {
        return false;
    }

    // Bootstrap file at workspace root
    if let Ok(rel) = path.strip_prefix(workspace_path) {
        let components: Vec<_> = rel.components().collect();
        match components.len() {
            1 => return BOOTSTRAP_FILES.contains(&name.as_ref()),
            2 => {
                // memory/YYYY-MM-DD.md
                if let Some(parent) = rel.parent() {
                    return parent == Path::new("memory");
                }
            }
            _ => {}
        }
    }

    false
}

fn read_tracked_file(name: &str, path: &Path) -> TrackedFile {
    match std::fs::read_to_string(path) {
        Ok(content) => {
            let content_sha = sha256_short(&content);
            let mod_time = file_mod_time(path);
            TrackedFile {
                name: name.to_string(),
                path: path.to_path_buf(),
                content,
                content_sha,
                exists: true,
                mod_time,
            }
        }
        Err(_) => TrackedFile {
            name: name.to_string(),
            path: path.to_path_buf(),
            content: String::new(),
            content_sha: String::new(),
            exists: false,
            mod_time: 0,
        },
    }
}

fn sha256_short(data: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data.as_bytes());
    let result = hasher.finalize();
    hex::encode(&result[..8]) // 16-char hex prefix
}

fn file_mod_time(path: &Path) -> i64 {
    std::fs::metadata(path)
        .ok()
        .and_then(|m| m.modified().ok())
        .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Cross-platform home directory lookup without pulling in a heavy dep.
fn dirs_next_home() -> Option<PathBuf> {
    #[cfg(unix)]
    {
        std::env::var_os("HOME").map(PathBuf::from)
    }
    #[cfg(windows)]
    {
        std::env::var_os("USERPROFILE").map(PathBuf::from)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_sha256_short() {
        let hash = sha256_short("hello world");
        assert_eq!(hash.len(), 16);
    }

    #[test]
    fn test_snapshot_empty_dir() {
        let dir = tempfile::tempdir().unwrap();
        let snap = take_snapshot(dir.path()).unwrap();
        assert_eq!(snap.bootstrap_files.len(), BOOTSTRAP_FILES.len());
        assert!(snap.daily_memories.is_empty());

        // All files should be marked as not existing
        for tf in snap.bootstrap_files.values() {
            assert!(!tf.exists);
        }
    }

    #[test]
    fn test_snapshot_with_files() {
        let dir = tempfile::tempdir().unwrap();

        // Create an AGENTS.md
        fs::write(dir.path().join("AGENTS.md"), "You are a helpful assistant.").unwrap();

        // Create memory directory with a daily file
        fs::create_dir_all(dir.path().join("memory")).unwrap();
        fs::write(
            dir.path().join("memory/2026-02-12.md"),
            "# Session log\nDid some things.",
        )
        .unwrap();

        let snap = take_snapshot(dir.path()).unwrap();

        let agents = snap.bootstrap_files.get("AGENTS.md").unwrap();
        assert!(agents.exists);
        assert_eq!(agents.content, "You are a helpful assistant.");
        assert!(!agents.content_sha.is_empty());

        assert_eq!(snap.daily_memories.len(), 1);
        assert_eq!(snap.daily_memories[0].date, "2026-02-12");
    }

    #[test]
    fn test_snapshot_daily_memories_sorted_descending() {
        let dir = tempfile::tempdir().unwrap();
        fs::create_dir_all(dir.path().join("memory")).unwrap();
        fs::write(dir.path().join("memory/2026-02-10.md"), "day1").unwrap();
        fs::write(dir.path().join("memory/2026-02-12.md"), "day3").unwrap();
        fs::write(dir.path().join("memory/2026-02-11.md"), "day2").unwrap();

        let snap = take_snapshot(dir.path()).unwrap();
        assert_eq!(snap.daily_memories.len(), 3);
        assert_eq!(snap.daily_memories[0].date, "2026-02-12");
        assert_eq!(snap.daily_memories[1].date, "2026-02-11");
        assert_eq!(snap.daily_memories[2].date, "2026-02-10");
    }

    #[test]
    fn test_is_relevant_file() {
        let ws = PathBuf::from("/home/user/.openclaw/workspace");

        assert!(is_relevant_file(
            &ws.join("AGENTS.md"),
            &ws,
        ));
        assert!(is_relevant_file(
            &ws.join("SOUL.md"),
            &ws,
        ));
        assert!(is_relevant_file(
            &ws.join("memory/2026-02-12.md"),
            &ws,
        ));
        assert!(!is_relevant_file(
            &ws.join("random.md"),
            &ws,
        ));
        assert!(!is_relevant_file(
            &ws.join("AGENTS.txt"),
            &ws,
        ));
        assert!(!is_relevant_file(
            &ws.join("subdir/deep/file.md"),
            &ws,
        ));
    }
}
