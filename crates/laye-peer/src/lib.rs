#![cfg_attr(
    test,
    allow(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::panic,
        clippy::indexing_slicing,
        clippy::string_slice
    )
)]
//! A laye peer that others connect to, run without a browser.

// "relaye is dead. Make laye-net able to accept connections, and prove it with two native peers passing one message through a third."

use std::path::Path;

use laye_net::{Keypair, Net, NetConfig, NetDrive, NetError, Topic};
use sacred_error::{Context, Error, Severity};

/// What a laye peer says it speaks when another identifies it.
pub const IDENTIFY: &str = "/laye/1.0.0";

/// A peer that listens at `listen_addrs` and carries `topics` between the
/// peers that connect to it.
pub fn listening(
    keypair: Keypair,
    listen_addrs: Vec<String>,
    topics: Vec<Topic>,
) -> Result<(Net, NetDrive), NetError> {
    laye_net::new(NetConfig {
        bootstrap_addrs: Vec::new(),
        listen_addrs,
        keypair,
        topics,
        identify_protocol: IDENTIFY.into(),
    })
}

/// The did:key a keypair is, when it is an ed25519 one.
pub fn did_of(keypair: &Keypair) -> Option<String> {
    let ed25519 = keypair.public().try_into_ed25519().ok()?;
    Some(laye_me::didkey::encode(&ed25519.to_bytes()))
}

/// The key this peer is, kept at `path`. Others dial a peer by its id, so the
/// key is made once and read every start after.
pub fn key_at(path: &Path) -> Result<Keypair, Box<Error>> {
    match std::fs::read(path) {
        Ok(bytes) => decode(path, &bytes),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            let keypair = laye_me::fresh();
            let bytes = laye_me::to_bytes(&keypair).map_err(|e| {
                error(
                    "key-unwritable",
                    "the peer's new key was not encoded",
                    e.to_string(),
                )
            })?;
            match keep_once(path, &bytes) {
                Ok(()) => Ok(keypair),
                // Another start kept its key first: that key is this peer. It
                // is read once, and what does not read is said to: a path
                // that is there to link to and not there to read (a dangling
                // symlink) is never retried. This start returns that key, so
                // it makes the key's entry durable too.
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
                    sync_dir_of(path).map_err(|e| {
                        error(
                            "key-unwritable",
                            "the directory holding the peer's key was not synced",
                            format!("{}: {e}", path.display()),
                        )
                    })?;
                    let bytes = std::fs::read(path).map_err(|e| unreadable(path, &e))?;
                    decode(path, &bytes)
                }
                Err(e) => Err(error(
                    "key-unwritable",
                    "the peer's new key was not kept",
                    format!("{}: {e}", path.display()),
                )),
            }
        }
        Err(e) => Err(unreadable(path, &e)),
    }
}

fn decode(path: &Path, bytes: &[u8]) -> Result<Keypair, Box<Error>> {
    laye_me::load(bytes).map_err(|e| {
        error(
            "key-unreadable",
            "the peer's key does not read",
            format!("{}: {e}", path.display()),
        )
    })
}

fn unreadable(path: &Path, e: &std::io::Error) -> Box<Error> {
    error(
        "key-unreadable",
        "the peer's key could not be read",
        format!("{}: {e}", path.display()),
    )
}

/// A file's sync makes its bytes durable, not its name: the directory that
/// holds the name is synced too, or after a crash the key can be gone and the
/// next start is a different peer.
fn sync_dir_of(path: &Path) -> std::io::Result<()> {
    let dir = match path.parent() {
        Some(p) if !p.as_os_str().is_empty() => p,
        _ => Path::new("."),
    };
    std::fs::File::open(dir)?.sync_all()
}

/// The key is written whole and synced beside `path`, then linked in. A link
/// never replaces a file, so `path` holds either nothing or a whole key, and
/// a second start finds `AlreadyExists` rather than overwriting the first.
fn keep_once(path: &Path, bytes: &[u8]) -> std::io::Result<()> {
    use std::sync::atomic::{AtomicU64, Ordering};
    static NEXT: AtomicU64 = AtomicU64::new(0);
    let mut beside = path.as_os_str().to_owned();
    beside.push(format!(
        ".{}-{}.new",
        std::process::id(),
        NEXT.fetch_add(1, Ordering::Relaxed)
    ));
    let beside = std::path::PathBuf::from(beside);

    let written = write_owner_only(&beside, bytes).and_then(|()| std::fs::hard_link(&beside, path));
    let removed = std::fs::remove_file(&beside);
    written?;
    removed?;
    sync_dir_of(path)
}

