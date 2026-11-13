//! ats-dev through its binary: what it says on stdout is what is on disk.
#![allow(clippy::unwrap_used, clippy::expect_used, clippy::indexing_slicing)]

use std::io::Write;
use std::process::{Command, Output, Stdio};

/// The seed every test's own key is, kept where `~/.qntx/key` is for the test:
/// fixed, so a signature a test makes is the same every run.
const SEED_HEX: &str = "1111111111111111111111111111111111111111111111111111111111111111";
/// Another key's seed, for a row signed by someone else.
const OTHER_SEED_HEX: &str = "2222222222222222222222222222222222222222222222222222222222222222";

/// A key kept at `path` as `ground code login` keeps one: the seed in hex, 0600.
fn write_key(path: &std::path::Path, seed_hex: &str) {
    use std::os::unix::fs::PermissionsExt;
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, format!("{seed_hex}\n")).unwrap();
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).unwrap();
}

/// The binary with no key named by the environment and no proxy between it and
/// a local node; each caller sets HOME, so `~/.qntx/key` is never the machine's.
fn bin() -> Command {
    let mut c = Command::new(env!("CARGO_BIN_EXE_ats-dev"));
    c.env_remove("ATS_DEV_KEY");
    for v in ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"] {
        c.env_remove(v);
    }
    c
}

fn run(db: &std::path::Path, args: &[&str], stdin: &str) -> Output {
    let mut c = bin()
        .env("HOME", db.parent().unwrap())
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
/// A database in a directory of its own, which is the test's HOME, holding the
/// test's key at `.qntx/key`.
fn db() -> (tempfile::TempDir, std::path::PathBuf) {
    let d = tempfile::tempdir().unwrap();
    write_key(&home_key(d.path()), SEED_HEX);
    let p = d.path().join("t.db");
    (d, p)
}
fn home_key(home: &std::path::Path) -> std::path::PathBuf { home.join(".qntx").join("key") }
/// The did:key the test's key signs as.
fn did(p: &std::path::Path) -> String {
    let v: serde_json::Value = serde_json::from_str(out(&run(p, &["key"], "")).trim()).unwrap();
    v["did"].as_str().unwrap().to_string()
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
fn a_signed_row_is_authored_by_its_signer_and_an_unsigned_one_by_ats_dev() {
    let (_d, p) = db();
    let me = did(&p);
    let o = run(&p, &["put", "ALICE is author of GITHUB"], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&o).contains(&format!("\"actors\":[\"{me}\"]")), "{}", out(&o));
    let o = run(&p, &["--unsigned", "put", "CAROL is reviewer of GITHUB"], "");
    assert!(out(&o).contains("\"actors\":[\"ats-dev\"]"), "{}", out(&o));
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"DAVE\"],\"predicates\":[\"reviewer\"]}\n");
    assert!(o.status.success(), "{}", err(&o));
    for (who, by) in [("ALICE", me.as_str()), ("CAROL", "ats-dev"), ("DAVE", me.as_str())] {
        let o = run(&p, &["ax", who], "");
        assert!(out(&o).contains(by), "ax {who} has not {by}: {}", out(&o));
    }
}

/// The node writes a request admitted as a key with that key leading the
/// actors, and a signature's actors must be those (`actorsFor`); unsigned, the
/// actors named stand alone.
#[test]
fn an_actor_that_was_named_stands_after_the_signer() {
    let (_d, p) = db();
    let me = did(&p);
    let o = run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert!(out(&o).contains(&format!("\"actors\":[\"{me}\",\"BOB\"]")), "{}", out(&o));
    let o = run(&p, &["--unsigned", "put", "ALICE is author of GITHUB by BOB"], "");
    assert!(out(&o).contains("\"actors\":[\"BOB\"]"), "{}", out(&o));
}

#[test]
fn a_date_no_store_holds_is_refused_not_kept_as_today() {
    let (_d, p) = db();
    let o = run(&p, &["--unsigned", "put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"timestamp\":9223372036854775807}\n");
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
    let o = run(&p, &["--unsigned", "put", "-"], "{\"subjects\":[\"OLD\"],\"predicates\":[\"p\"],\"actors\":[\"x\"],\"timestamp\":-1500,\"created_at\":-1500}\n");
    assert!(o.status.success(), "{}", err(&o));
    let o = run(&p, &["ax", "OLD"], "");
    assert!(out(&o).contains("\"timestamp\":-1500"), "{}", out(&o));
}

/// The node takes a timestamp in seconds, so a signature over milliseconds
/// would not verify there: a signed row's moment is a whole second.
#[test]
fn a_signed_row_s_timestamp_is_a_whole_second() {
    let (_d, p) = db();
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"timestamp\":1500}\n");
    assert!(!o.status.success() && err(&o).contains("whole second"), "{}", err(&o));
    assert_eq!(count(&p), "0");
    let o = run(&p, &["put", "-"], "{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"timestamp\":-2000}\n");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&run(&p, &["ax", "A"], "")).contains("\"timestamp\":-2000"));
    for o in [run(&p, &["put", "B is p of C"], ""), run(&p, &["put", "-"], "{\"subjects\":[\"D\"],\"predicates\":[\"p\"]}\n")] {
        assert!(o.status.success(), "{}", err(&o));
        let id = out(&o).lines().next().unwrap().trim_matches('"').to_string();
        let id = serde_json::from_str::<serde_json::Value>(&id).ok().and_then(|v| v["id"].as_str().map(str::to_string)).unwrap_or(id);
        let v: serde_json::Value = serde_json::from_str(out(&run(&p, &["get", &id], "")).trim()).unwrap();
        let at = v["timestamp"].as_i64().unwrap();
        assert_eq!(at % 1000, 0, "a timestamp ats-dev filled in is not a whole second: {at}");
    }
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
    assert!(out(&run(&p, &["actors"], "")).lines().any(|l| l == "BOB"));
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
    // the signer leads a signed row's actors, then who was named
    let mut actors: Vec<String> = out(&run(&p, &["actors"], "")).lines().map(str::to_string).collect();
    actors.sort();
    assert_eq!(actors, [did(&p), "BOB".to_string()].iter().cloned().collect::<std::collections::BTreeSet<_>>().into_iter().collect::<Vec<_>>());
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
fn put_signs_as_the_key_kept_in_qntx_key_and_mints_none() {
    let (d, p) = db();
    let k: serde_json::Value = serde_json::from_str(out(&run(&p, &["key"], "")).trim()).unwrap();
    assert_eq!(k["file"], home_key(d.path()).display().to_string());
    assert!(k["did"].as_str().unwrap().starts_with("did:key:z"));
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    run(&p, &["put", "CAROL is author of GITHUB by BOB"], "");
    let v = verdicts(&p);
    assert_eq!(v.len(), 2);
    for x in &v {
        assert_eq!(x["signature"], "valid");
        assert_eq!(x["signer"], k["did"]);
    }
    // the key is the file as it was kept: read, not rewritten
    assert_eq!(std::fs::read_to_string(home_key(d.path())).unwrap(), format!("{SEED_HEX}\n"));

    // none kept: refused, with where one comes from, and none made
    std::fs::remove_file(home_key(d.path())).unwrap();
    for args in [vec!["put", "DAVE is author of GITHUB by BOB"], vec!["key"]] {
        let o = run(&p, &args, "");
        assert!(!o.status.success() && err(&o).contains("no key is kept") && err(&o).contains("ground code login"), "{args:?}: {}", err(&o));
    }
    assert!(!home_key(d.path()).exists(), "a key was minted");
    assert_eq!(count(&p), "2");
}

