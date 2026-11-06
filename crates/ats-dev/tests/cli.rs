//! ats-dev through its binary: what it says on stdout is what is on disk.
#![allow(clippy::unwrap_used, clippy::expect_used, clippy::indexing_slicing)]

use std::io::Write;
use std::process::{Command, Output, Stdio};

fn run(db: &std::path::Path, args: &[&str], stdin: &str) -> Output {
    let mut c = Command::new(env!("CARGO_BIN_EXE_ats-dev"))
        .arg("--db")
        .arg(db)
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    c.stdin.take().unwrap().write_all(stdin.as_bytes()).unwrap();
    c.wait_with_output().unwrap()
}
fn out(o: &Output) -> String { String::from_utf8_lossy(&o.stdout).into_owned() }
fn err(o: &Output) -> String { String::from_utf8_lossy(&o.stderr).into_owned() }
fn db() -> (tempfile::TempDir, std::path::PathBuf) {
    let d = tempfile::tempdir().unwrap();
    let p = d.path().join("t.db");
    (d, p)
}
fn count(p: &std::path::Path) -> String { out(&run(p, &["count"], "")).trim().to_string() }

#[test]
fn a_sentence_is_written_and_ax_returns_it() {
    let (_d, p) = db();
    assert!(run(&p, &["put", "ALICE is author of GITHUB by BOB"], "").status.success());
    let o = run(&p, &["ax", "ALICE"], "");
    assert!(out(&o).contains("\"author\""), "{}", out(&o));
}

#[test]
fn an_attestation_no_actor_was_named_for_is_authored_by_ats_dev_and_ax_returns_it() {
    let (_d, p) = db();
    let o = run(&p, &["put", "ALICE is author of GITHUB"], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&o).contains("\"actors\":[\"ats-dev\"]"), "{}", out(&o));
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"CAROL\"],\"predicates\":[\"reviewer\"]}\n");
    assert!(o.status.success(), "{}", err(&o));
    for who in ["ALICE", "CAROL"] {
        let o = run(&p, &["ax", who], "");
        assert!(out(&o).contains("\"ats-dev\""), "ax {who} returned nothing: {}", err(&o));
    }
}

#[test]
fn an_actor_that_was_named_stands() {
    let (_d, p) = db();
    let o = run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert!(out(&o).contains("\"actors\":[\"BOB\"]"), "{}", out(&o));
}

#[test]
fn a_date_no_store_holds_is_refused_not_kept_as_today() {
    let (_d, p) = db();
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"timestamp\":9223372036854775807}\n");
    assert!(!o.status.success());
    assert!(err(&o).contains("outside the dates"), "{}", err(&o));
    assert_eq!(out(&o), "");
    assert_eq!(count(&p), "0");
}

#[test]
fn an_id_is_said_only_for_a_row_that_is_on_disk() {
    let (_d, p) = db();
    let good = "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"actors\":[\"x\"]}\n";
    let o = run(&p, &["put", "-"], &format!("{good}not json\n"));
    assert!(!o.status.success());
    assert_eq!(out(&o), "", "an id was said for a batch that was rolled back");
    assert!(err(&o).contains("line 2"), "{}", err(&o));
    assert_eq!(count(&p), "0");
    // and the same good line alone is written and said
    let o = run(&p, &["put", "-"], good);
    assert!(o.status.success());
    assert_eq!(out(&o).lines().count(), 1);
    assert_eq!(count(&p), "1");
}

#[test]
fn a_field_that_is_not_an_attestation_field_is_refused() {
    let (_d, p) = db();
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"actors\":[\"x\"],\"typo_field\":1}\n");
    assert!(!o.status.success());
    assert!(err(&o).contains("typo_field"), "{}", err(&o));
    assert_eq!(count(&p), "0");
}

#[test]
fn a_sentence_with_a_time_clause_is_refused() {
    let (_d, p) = db();
    let o = run(&p, &["put", "ALICE is author of GITHUB since last month by BOB"], "");
    assert!(!o.status.success());
    assert!(err(&o).contains("time clause"), "{}", err(&o));
    assert_eq!(count(&p), "0");
}

#[test]
fn a_negative_timestamp_with_a_sub_second_is_kept() {
    let (_d, p) = db();
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"OLD\"],\"predicates\":[\"p\"],\"actors\":[\"x\"],\"timestamp\":-1500,\"created_at\":-1500}\n");
    assert!(o.status.success(), "{}", err(&o));
    let o = run(&p, &["ax", "OLD"], "");
    assert!(out(&o).contains("\"timestamp\":-1500"), "{}", out(&o));
}

#[test]
fn a_signed_attestation_without_actors_is_refused_not_authored_over_its_signature() {
    let (_d, p) = db();
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"signature\":[1,2,3]}\n");
    assert!(!o.status.success());
    assert!(err(&o).contains("signed attestation must name its actors"), "{}", err(&o));
    assert_eq!(out(&o), "");
    assert_eq!(count(&p), "0");
    // named actors under a signature stand, untouched
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"actors\":[\"bob\"],\"signature\":[1,2,3]}\n");
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(count(&p), "1");
}