#[cfg(unix)]
fn write_owner_only(path: &Path, bytes: &[u8]) -> std::io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let mut f = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)?;
    f.write_all(bytes)?;
    f.sync_all()
}

/// Off unix a new file takes its directory's access list, which can let other
/// local users read the key. No key is made where only its owner reading it
/// cannot be set.
#[cfg(not(unix))]
fn write_owner_only(path: &Path, _bytes: &[u8]) -> std::io::Result<()> {
    Err(std::io::Error::new(
        std::io::ErrorKind::Unsupported,
        format!(
            "{}: laye-peer makes a key only where it can make it readable by its owner alone (unix)",
            path.display()
        ),
    ))
}

/// A failure at laye-peer's boundary, in sacred-error's shape.
pub fn error(region: &str, title: &str, why: impl Into<String>) -> Box<Error> {
    use std::time::{SystemTime, UNIX_EPOCH};
    let ms = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis())
        .unwrap_or(0);
    Box::new(Error {
        id: format!("err-laye-peer-{ms}"),
        severity: Severity::Error,
        context: Context {
            surface: "laye-peer".into(),
            region: Some(region.into()),
            anchor: None,
        },
        title: title.into(),
        why: why.into(),
        trace: Vec::new(),
        raw: None,
        at: ms.to_string(),
        source: Some("rust".into()),
        ffi_call: None,
        location: None,
        js_stack: None,
        raw_stderr: None,
        requires_reload: false,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_key_kept_is_the_key_read_back() {
        let dir = std::env::temp_dir().join(format!("laye-peer-key-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("key");
        if let Err(e) = std::fs::remove_file(&path) {
            assert_eq!(e.kind(), std::io::ErrorKind::NotFound, "{e}");
        }

        let made = key_at(&path).unwrap();
        let read = key_at(&path).unwrap();
        assert_eq!(made.public(), read.public());

        std::fs::remove_dir_all(&dir).unwrap();
    }

    /// Two starts at once on an empty path end as one peer: whoever kept a
    /// key first, the other reads it.
    #[test]
    fn two_starts_at_once_are_one_peer() {
        for round in 0..20 {
            let dir =
                std::env::temp_dir().join(format!("laye-peer-race-{}-{round}", std::process::id()));
            std::fs::create_dir_all(&dir).unwrap();
            let path = dir.join("key");

            let a = {
                let path = path.clone();
                std::thread::spawn(move || key_at(&path))
            };
            let b = {
                let path = path.clone();
                std::thread::spawn(move || key_at(&path))
            };
            let a = a.join().unwrap().unwrap();
            let b = b.join().unwrap().unwrap();
            assert_eq!(a.public(), b.public(), "round {round}");
            assert_eq!(key_at(&path).unwrap().public(), a.public());

            // Nothing written beside the key is left behind.
            let left: Vec<_> = std::fs::read_dir(&dir).unwrap().collect();
            assert_eq!(left.len(), 1, "round {round}");

            std::fs::remove_dir_all(&dir).unwrap();
        }
    }

    /// A key path that is a dangling symlink reads as missing and links as
    /// taken. It is said to once, not retried until the stack runs out.
    #[cfg(unix)]
    #[test]
    fn a_dangling_key_path_is_said_to_once() {
        let dir = std::env::temp_dir().join(format!("laye-peer-dangling-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("key");
        std::os::unix::fs::symlink(dir.join("nothing-here"), &path).unwrap();

        let e = key_at(&path).err().unwrap();
        assert_eq!(e.context.region.as_deref(), Some("key-unreadable"));

        // Nothing written beside the key is left behind.
        let left: Vec<_> = std::fs::read_dir(&dir).unwrap().collect();
        assert_eq!(left.len(), 1);

        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn a_key_that_does_not_read_is_said_to() {
        let dir = std::env::temp_dir().join(format!("laye-peer-bad-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("key");
        std::fs::write(&path, [0xff; 8]).unwrap();

        let e = key_at(&path).err().unwrap();
        assert_eq!(e.context.region.as_deref(), Some("key-unreadable"));

        std::fs::remove_dir_all(&dir).unwrap();
    }
}