#[test]
fn unsigned_writes_no_signature_and_needs_no_key() {
    let (d, p) = db();
    std::fs::remove_file(home_key(d.path())).unwrap();
    let o = run(&p, &["--unsigned", "put", "ALICE is author of GITHUB by BOB"], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(!home_key(d.path()).exists());
    assert_eq!(verdicts(&p)[0]["signature"], "unsigned");
}

#[test]
fn a_signature_given_that_does_not_hold_is_refused_and_one_that_holds_is_kept() {
    let (d, p) = db();
    let other = d.path().join("other.key");
    write_key(&other, OTHER_SEED_HEX);
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
    write_key(&other, OTHER_SEED_HEX);
    let id2 = id_of(&run(&p, &["--key", other.to_str().unwrap(), "put", "CAROL is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id2}\",\"subjects\":[\"MALLORY\"]}}")], "");
    assert!(!o.status.success() && err(&o).contains("cannot make"), "{}", err(&o));
    assert!(verdicts(&p).iter().all(|v| v["signature"] == "valid"));
}

#[test]
fn a_key_file_that_holds_no_seed_is_refused_and_left_as_it_is() {
    let (d, p) = db();
    for held in ["hello".to_string(), "1111".to_string(), format!("{SEED_HEX}00")] {
        let f = d.path().join("not.key");
        std::fs::write(&f, &held).unwrap();
        std::fs::set_permissions(&f, std::os::unix::fs::PermissionsExt::from_mode(0o600)).unwrap();
        let o = run(&p, &["--key", f.to_str().unwrap(), "put", "ALICE is author of GITHUB by BOB"], "");
        assert!(!o.status.success() && err(&o).contains("no 32-byte ed25519 seed in hex"), "{}", err(&o));
        assert_eq!(std::fs::read_to_string(&f).unwrap(), held);
    }
    assert_eq!(count(&p), "0");
}

#[test]
fn an_update_that_empties_a_signed_row_s_actors_leaves_its_signer_and_names_no_signature() {
    let (_d, p) = db();
    let me = did(&p);
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"actors\":[]}}")], "");
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(out(&run(&p, &["actors"], "")).trim(), me, "the signer leads, as the node writes it");
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
fn a_signature_already_made_needs_no_key() {
    let (_d, p) = db();
    let line = out(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], "")).lines().next().unwrap().to_string();
    let (d2, q) = db();
    std::fs::remove_file(home_key(d2.path())).unwrap();
    let o = run(&q, &["put", "-"], &line);
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(verdicts(&q)[0]["signature"], "valid");
    assert!(!home_key(d2.path()).exists());
}

/// A row signed by another key on an earlier build, carried here as text: a
/// signature stays valid wherever the row is put.
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
fn refusing_an_edit_to_a_foreign_signed_row_with_no_key_kept_mints_none() {
    let (d, p) = db();
    std::fs::remove_file(home_key(d.path())).unwrap();
    assert!(run(&p, &["put", "-"], SIGNED).status.success());
    let id = serde_json::from_str::<serde_json::Value>(SIGNED).unwrap()["id"].as_str().unwrap().to_string();
    let o = run(&p, &["update", &format!("{{\"id\":\"{id}\",\"subjects\":[\"ZED\"]}}")], "");
    assert!(!o.status.success() && err(&o).contains("the key kept is none"), "{}", err(&o));
    assert!(!home_key(d.path()).exists(), "a refused edit minted a key");
}

#[test]
fn a_key_file_open_to_group_or_other_is_refused_by_every_command_that_reads_it() {
    use std::os::unix::fs::PermissionsExt;
    let (home, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    let key = home_key(home.path());
    let id = out(&run(&p, &["ids"], "")).trim().to_string();
    for mode in [0o644, 0o640, 0o604, 0o660] {
        std::fs::set_permissions(&key, std::fs::Permissions::from_mode(mode)).unwrap();
        for args in [
            vec!["put", "CAROL is author of GITHUB by BOB"],
            vec!["key"],
        ] {
            let o = run(&p, &args, "");
            assert!(!o.status.success() && err(&o).contains("open to other users"), "{mode:o} {args:?}: {}", err(&o));
            assert!(err(&o).contains("chmod 600"), "{}", err(&o));
        }
        let patch = format!("{{\"id\":\"{id}\",\"subjects\":[\"ZED\"]}}");
        let o = run(&p, &["update", &patch], "");
        assert!(!o.status.success() && err(&o).contains("open to other users"), "{mode:o}: {}", err(&o));
    }
    assert_eq!(count(&p), "1");
    // reading and `--unsigned` need no key
    assert!(run(&p, &["ax", "ALICE"], "").status.success());
    assert!(run(&p, &["--unsigned", "put", "DAVE is author of GITHUB by BOB"], "").status.success());
    // and the fix is the one the error names
    std::fs::set_permissions(&key, std::fs::Permissions::from_mode(0o600)).unwrap();
    assert!(run(&p, &["put", "CAROL is author of GITHUB by BOB"], "").status.success());
    // a key that is not a regular file is not a key
    let d = tempfile::tempdir().unwrap();
    let o = run(&p, &["--key", d.path().to_str().unwrap(), "key"], "");
    assert!(!o.status.success() && err(&o).contains("not a regular file"), "{}", err(&o));
}

fn bench_lines(o: &Output) -> Vec<String> {
    out(o).lines().map(str::to_string).collect()
}

#[test]
fn bench_load_prints_a_line_per_step_and_the_queries_and_keeps_its_workload_fixed() {
    let (_d, p) = db();
    let o = run(&p, &["bench", "load", "300", "--step", "100"], "");
    assert!(o.status.success(), "{}", err(&o));
    let lines = bench_lines(&o);
    let header = lines.iter().position(|l| l.starts_with("n_done,")).unwrap();
    let steps: Vec<&String> = lines[header + 1..].iter().take_while(|l| !l.starts_with('#')).collect();
    assert_eq!(steps.len(), 3, "{lines:?}");
    assert!(steps[2].starts_with("300,"), "{}", steps[2]);
    assert!(lines.iter().any(|l| l.starts_with("# final db_bytes=")), "{lines:?}");
    assert_eq!(count(&p), "300");
    // the query rows are a function of the workload, so a second load of the same size agrees
    let rows = |lines: &[String]| -> Vec<String> {
        let q = lines.iter().position(|l| l.starts_with("# queries")).unwrap();
        lines[q + 1..].iter().map(|l| l.split(',').take(2).collect::<Vec<_>>().join(",")).collect()
    };
    let again = bench_lines(&run(&p, &["bench", "load", "300", "--step", "100", "--replace"], ""));
    assert_eq!(rows(&lines), rows(&again));
    assert_eq!(rows(&lines).len(), 7);
}

#[test]
fn bench_load_refuses_to_replace_a_database_unasked_and_leaves_it_alone() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    let o = run(&p, &["bench", "load", "50"], "");
    assert!(!o.status.success() && err(&o).contains("--replace"), "{}", err(&o));
    assert_eq!(count(&p), "1");
    assert!(run(&p, &["bench", "load", "50", "--replace"], "").status.success());
    assert_eq!(count(&p), "50");
}

