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

fn id_of(o: &Output) -> String {
    let v: serde_json::Value = serde_json::from_str(out(o).lines().next().unwrap()).unwrap();
    v["id"].as_str().unwrap().to_string()
}

#[test]
fn exists_delete_and_ids_agree_with_count() {
    let (_d, p) = db();
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    assert_eq!(out(&run(&p, &["exists", &id], "")).trim(), "true");
    assert_eq!(out(&run(&p, &["ids"], "")).trim(), id);
    assert!(run(&p, &["delete", &id], "").status.success());
    assert_eq!(out(&run(&p, &["exists", &id], "")).trim(), "false");
    assert_eq!(count(&p), "0");
    let again = run(&p, &["delete", &id], "");
    assert!(!again.status.success() && err(&again).contains("no such attestation"));
}

#[test]
fn update_replaces_the_given_fields_and_ax_follows() {
    let (_d, p) = db();
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let patch = format!("{{\"id\":\"{id}\",\"subjects\":[\"ZED\"]}}");
    let o = run(&p, &["update", &patch], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&run(&p, &["ax", "ZED"], "")).contains(&id));
    assert!(!out(&run(&p, &["ax", "ALICE"], "")).contains(&id));
    assert_eq!(out(&run(&p, &["subjects"], "")).trim(), "ZED");
    // fields not named stay as they were
    assert!(out(&run(&p, &["get", &id], "")).contains("\"author\""));
}

#[test]
fn update_refuses_a_missing_id_an_unknown_id_and_an_unknown_field() {
    let (_d, p) = db();
    assert!(err(&run(&p, &["update", "{\"source\":\"x\"}"], "")).contains("needs an"));
    assert!(err(&run(&p, &["update", "{\"id\":\"AS-nope\"}"], "")).contains("no such attestation"));
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"bogus\":1}}")], "");
    assert!(err(&o).contains("not an attestation field"));
}

#[test]
fn clear_wants_to_be_told_to() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert!(!run(&p, &["clear"], "").status.success());
    assert_eq!(count(&p), "1");
    assert!(run(&p, &["clear", "--yes"], "").status.success());
    assert_eq!(count(&p), "0");
}

#[test]
fn the_listings_and_stats_read_what_was_written() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert_eq!(out(&run(&p, &["subjects"], "")).trim(), "ALICE");
    assert_eq!(out(&run(&p, &["predicates"], "")).trim(), "author");
    assert_eq!(out(&run(&p, &["contexts"], "")).trim(), "GITHUB");
    assert_eq!(out(&run(&p, &["actors"], "")).trim(), "BOB");
    let st: serde_json::Value = serde_json::from_str(out(&run(&p, &["stats"], "")).trim()).unwrap();
    assert_eq!(st["total_attestations"], 1);
    assert_eq!(st["unique_subjects"], 1);
}

#[test]
fn an_alias_makes_ax_find_the_other_name_and_rm_takes_it_back() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert!(out(&run(&p, &["ax", "ALIS"], "")).is_empty());
    assert!(run(&p, &["alias", "add", "ALICE", "ALIS"], "").status.success());
    assert!(out(&run(&p, &["ax", "ALIS"], "")).contains("\"ALICE\""));
    assert!(out(&run(&p, &["alias", "resolve", "ALIS"], "")).contains("ALICE"));
    assert_eq!(out(&run(&p, &["alias", "ls"], "")).lines().count(), 2);
    assert!(run(&p, &["alias", "rm", "ALICE", "ALIS"], "").status.success());
    assert!(out(&run(&p, &["ax", "ALIS"], "")).is_empty());
}
