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
    // named actors under a real signature stand, untouched
    let (_d2, q) = db();
    let line = out(&run(&q, &["put", "ALICE is author of GITHUB by BOB"], "")).lines().next().unwrap().to_string();
    let o = run(&p, &["put", "-"], &line);
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(count(&p), "1");
    assert!(out(&run(&p, &["actors"], "")).trim() == "BOB");
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

fn verdicts(p: &std::path::Path) -> Vec<serde_json::Value> {
    out(&run(p, &["verify"], "")).lines().map(|l| serde_json::from_str(l).unwrap()).collect()
}

#[test]
fn put_signs_with_a_key_minted_beside_the_db_and_the_same_key_signs_again() {
    let (_d, p) = db();
    let key_file = std::path::PathBuf::from(format!("{}.key", p.display()));
    assert!(!key_file.exists());
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert!(key_file.exists());
    let did1: serde_json::Value = serde_json::from_str(out(&run(&p, &["key"], "")).trim()).unwrap();
    run(&p, &["put", "CAROL is author of GITHUB by BOB"], "");
    let v = verdicts(&p);
    assert_eq!(v.len(), 2);
    for x in &v {
        assert_eq!(x["signature"], "valid");
        assert_eq!(x["signer"], did1["did"]);
    }
    assert!(did1["did"].as_str().unwrap().starts_with("did:key:z"));
    {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(std::fs::metadata(&key_file).unwrap().permissions().mode() & 0o777, 0o600);
    }
}

#[test]
fn unsigned_writes_no_signature_and_mints_no_key() {
    let (_d, p) = db();
    run(&p, &["--unsigned", "put", "ALICE is author of GITHUB by BOB"], "");
    assert!(!std::path::PathBuf::from(format!("{}.key", p.display())).exists());
    assert_eq!(verdicts(&p)[0]["signature"], "unsigned");
}

#[test]
fn a_signature_given_that_does_not_hold_is_refused_and_one_that_holds_is_kept() {
    let (d, p) = db();
    let other = d.path().join("other.key");
    let o = run(&p, &["--key", other.to_str().unwrap(), "put", "ALICE is author of GITHUB by BOB"], "");
    assert!(o.status.success(), "{}", err(&o));
    let line = out(&o).lines().next().unwrap().to_string();
    let (_d2, p2) = db();
    // carried to another db, the signature holds and the signer stays the other key's
    assert!(run(&p2, &["put", "-"], &line).status.success());
    assert_eq!(verdicts(&p2)[0]["signature"], "valid");
    // one field changed under the signature
    let tampered = line.replace("ALICE", "MALLORY");
    let (_d3, p3) = db();
    let o = run(&p3, &["put", "-"], &tampered);
    assert!(!o.status.success() && err(&o).contains("does not hold"), "{}", err(&o));
    assert_eq!(count(&p3), "0");
}

#[test]
fn update_re_signs_what_its_own_key_signed_and_refuses_another_signers() {
    let (d, p) = db();
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"subjects\":[\"ZED\"]}}")], "");
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(verdicts(&p)[0]["signature"], "valid");

    let other = d.path().join("other.key");
    let id2 = id_of(&run(&p, &["--key", other.to_str().unwrap(), "put", "CAROL is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id2}\",\"subjects\":[\"MALLORY\"]}}")], "");
    assert!(!o.status.success() && err(&o).contains("cannot make"), "{}", err(&o));
    assert!(verdicts(&p).iter().all(|v| v["signature"] == "valid"));
}

#[test]
fn a_file_that_is_not_a_key_is_never_replaced() {
    let (d, p) = db();
    let f = d.path().join("not.key");
    std::fs::write(&f, b"hello").unwrap();
    let o = run(&p, &["--key", f.to_str().unwrap(), "put", "ALICE is author of GITHUB by BOB"], "");
    assert!(!o.status.success() && err(&o).contains("not a key"), "{}", err(&o));
    assert_eq!(std::fs::read(&f).unwrap(), b"hello");
}