#[test]
fn bench_batch_probe_and_query_run_on_what_load_made() {
    let (_d, p) = db();
    let o = run(&p, &["bench", "load", "200", "--batch", "50"], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&o).contains("# mode=batch:50"), "{}", out(&o));
    assert_eq!(count(&p), "200");
    let o = run(&p, &["bench", "probe", "20"], "");
    assert!(o.status.success(), "{}", err(&o));
    assert!(out(&o).contains("# probe 20 per-put writes"), "{}", out(&o));
    assert_eq!(count(&p), "220");
    let o = run(&p, &["bench", "query"], "");
    assert!(o.status.success() && out(&o).contains("by_subject(rare)"), "{}", err(&o));
}

#[test]
fn bench_refuses_what_it_cannot_run() {
    let (_d, p) = db();
    for (args, why) in [
        (vec!["bench"], "needs load"),
        (vec!["bench", "nope"], "unknown bench subcommand"),
        (vec!["bench", "load"], "N needs a number"),
        (vec!["bench", "load", "0"], "at least 1"),
        (vec!["bench", "load", "abc"], "not a number"),
        (vec!["bench", "load", "10", "--bogus"], "unknown bench load flag"),
    ] {
        let o = run(&p, &args, "");
        assert!(!o.status.success() && err(&o).contains(why), "{args:?}: {}", err(&o));
    }
}

#[test]
fn a_probe_continues_the_series_so_its_queries_read_what_a_load_of_that_size_reads() {
    let line = |o: &Output, label: &str| -> String {
        out(o).lines().find(|l| l.starts_with(label)).unwrap().split(',').take(2).collect::<Vec<_>>().join(",")
    };
    let (_d, p) = db();
    run(&p, &["bench", "load", "300"], "");
    let after_probe = run(&p, &["bench", "probe", "20"], "");
    assert!(after_probe.status.success(), "{}", err(&after_probe));
    let (_d2, q) = db();
    let plain = run(&q, &["bench", "load", "320"], "");
    for label in ["window(newest 1%)", "window(oldest 1%)", "by_actor(common)"] {
        assert_eq!(line(&after_probe, label), line(&plain, label), "{label}");
    }
}

#[test]
fn the_bytes_written_are_printed_to_two_decimals_not_rounded_to_whole_megabytes() {
    let (_d, p) = db();
    let o = run(&p, &["bench", "load", "20", "--step", "20"], "");
    let row = out(&o).lines().find(|l| l.starts_with("20,")).unwrap().to_string();
    let written = row.split(',').nth(7).unwrap();
    let (whole, frac) = written.split_once('.').unwrap_or((written, ""));
    assert!(frac.len() == 2 && whole.parse::<u64>().is_ok(), "{row}");
}

/// The `created_at` of the attestation on a line of output.
fn created_at_of(line: &str) -> String {
    line.split("\"created_at\":").nth(1).unwrap().split([',', '}']).next().unwrap().to_string()
}

#[test]
fn an_update_that_names_created_at_does_not_move_it() {
    let (_d, p) = db();
    let put = run(&p, &["--unsigned", "put", "ALICE is author of GITHUB by BOB"], "");
    let id = out(&put).split("\"id\":\"").nth(1).unwrap().split('"').next().unwrap().to_string();
    let created = created_at_of(&out(&put));
    let patch = format!("{{\"id\":\"{id}\",\"source\":\"changed\",\"created_at\":1}}");
    let u = run(&p, &["--unsigned", "update", &patch], "");
    assert!(u.status.success(), "{}", err(&u));
    assert_eq!(created_at_of(&out(&u)), created, "{}", out(&u));
    assert!(out(&u).contains("\"source\":\"changed\""));
    assert_eq!(created_at_of(&out(&run(&p, &["get", &id], ""))), created);
}

/// `run` for a backend: the global flags go before `--db`, and `db` is whatever
/// that backend calls its location (a path, a url, a connection string).
fn run_on(backend: &[&str], db: &str, args: &[&str], stdin: &str) -> Output {
    let mut c = bin()
        .env("HOME", std::env::temp_dir().join("ats-dev-test-home-with-no-key"))
        .args(backend)
        .arg("--db")
        .arg(db)
        .args(args)
        .env_remove("QNTX_POSTGRES_URL")
        .env_remove("ATS_DEV_DB")
        .env_remove("ATS_DEV_BACKEND")
        .env_remove("ATS_DEV_KEY")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    c.stdin.take().unwrap().write_all(stdin.as_bytes()).unwrap();
    c.wait_with_output().unwrap()
}

