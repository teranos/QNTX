//! laye-peer --key <path> --listen <multiaddr>... --topic <topic>...
//!
//! Starts a laye peer others connect to. Says who it is and every address it
//! listens on, then every laye event, one JSON line each on stdout. A failure
//! is one sacred-error JSON line on stderr.

use std::path::PathBuf;
use std::time::Duration;

use laye_net::{NetEvent, Topic};

struct Args {
    key: PathBuf,
    listen: Vec<String>,
    topics: Vec<Topic>,
}

fn args() -> Result<Args, Box<sacred_error::Error>> {
    let mut key = None;
    let mut listen = Vec::new();
    let mut topics = Vec::new();
    let mut it = std::env::args().skip(1);
    while let Some(flag) = it.next() {
        let value = it.next().ok_or_else(|| {
            laye_peer::error(
                "args",
                "a flag was given no value",
                format!("{flag} names nothing"),
            )
        })?;
        match flag.as_str() {
            "--key" => key = Some(PathBuf::from(value)),
            "--listen" => listen.push(value),
            "--topic" => topics.push(Topic(value)),
            _ => {
                return Err(laye_peer::error(
                    "args",
                    "laye-peer does not know this flag",
                    format!("{flag}; it takes --key, --listen and --topic"),
                ));
            }
        }
    }
    let key = key.ok_or_else(|| {
        laye_peer::error(
            "args",
            "laye-peer was not told where its key is",
            "--key <path> names it; a key is made there when none is",
        )
    })?;
    if listen.is_empty() {
        return Err(laye_peer::error(
            "args",
            "laye-peer was not told where to listen",
            "--listen <multiaddr>, e.g. /ip4/0.0.0.0/tcp/4001/ws",
        ));
    }
    Ok(Args {
        key,
        listen,
        topics,
    })
}

fn fail(error: &sacred_error::Error) -> ! {
    match serde_json::to_string(error) {
        Ok(line) => eprintln!("{line}"),
        Err(e) => eprintln!(
            "{} ({}): {}; and it did not serialize: {e}",
            error.title, error.id, error.why
        ),
    }
    std::process::exit(1)
}

#[tokio::main]
async fn main() {
    let args = args().unwrap_or_else(|e| fail(&e));
    let keypair = laye_peer::key_at(&args.key).unwrap_or_else(|e| fail(&e));
    let did = laye_peer::did_of(&keypair).unwrap_or_default();
    let (net, drive) =
        laye_peer::listening(keypair, args.listen, args.topics).unwrap_or_else(|e| {
            fail(&laye_peer::error(
                "start",
                "the peer did not start",
                e.to_string(),
            ))
        });
    tokio::spawn(drive);

    println!(
        "{}",
        serde_json::json!({ "peer": net.identity().0, "did": did })
    );
    loop {
        for event in net.poll_events() {
            match serde_json::to_string(&event) {
                Ok(line) => println!("{line}"),
                Err(e) => fail(&laye_peer::error(
                    "event",
                    "a laye event did not serialize",
                    e.to_string(),
                )),
            }
            match &event {
                NetEvent::Listening { addr } => println!(
                    "{}",
                    serde_json::json!({ "dial": format!("{addr}/p2p/{}", net.identity().0) })
                ),
                // A peer nobody can connect to is no peer: it stops, and says why.
                NetEvent::Unreachable { reason } => fail(&laye_peer::error(
                    "listen",
                    "the peer listens on nothing, so nobody can connect to it",
                    reason.clone(),
                )),
                _ => {}
            }
        }
        tokio::time::sleep(Duration::from_millis(200)).await;
    }
}
