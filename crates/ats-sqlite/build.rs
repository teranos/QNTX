// The migration list is generated from db/sqlite/migrations/ rather than
// hand-written. A hand-written list can drift from the directory, and it did:
// 051 was never added, so a column existed in Go's runner and not in Rust's.

// Go's runner walks the directory, so tests that use it pass while the box
// running Rust's runner is missing a table column. Generating removes the
// class rather than guarding against it.

use std::fs;
use std::path::Path;

fn main() {
    let dir = Path::new("../../db/sqlite/migrations");
    println!("cargo:rerun-if-changed={}", dir.display());

    let mut files: Vec<String> = fs::read_dir(dir)
        .unwrap_or_else(|e| panic!("failed to read {}: {e}", dir.display()))
        .filter_map(|entry| entry.ok())
        .map(|entry| entry.file_name().to_string_lossy().into_owned())
        .filter(|name| name.ends_with(".sql"))
        .collect();
    files.sort();

    let root = fs::canonicalize(dir)
        .unwrap_or_else(|e| panic!("failed to resolve {}: {e}", dir.display()));

    let mut migrations = String::from("pub const MIGRATIONS: &[(&str, &str)] = &[\n");
    let mut optional = String::from("pub const OPTIONAL_VERSIONS: &[&str] = &[\n");

    for name in &files {
        let version = match name.split_once('_') {
            Some((v, _)) => v,
            None => panic!("migration {name} has no version prefix"),
        };
        let path = root.join(name);
        migrations.push_str(&format!(
            "    ({:?}, include_str!({:?})),\n",
            version,
            path.display().to_string()
        ));
        // Matches Go: a filename containing "optional" may fail without
        // failing the run, because it depends on sqlite-vec being present.
        if name.contains("optional") {
            optional.push_str(&format!("    {version:?},\n"));
        }
    }
    migrations.push_str("];\n");
    optional.push_str("];\n");

    let out = Path::new(&std::env::var("OUT_DIR").expect("OUT_DIR")).join("migrations.rs");
    fs::write(&out, format!("{migrations}\n{optional}"))
        .unwrap_or_else(|e| panic!("failed to write {}: {e}", out.display()));
}