#[test]
fn a_backend_that_does_not_exist_is_refused() {
    let (_d, p) = db();
    let o = run_on(&["--backend", "mysql"], p.to_str().unwrap(), &["count"], "");
    assert!(!o.status.success() && err(&o).contains("--backend takes sqlite, duckdb or postgres"), "{}", err(&o));
}

#[test]
fn what_is_sqlites_is_refused_on_another_backend_before_anything_is_opened() {
    let (_d, p) = db();
    let db = p.to_str().unwrap();
    for (flags, command, why) in [
        (vec!["--backend", "duckdb", "--sync", "off"], vec!["count"], "--sync is SQLite's"),
        (vec!["--backend", "postgres", "--sync", "off"], vec!["count"], "--sync is SQLite's"),
        (vec!["--backend", "sqlite", "--ca", "x.pem"], vec!["count"], "--ca is Postgres's"),
        (vec!["--backend", "duckdb", "--ca", "x.pem"], vec!["count"], "--ca is Postgres's"),
        (vec!["--backend", "duckdb"], vec!["bench", "query"], "this command is SQLite's"),
        (vec!["--backend", "postgres"], vec!["bench", "query"], "this command is SQLite's"),
    ] {
        let o = run_on(&flags, db, &command, "");
        assert!(!o.status.success() && err(&o).contains(why), "{flags:?} {command:?}: {}", err(&o));
    }
    assert!(!p.exists(), "nothing was created");
}

#[test]
fn the_key_is_the_same_whatever_the_backend() {
    let d = tempfile::tempdir().unwrap();
    let key = d.path().join("k");
    write_key(&key, SEED_HEX);
    let mut dids = Vec::new();
    for (backend, db) in [("sqlite", d.path().join("t.db").display().to_string()), ("postgres", "host=/nowhere user=x".into()), ("duckdb", "s3://bucket/prefix".into())] {
        let o = run_on(&["--backend", backend, "--key", key.to_str().unwrap()], &db, &["key"], "");
        assert!(o.status.success(), "{backend}: {}", err(&o));
        dids.push(serde_json::from_str::<serde_json::Value>(out(&o).trim()).unwrap()["did"].clone());
    }
    assert!(dids.iter().all(|x| x == &dids[0]), "{dids:?}");
}

#[cfg(not(feature = "duckdb"))]
#[test]
fn a_build_without_duckdb_says_how_to_get_it() {
    let d = tempfile::tempdir().unwrap();
    let o = run_on(&["--backend", "duckdb"], d.path().to_str().unwrap(), &["count"], "");
    assert!(!o.status.success() && err(&o).contains("built without duckdb") && err(&o).contains("--features duckdb"), "{}", err(&o));
}

#[cfg(not(feature = "postgres"))]
#[test]
fn a_build_without_postgres_says_how_to_get_it() {
    let o = run_on(&["--backend", "postgres"], "host=/nowhere", &["count"], "");
    assert!(!o.status.success() && err(&o).contains("built without postgres") && err(&o).contains("--features postgres"), "{}", err(&o));
}

#[test]
fn postgres_without_a_url_says_where_one_comes_from() {
    let o = Command::new(env!("CARGO_BIN_EXE_ats-dev"))
        .args(["--backend", "postgres", "count"])
        .env_remove("QNTX_POSTGRES_URL")
        .env_remove("ATS_DEV_DB")
        .output()
        .unwrap();
    assert!(!o.status.success() && err(&o).contains("QNTX_POSTGRES_URL"), "{}", err(&o));
}

/// What every backend must do for the commands `AttestationStore` has, run
/// against one location. A change is either made or refused, never reported
/// as made and not there: the property that holds whichever backend answers.
#[cfg(any(feature = "duckdb", feature = "postgres"))]
fn holds_what_attestation_store_promises(backend: &[&str], db: &str, key: &str) {
    let r = |args: &[&str]| {
        let mut b = backend.to_vec();
        b.extend(["--key", key]);
        run_on(&b, db, args, "")
    };
    let put = r(&["put", "ALICE is author of GITHUB by BOB"]);
    assert!(put.status.success(), "{}", err(&put));
    let id = out(&put).split("\"id\":\"").nth(1).unwrap().split('"').next().unwrap().to_string();
    // a new process each time: what one wrote is what the next reads
    assert_eq!(out(&r(&["count"])).trim(), "1");
    // ids and verify list what count counts, or say they cannot; never fewer with an exit 0
    for args in [vec!["ids"], vec!["verify"]] {
        let o = r(&args);
        assert!(!o.status.success() || out(&o).lines().count() == 1, "{args:?} exit {} listed {:?} of 1: {}", o.status, out(&o), err(&o));
    }
    assert_eq!(out(&r(&["exists", &id])).trim(), "true");
    assert!(out(&r(&["get", &id])).contains("\"source\":\"ats-dev\""));
    let v = r(&["verify", &id]);
    assert!(v.status.success() && out(&v).contains("\"valid\""), "{}{}", out(&v), err(&v));

    // update: changed, or refused; never Ok with the old row left. A patch that
    // names created_at does not move it: when it was first stored is kept.
    let created = created_at_of(&out(&put));
    let patch = format!("{{\"id\":\"{id}\",\"source\":\"changed\",\"created_at\":1}}");
    let u = r(&["update", &patch]);
    let after = out(&r(&["get", &id]));
    let now_changed = after.contains("\"source\":\"changed\"");
    assert_eq!(created_at_of(&after), created, "an update rewrote created_at: {after}");
    assert_eq!(u.status.success(), now_changed, "update exit {} but stored changed={now_changed}: {}", u.status, err(&u));

    // delete: removed, or refused; never Ok with the row still there
    let d = r(&["delete", &id]);
    let still = out(&r(&["exists", &id])).trim() == "true";
    assert_eq!(d.status.success(), !still, "delete exit {} but exists={still}: {}", d.status, err(&d));

    // clear: cleared, or refused; never Ok with rows left
    let again = r(&["put", "BOB is author of GITHUB by ALICE"]);
    assert!(again.status.success(), "{}", err(&again));
    let c = r(&["clear", "--yes"]);
    let left = out(&r(&["count"])).trim().to_string();
    assert_eq!(c.status.success(), left == "0", "clear exit {} but {left} left: {}", c.status, err(&c));

    // and what only SqliteStore implements is refused by name, not answered from nothing
    for (args, needs) in [
        (vec!["ax", "ALICE"], "QueryStore and AliasStore"),
        (vec!["subjects"], "QueryStore"),
        (vec!["stats"], "QueryStore"),
        (vec!["alias", "ls"], "AliasStore"),
    ] {
        let o = r(&args);
        assert!(!o.status.success() && err(&o).contains(&format!("does not implement {needs}")), "{args:?}: {}", err(&o));
    }
}