#[test]
fn an_update_cannot_leave_a_signed_row_without_actors_or_name_a_signature() {
    let (_d, p) = db();
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"actors\":[]}}")], "");
    assert!(!o.status.success() && err(&o).contains("must name its actors"), "{}", err(&o));
    assert_eq!(out(&run(&p, &["actors"], "")).trim(), "BOB");
    assert_eq!(verdicts(&p)[0]["signature"], "valid");

    // a signature is made, not edited: not on a signed row, not on an unsigned one
    for patch in [
        format!("{{\"id\":\"{id}\",\"signer_did\":\"did:key:zFAKE\"}}"),
        format!("{{\"id\":\"{id}\",\"signature\":[1,2,3]}}"),
    ] {
        let o = run(&p, &["update", &patch], "");
        assert!(!o.status.success() && err(&o).contains("cannot name a signature"), "{}", err(&o));
    }
    let un = id_of(&run(&p, &["--unsigned", "put", "CAROL is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{un}\",\"signature\":[1,2,3],\"signer_did\":\"did:key:zFAKE\"}}")], "");
    assert!(!o.status.success());
    assert!(verdicts(&p).iter().all(|v| v["signature"] != "invalid"));
}

#[test]
fn a_signer_without_a_signature_is_refused_signed_or_not() {
    let (_d, p) = db();
    let line = "{\"subjects\":[\"X\"],\"predicates\":[\"p\"],\"actors\":[\"a\"],\"signer_did\":\"did:key:zFAKE\"}\n";
    for flags in [vec!["--unsigned", "put", "-"], vec!["put", "-"]] {
        let o = run(&p, &flags, line);
        assert!(!o.status.success() && err(&o).contains("signer without a signature"), "{}", err(&o));
    }
    assert_eq!(count(&p), "0");
}

#[test]
fn a_signature_already_made_needs_no_key_and_a_refused_input_mints_none() {
    let (d, p) = db();
    let line = out(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], "")).lines().next().unwrap().to_string();
    let (_d2, q) = db();
    let no_key = d.path().join("no-such-dir").join("k.key");
    let o = run(&q, &["--key", no_key.to_str().unwrap(), "put", "-"], &line);
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(verdicts(&q)[0]["signature"], "valid");

    let (_d3, r) = db();
    let key = std::path::PathBuf::from(format!("{}.key", r.display()));
    let o = run(&r, &["put", "-"], "{\"nonsense\":1}\n");
    assert!(!o.status.success());
    assert!(!key.exists(), "a refused input minted a key");
}

#[test]
fn processes_minting_at_once_end_with_one_key_and_leave_nothing_behind() {
    let d = tempfile::tempdir().unwrap();
    let key = d.path().join("shared.key");
    let handles: Vec<_> = (0..8)
        .map(|i| {
            let db = d.path().join(format!("{i}.db"));
            let key = key.clone();
            std::thread::spawn(move || {
                let o = run(&db, &["--key", key.to_str().unwrap(), "put", "ALICE is author of GITHUB by BOB"], "");
                assert!(o.status.success(), "{}", err(&o));
                let k = run(&db, &["--key", key.to_str().unwrap(), "key"], "");
                serde_json::from_str::<serde_json::Value>(out(&k).trim()).unwrap()["did"].as_str().unwrap().to_string()
            })
        })
        .collect();
    let dids: Vec<String> = handles.into_iter().map(|h| h.join().unwrap()).collect();
    assert!(dids.iter().all(|x| x == &dids[0]), "{dids:?}");
    let left: Vec<_> = std::fs::read_dir(d.path()).unwrap().map(|e| e.unwrap().file_name().to_string_lossy().into_owned()).filter(|n| n.ends_with(".tmp")).collect();
    assert!(left.is_empty(), "{left:?}");
}