#[cfg(feature = "duckdb")]
#[test]
fn duckdb_keeps_across_processes_what_put_wrote_and_reports_only_changes_it_made() {
    let d = tempfile::tempdir().unwrap();
    let loc = d.path().join("parquet");
    let key = d.path().join("k");
    write_key(&key, SEED_HEX);
    holds_what_attestation_store_promises(&["--backend", "duckdb"], loc.to_str().unwrap(), key.to_str().unwrap());
}

#[cfg(feature = "duckdb")]
#[test]
fn duckdb_says_the_ids_of_put_stdin_only_once_they_are_in_a_file() {
    let d = tempfile::tempdir().unwrap();
    let loc = d.path().join("parquet");
    let b = ["--backend", "duckdb", "--unsigned"];
    let o = run_on(&b, loc.to_str().unwrap(), &["put", "-"], "{\"subjects\":[\"A\"]}\n{\"subjects\":[\"B\"]}\n{\"subjects\":[\"C\"]}\n");
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(out(&o).lines().count(), 3);
    assert_eq!(out(&run_on(&b, loc.to_str().unwrap(), &["count"], "")).trim(), "3");
}

#[cfg(feature = "duckdb")]
#[test]
fn duckdb_put_stdin_stops_at_a_bad_line_and_keeps_and_says_what_came_before() {
    let d = tempfile::tempdir().unwrap();
    let loc = d.path().join("parquet");
    let b = ["--backend", "duckdb", "--unsigned"];
    let o = run_on(&b, loc.to_str().unwrap(), &["put", "-"], "{\"subjects\":[\"A\"]}\nnot json\n{\"subjects\":[\"C\"]}\n");
    assert!(!o.status.success() && err(&o).contains("line 2"), "{}", err(&o));
    assert!(err(&o).contains("no batch to roll back"), "{}", err(&o));
    // the id said is a row stored, and the line after the bad one was never read
    assert_eq!(out(&o).lines().count(), 1);
    assert_eq!(out(&run_on(&b, loc.to_str().unwrap(), &["count"], "")).trim(), "1");
}

#[cfg(feature = "postgres")]
fn postgres() -> (String, String, tempfile::TempDir) {
    let url = std::env::var("QNTX_POSTGRES_URL").expect("QNTX_POSTGRES_URL names the server (scripts/with-postgres.sh)");
    let ns = format!("t{}-{}", std::process::id(), std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_nanos());
    (url, ns, tempfile::tempdir().unwrap())
}

#[cfg(feature = "postgres")]
#[test]
fn postgres_keeps_across_processes_what_put_wrote_and_reports_only_changes_it_made() {
    let (url, ns, d) = postgres();
    let key = d.path().join("k");
    write_key(&key, SEED_HEX);
    holds_what_attestation_store_promises(&["--backend", "postgres", "--namespace", &ns], &url, key.to_str().unwrap());
}

#[cfg(feature = "postgres")]
#[test]
fn postgres_changes_what_it_is_asked_to_and_a_namespace_is_its_own() {
    let (url, ns, _d) = postgres();
    let a = ["--backend", "postgres", "--unsigned", "--namespace", &ns];
    let other = format!("{ns}-other");
    let b = ["--backend", "postgres", "--unsigned", "--namespace", &other];
    let put = run_on(&a, &url, &["put", "ALICE is author of GITHUB by BOB"], "");
    assert!(put.status.success(), "{}", err(&put));
    let id = out(&put).split("\"id\":\"").nth(1).unwrap().split('"').next().unwrap().to_string();
    let patch = format!("{{\"id\":\"{id}\",\"source\":\"changed\"}}");
    let u = run_on(&a, &url, &["update", &patch], "");
    assert!(u.status.success() && out(&u).contains("\"source\":\"changed\""), "{}", err(&u));
    assert_eq!(out(&run_on(&b, &url, &["count"], "")).trim(), "0");
    assert!(run_on(&a, &url, &["delete", &id], "").status.success());
    assert_eq!(out(&run_on(&a, &url, &["count"], "")).trim(), "0");
    let gone = run_on(&a, &url, &["delete", &id], "");
    assert!(!gone.status.success() && err(&gone).contains("no such attestation"), "{}", err(&gone));
}

#[test]
fn a_refused_bench_load_replace_deletes_nothing() {
    let (d, p) = db();
    assert!(run(&p, &["--unsigned", "put", "ALICE is author of GITHUB by BOB"], "").status.success());
    let db = p.to_str().unwrap();
    for backend in ["duckdb", "postgres"] {
        let o = run_on(&["--backend", backend], db, &["bench", "load", "10", "--replace"], "");
        assert!(!o.status.success() && err(&o).contains("this command is SQLite's"), "{backend}: {}", err(&o));
        assert!(p.exists(), "{backend}: the SQLite file named by --db was deleted by a refused command");
    }
    assert_eq!(count(&p), "1");
    drop(d);
}

/// `run_on` with raw bytes on stdin and a working directory.
fn run_raw(backend: &[&str], cwd: &std::path::Path, db: &str, args: &[&str], stdin: &[u8]) -> Output {
    let mut c = bin()
        .env("HOME", cwd)
        .args(backend)
        .arg("--db")
        .arg(db)
        .args(args)
        .current_dir(cwd)
        .env_remove("QNTX_POSTGRES_URL")
        .env_remove("ATS_DEV_DB")
        .env_remove("ATS_DEV_BACKEND")
        .env_remove("ATS_DEV_KEY")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    c.stdin.take().unwrap().write_all(stdin).unwrap();
    c.wait_with_output().unwrap()
}

#[test]
fn a_stdin_that_cannot_be_read_rolls_back_the_open_batch_and_says_so() {
    let (d, p) = db();
    let o = run_raw(&["--unsigned"], d.path(), p.to_str().unwrap(), &["put", "-"], b"{\"subjects\":[\"A\"]}\n\xff\xfe\n");
    assert!(!o.status.success() && err(&o).contains("line 2: could not read stdin"), "{}", err(&o));
    assert!(err(&o).contains("rolled back 1"), "{}", err(&o));
    assert_eq!(out(&o), "", "an id is said only for a row that is kept");
    assert_eq!(count(&p), "0");
}

#[cfg(feature = "duckdb")]
#[test]
fn duckdb_says_the_id_of_a_row_kept_before_a_stdin_that_cannot_be_read() {
    let d = tempfile::tempdir().unwrap();
    let loc = d.path().join("parquet");
    let b = ["--backend", "duckdb", "--unsigned"];
    let o = run_raw(&b, d.path(), loc.to_str().unwrap(), &["put", "-"], b"{\"subjects\":[\"A\"]}\n\xff\xfe\n");
    assert!(!o.status.success() && err(&o).contains("could not read stdin"), "{}", err(&o));
    assert_eq!(out(&o).lines().count(), 1, "the row that was kept has its id said: {}", out(&o));
    assert_eq!(out(&run_on(&b, loc.to_str().unwrap(), &["count"], "")).trim(), "1");
}

#[test]
fn a_duckdb_namespace_or_resolved_path_with_an_apostrophe_is_refused_before_anything_is_made() {
    let d = tempfile::tempdir().unwrap();
    // the namespace
    let loc = d.path().join("loc");
    let o = run_on(&["--backend", "duckdb", "--unsigned", "--namespace", "O'Brien"], loc.to_str().unwrap(), &["put", "ALICE is author of GITHUB"], "");
    assert!(!o.status.success() && err(&o).contains("DuckDB namespace cannot contain an apostrophe"), "{}", err(&o));
    assert!(!loc.exists(), "no directory was made");
    // a relative --db under a working directory that has one
    let cwd = d.path().join("O'Brien");
    std::fs::create_dir(&cwd).unwrap();
    let o = run_raw(&["--backend", "duckdb", "--unsigned"], &cwd, "rel", &["put", "ALICE is author of GITHUB"], b"");
    assert!(!o.status.success() && err(&o).contains("DuckDB location cannot contain an apostrophe"), "{}", err(&o));
    assert!(!cwd.join("rel").exists(), "no directory was made");
}

#[test]
fn a_duckdb_location_with_an_apostrophe_is_refused_before_anything_is_written() {
    let d = tempfile::tempdir().unwrap();
    let loc = d.path().join("O'Brien");
    let o = run_on(&["--backend", "duckdb", "--unsigned"], loc.to_str().unwrap(), &["put", "ALICE is author of GITHUB"], "");
    assert!(!o.status.success() && err(&o).contains("cannot contain an apostrophe"), "{}", err(&o));
    assert!(!loc.exists(), "nothing was created");
}

/// A node of one request: it keeps the request line, the Authorization and the
/// body it was sent, and answers `status` with what `answer` makes of the body.
fn fake_node(
    status: u16,
    answer: impl Fn(&serde_json::Value) -> serde_json::Value + Send + 'static,
) -> (String, std::thread::JoinHandle<(String, String, serde_json::Value)>) {
    use std::io::{BufRead, BufReader, Read};
    let l = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", l.local_addr().unwrap());
    let h = std::thread::spawn(move || {
        let (mut s, _) = l.accept().unwrap();
        let mut r = BufReader::new(s.try_clone().unwrap());
        let mut line = String::new();
        r.read_line(&mut line).unwrap();
        let (mut auth, mut len) = (String::new(), 0usize);
        loop {
            let mut h = String::new();
            r.read_line(&mut h).unwrap();
            let h = h.trim_end();
            if h.is_empty() {
                break;
            }
            let (k, v) = h.split_once(':').unwrap();
            match k.to_ascii_lowercase().as_str() {
                "authorization" => auth = v.trim().to_string(),
                "content-length" => len = v.trim().parse().unwrap(),
                _ => {}
            }
        }
        let mut body = vec![0; len];
        r.read_exact(&mut body).unwrap();
        let sent: serde_json::Value = serde_json::from_slice(&body).unwrap();
        let reply = answer(&sent).to_string();
        write!(s, "HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{reply}", reply.len()).unwrap();
        (line.trim().to_string(), auth, sent)
    });
    (url, h)
}

/// Every row taken, as the node answers a list it took.
fn all_created(sent: &serde_json::Value) -> serde_json::Value {
    let n = sent.as_array().unwrap().len();
    serde_json::json!({ "results": vec![serde_json::json!({"status": 201, "answer": {"status": "created"}}); n] })
}

fn lines_of(o: &Output) -> Vec<serde_json::Value> {
    out(o).lines().map(|l| serde_json::from_str(l).unwrap()).collect()
}

#[test]
fn push_sends_each_row_as_the_node_takes_it_on_a_request_signed_for_that_node() {
    use base64::Engine;
    let (_d, p) = db();
    let me = did(&p);
    let signed = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB", "--attr", "n=3"], ""));
    let unsigned = id_of(&run(&p, &["--unsigned", "put", "CAROL is author of GITHUB"], ""));
    let (url, node) = fake_node(200, all_created);
    let o = run(&p, &["push", &url], "");
    assert!(o.status.success(), "{}", err(&o));
    let said = lines_of(&o);
    assert_eq!(said.len(), 2, "{}", out(&o));
    assert!(said.iter().all(|l| l["status"] == 201), "{said:?}");

    let (line, auth, sent) = node.join().unwrap();
    assert_eq!(line, "POST /api/attestations HTTP/1.1");

    // the request: a JWT the key signs as its did:key, made out to this node, once
    let b64 = base64::engine::general_purpose::URL_SAFE_NO_PAD;
    let jwt = auth.strip_prefix("Signed ").expect("the request carries a Signed authorization");
    let parts: Vec<&str> = jwt.split('.').collect();
    let claims: serde_json::Value = serde_json::from_slice(&b64.decode(parts[1]).unwrap()).unwrap();
    assert_eq!(claims["iss"], me.as_str());
    assert_eq!(claims["sub"], me.as_str());
    assert_eq!(claims["aud"], serde_json::json!([url]));
    let pk = libp2p_identity::ed25519::PublicKey::try_from_bytes(&ats_sign::didkey::decode(&me).unwrap()).unwrap();
    assert!(pk.verify(format!("{}.{}", parts[0], parts[1]).as_bytes(), &b64.decode(parts[2]).unwrap()), "the request's signature is not the key's");

    // the rows: seconds, the signature as Go reads []byte, created_at left to the node
    let rows = sent.as_array().unwrap();
    let row = |id: &str| rows.iter().find(|r| r["id"] == id).unwrap().clone();
    let stored: serde_json::Value = serde_json::from_str(out(&run(&p, &["get", &signed], "")).trim()).unwrap();
    let s = row(&signed);
    assert_eq!(s["timestamp"].as_i64().unwrap() * 1000, stored["timestamp"].as_i64().unwrap());
    assert_eq!(s["actors"][0], me.as_str(), "the signer leads, as the node writes a request admitted as it");
    assert_eq!(s["signer_did"], me.as_str());
    let sig = base64::engine::general_purpose::STANDARD.decode(s["signature"].as_str().unwrap()).unwrap();
    assert_eq!(serde_json::json!(sig), stored["signature"]);
    assert_eq!(s["attributes"]["n"], 3);
    assert!(s.get("created_at").is_none());
    let u = row(&unsigned);
    assert!(u.get("signature").is_none() && u.get("signer_did").is_none(), "{u}");
}