/// A row signed by ats-dev's key on the signing build, carried here as text: this
/// build has no key, and a signature stays valid wherever the row is put.
const SIGNED: &str = r##"{"id":"AS-75847316-caac-45d6-b819-06eecbf8be07","subjects":["ALICE"],"predicates":["author"],"contexts":["GITHUB"],"actors":["BOB"],"timestamp":1794266294644,"source":"ats-dev","attributes":{"n":3,"note":"<a & \"b\">"},"created_at":1794266294644,"signature":[154,101,30,4,174,33,231,130,163,19,62,221,216,85,196,207,112,183,156,27,24,248,47,250,137,19,188,22,86,184,215,151,35,6,77,164,172,202,97,80,81,54,127,39,244,108,1,4,35,240,67,118,113,106,175,97,208,66,119,80,181,32,18,8],"signer_did":"did:key:z6Mkofs97sbDZBoGXR6im4Po2zJmwGdjJe2LQpAudzVx2ekp"}"##;

fn signed_line() -> String {
    SIGNED.to_string()
}

#[test]
fn a_signed_row_is_not_edited_under_its_signature_and_a_patch_names_none() {
    let (_d, p) = db();
    let line = signed_line();
    assert!(run(&p, &["put", "-"], &line).status.success());
    let id = serde_json::from_str::<serde_json::Value>(&line).unwrap()["id"].as_str().unwrap().to_string();
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"subjects\":[\"ZED\"]}}")], "");
    assert!(!o.status.success() && err(&o).contains("ats-dev cannot make"), "{}", err(&o));
    assert!(out(&run(&p, &["get", &id], "")).contains("\"ALICE\""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"signature\":[1]}}")], "");
    assert!(!o.status.success() && err(&o).contains("cannot name a signature"), "{}", err(&o));
}

#[test]
fn an_update_that_empties_the_actors_authors_them_and_reports_what_is_on_disk() {
    let (_d, p) = db();
    let id = id_of(&run(&p, &["--unsigned", "put", "CAROL is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"actors\":[]}}")], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&run(&p, &["ax", "CAROL"], "")).contains(&id), "the row vanished from ax");
    assert_eq!(out(&run(&p, &["actors"], "")).trim(), "ats-dev");

    let said = out(&run(&p, &["update", &format!("{{\"id\":\"{id}\",\"created_at\":1}}")], ""));
    let said: serde_json::Value = serde_json::from_str(said.lines().next().unwrap()).unwrap();
    let got: serde_json::Value = serde_json::from_str(out(&run(&p, &["get", &id], "")).trim()).unwrap();
    assert_eq!(said["created_at"], got["created_at"]);
    assert_ne!(got["created_at"], 1);
}

#[test]
fn refusing_an_edit_to_a_foreign_signed_row_mints_no_key() {
    let (_d, p) = db();
    assert!(run(&p, &["put", "-"], SIGNED).status.success());
    let key = std::path::PathBuf::from(format!("{}.key", p.display()));
    assert!(!key.exists(), "importing a signed row minted a key");
    let id = serde_json::from_str::<serde_json::Value>(SIGNED).unwrap()["id"].as_str().unwrap().to_string();
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"subjects\":[\"ZED\"]}}")], "");
    assert!(!o.status.success() && err(&o).contains("not made yet"), "{}", err(&o));
    assert!(!key.exists(), "a refused edit minted a key");
}

#[test]
fn a_leftover_temporary_file_does_not_stop_a_key_being_made_and_is_not_removed() {
    let d = tempfile::tempdir().unwrap();
    let key = d.path().join("k.key");
    let stale: Vec<_> = ["k.key.1.tmp", format!("k.key.{}.tmp", std::process::id()).as_str()]
        .iter()
        .map(|n| {
            let f = d.path().join(n);
            std::fs::write(&f, b"left by something else").unwrap();
            f
        })
        .collect();
    let db = d.path().join("a.db");
    let o = run(&db, &["--key", key.to_str().unwrap(), "put", "ALICE is author of GITHUB by BOB"], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(key.exists());
    for f in stale {
        assert_eq!(std::fs::read(&f).unwrap(), b"left by something else", "{}", f.display());
    }
}