#[test]
fn push_says_what_the_node_refused_row_by_row_and_what_it_never_sent() {
    let (_d, p) = db();
    let ok = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let late = id_of(&run(&p, &["put", "DAVE is author of GITHUB by BOB"], ""));
    // `put -` says the ids it wrote, one per line
    let sub_second = out(&run(&p, &["--unsigned", "put", "-"], "{\"subjects\":[\"S\"],\"predicates\":[\"p\"],\"timestamp\":1500}\n")).trim().to_string();
    // the node takes the first row it is sent and refuses the second
    let (url, node) = fake_node(200, |sent| {
        let first = sent[0]["id"].clone();
        serde_json::json!({ "results": sent.as_array().unwrap().iter().map(|r| if r["id"] == first {
            serde_json::json!({"status": 201, "answer": {"status": "created"}})
        } else {
            serde_json::json!({"status": 403, "answer": {"error": "a key User does not hold"}})
        }).collect::<Vec<_>>() })
    });
    let o = run(&p, &["push", &url, &ok, &late, &sub_second, "AS-nope"], "");
    assert!(!o.status.success() && err(&o).contains("not taken"), "{}", err(&o));
    let said = lines_of(&o);
    let by_id = |id: &str| said.iter().find(|l| l["id"] == id).expect("a line for every id asked").clone();
    assert_eq!(by_id(&ok)["status"], 201);
    assert_eq!(by_id(&late)["status"], 403);
    assert_eq!(by_id(&late)["answer"]["error"], "a key User does not hold");
    assert!(by_id(&sub_second)["refused"].as_str().unwrap().contains("whole second"));
    assert_eq!(by_id("AS-nope")["refused"], "no such attestation");
    let (_, _, sent) = node.join().unwrap();
    assert_eq!(sent.as_array().unwrap().len(), 2, "a row that cannot be sent as it is was sent: {sent}");
}

#[test]
fn push_says_a_request_the_node_did_not_take_against_every_row_in_it() {
    let (_d, p) = db();
    let id = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let (url, node) = fake_node(401, |_| serde_json::json!({ "error": "this node holds no live grant" }));
    let o = run(&p, &["push", &url], "");
    assert!(!o.status.success());
    let said = lines_of(&o);
    assert_eq!(said.len(), 1);
    assert_eq!(said[0]["id"], id);
    assert_eq!(said[0]["status"], 401);
    assert_eq!(said[0]["answer"]["error"], "this node holds no live grant");
    node.join().unwrap();
}

#[test]
fn push_needs_the_key_and_a_node_url() {
    let (d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    for (args, why) in [
        (vec!["push"], "push needs a node"),
        (vec!["push", "ftp://node.example"], "http or https"),
        (vec!["push", "node.example"], "a node is a url"),
    ] {
        let o = run(&p, &args, "");
        assert!(!o.status.success() && err(&o).contains(why), "{args:?}: {}", err(&o));
    }
    std::fs::remove_file(home_key(d.path())).unwrap();
    let o = run(&p, &["push", "http://127.0.0.1:9"], "");
    assert!(!o.status.success() && err(&o).contains("no key is kept"), "{}", err(&o));
    assert_eq!(out(&o), "");
}

/// A node that reads one request and hangs up without a word.
fn node_that_hangs_up() -> (String, std::thread::JoinHandle<usize>) {
    use std::io::{BufRead, BufReader, Read};
    let l = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", l.local_addr().unwrap());
    let h = std::thread::spawn(move || {
        let (s, _) = l.accept().unwrap();
        let mut r = BufReader::new(s);
        let mut len = 0usize;
        loop {
            let mut h = String::new();
            r.read_line(&mut h).unwrap();
            if h.trim_end().is_empty() {
                break;
            }
            if let Some((k, v)) = h.split_once(':') {
                if k.eq_ignore_ascii_case("content-length") {
                    len = v.trim().parse().unwrap();
                }
            }
        }
        let mut body = vec![0; len];
        r.read_exact(&mut body).unwrap();
        serde_json::from_slice::<serde_json::Value>(&body).unwrap().as_array().unwrap().len()
    });
    (url, h)
}

#[test]
fn a_node_that_hangs_up_leaves_no_row_unsaid_and_the_rest_unsent() {
    let (_d, p) = db();
    let rows: String = (0..101).map(|i| format!("{{\"subjects\":[\"S{i}\"],\"predicates\":[\"p\"]}}\n")).collect();
    let o = run(&p, &["--unsigned", "put", "-"], &rows);
    assert!(o.status.success(), "{}", err(&o));
    let (url, node) = node_that_hangs_up();
    let o = run(&p, &["push", &url], "");
    assert!(!o.status.success());
    let said = lines_of(&o);
    assert_eq!(said.len(), 101, "every id has a line");
    let unanswered = said.iter().filter(|l| l["refused"].as_str().is_some_and(|w| w.starts_with(&url) && w.contains("did not answer"))).count();
    let unsent = said.iter().filter(|l| l["refused"].as_str().is_some_and(|w| w.starts_with("not sent:"))).count();
    assert_eq!((unanswered, unsent), (100, 1), "{said:?}");
    assert!(err(&o).contains("101 not taken"), "{}", err(&o));
    assert!(err(&o).contains("100 sent"), "the summary is said: {}", err(&o));
    assert_eq!(node.join().unwrap(), 100, "a request carries one batch, not the store");
}

#[test]
fn a_row_the_node_already_had_is_said_apart_from_one_it_took() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    let (url, node) = fake_node(200, |sent| {
        serde_json::json!({ "results": vec![serde_json::json!({"status": 200, "answer": {"status": "exists"}}); sent.as_array().unwrap().len()] })
    });
    let o = run(&p, &["push", &url], "");
    assert!(o.status.success(), "{}", err(&o));
    assert_eq!(lines_of(&o)[0]["answer"]["status"], "exists");
    assert!(err(&o).contains("0 created, 1 the node already had under that id (what it holds was not compared)"), "{}", err(&o));
    node.join().unwrap();
}

/// A push whose answers cannot be said is not a push that succeeded: the node
/// took the row, but nobody was told, and the exit says so.
#[test]
fn a_push_whose_lines_cannot_be_written_does_not_exit_0() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    let (url, node) = fake_node(200, all_created);
    let mut c = bin()
        .env("HOME", p.parent().unwrap())
        .arg("--db")
        .arg(&p)
        .args(["push", &url])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    // nobody reads what push says
    drop(c.stdout.take());
    let o = c.wait_with_output().unwrap();
    node.join().unwrap();
    assert!(!o.status.success(), "push exited 0 with its lines unwritten: {}", err(&o));
    assert!(err(&o).contains("could not write stdout"), "{}", err(&o));
}

#[test]
fn a_node_url_is_reached_as_written_and_names_no_query() {
    let (_d, p) = db();
    run(&p, &["put", "ALICE is author of GITHUB by BOB"], "");
    for (url, why) in [("https://node.example?x=y", "no query or fragment"), ("https://node.example/#a", "no query or fragment")] {
        let o = run(&p, &["push", url], "");
        assert!(!o.status.success() && err(&o).contains(why), "{url}: {}", err(&o));
    }
}

/// A node that takes `n` requests, each answered as taken, and keeps the size
/// of each body and how many rows it carried.
fn fake_node_n(n: usize) -> (String, std::thread::JoinHandle<Vec<(usize, usize)>>) {
    use std::io::{BufRead, BufReader, Read};
    let l = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", l.local_addr().unwrap());
    let h = std::thread::spawn(move || {
        let mut seen = Vec::new();
        for _ in 0..n {
            let (mut s, _) = l.accept().unwrap();
            let mut r = BufReader::new(s.try_clone().unwrap());
            let mut len = 0usize;
            loop {
                let mut h = String::new();
                r.read_line(&mut h).unwrap();
                if h.trim_end().is_empty() {
                    break;
                }
                if let Some((k, v)) = h.split_once(':') {
                    if k.eq_ignore_ascii_case("content-length") {
                        len = v.trim().parse().unwrap();
                    }
                }
            }
            let mut body = vec![0; len];
            r.read_exact(&mut body).unwrap();
            let sent: serde_json::Value = serde_json::from_slice(&body).unwrap();
            let reply = all_created(&sent).to_string();
            write!(s, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{reply}", reply.len()).unwrap();
            seen.push((len, sent.as_array().unwrap().len()));
        }
        seen
    });
    (url, h)
}

/// The node refuses a request past 10 MB whole, so rows are sent in requests
/// under it, and a row too large for any request is said and not sent.
#[test]
fn push_keeps_each_request_under_what_the_node_reads() {
    let (_d, p) = db();
    let blob = |mib: usize| "x".repeat(mib * 1024 * 1024);
    let rows = format!(
        "{{\"subjects\":[\"A\"],\"predicates\":[\"p\"],\"attributes\":{{\"b\":\"{}\"}}}}\n{{\"subjects\":[\"B\"],\"predicates\":[\"p\"],\"attributes\":{{\"b\":\"{}\"}}}}\n{{\"subjects\":[\"C\"],\"predicates\":[\"p\"],\"attributes\":{{\"b\":\"{}\"}}}}\n",
        blob(5), blob(5), blob(9)
    );
    let o = run(&p, &["--unsigned", "put", "-"], &rows);
    assert!(o.status.success(), "{}", err(&o));
    let (url, node) = fake_node_n(2);
    let o = run(&p, &["push", &url], "");
    let said = lines_of(&o);
    assert_eq!(said.len(), 3, "{}", err(&o));
    assert_eq!(said.iter().filter(|l| l["status"] == 201).count(), 2, "{said:?}");
    let too_big: Vec<_> = said.iter().filter(|l| l["refused"].as_str().is_some_and(|w| w.contains("carries at most"))).collect();
    assert_eq!(too_big.len(), 1, "{said:?}");
    assert!(!o.status.success(), "a row not sent is a row not taken");
    let seen = node.join().unwrap();
    assert_eq!(seen.iter().map(|(_, rows)| *rows).collect::<Vec<_>>(), [1, 1], "{seen:?}");
    assert!(seen.iter().all(|(bytes, _)| *bytes < 10 * 1000 * 1000), "{seen:?}");
}

/// A row that names a signer and no signature, or a signature and no signer,
/// is not sent as unsigned: the node would sign it as itself and the claim of a
/// signer would be gone. ats-dev never writes one; another writer of the file can.
#[test]
fn push_refuses_a_row_with_half_a_signature() {
    use ats::AttestationStore;
    let (_d, p) = db();
    {
        let mut s = ats_sqlite::SqliteStore::open(&p).unwrap();
        for (id, sig, did) in [
            ("AS-half-1", Some(vec![1u8, 2, 3]), None),
            ("AS-half-2", None, Some("did:key:zSomeone".to_string())),
        ] {
            let a = ats::Attestation {
                id: id.into(), subjects: vec!["S".into()], predicates: vec!["p".into()], actors: vec!["x".into()],
                timestamp: 1_000, created_at: 1_000, signature: sig, signer_did: did, ..ats::Attestation::default()
            };
            s.put(a).unwrap();
        }
    }
    let ok = id_of(&run(&p, &["put", "ALICE is author of GITHUB by BOB"], ""));
    let (url, node) = fake_node(200, all_created);
    let o = run(&p, &["push", &url], "");
    assert!(!o.status.success());
    let said = lines_of(&o);
    for id in ["AS-half-1", "AS-half-2"] {
        let l = said.iter().find(|l| l["id"] == id).unwrap();
        assert!(l["refused"].as_str().unwrap().contains("together or not at all"), "{l}");
    }
    let (_, _, sent) = node.join().unwrap();
    let ids: Vec<_> = sent.as_array().unwrap().iter().map(|r| r["id"].as_str().unwrap().to_string()).collect();
    assert_eq!(ids, [ok]);
}

#[test]
fn an_empty_home_names_no_key() {
    let (_d, p) = db();
    let o = bin().env("HOME", "").arg("--db").arg(&p).args(["put", "ALICE is author of GITHUB by BOB"]).output().unwrap();
    assert!(!o.status.success() && err(&o).contains("home directory is unknown"), "{}", err(&o));
}
