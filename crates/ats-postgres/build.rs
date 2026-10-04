// The migration list is generated from db/postgres/migrations/, as ats-sqlite's
// is from db/sqlite/migrations/: adding a .sql file is the whole job.

use std::fs;
use std::path::Path;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let dir = Path::new("../../db/postgres/migrations");
    println!("cargo:rerun-if-changed={}", dir.display());

    let mut files: Vec<String> = fs::read_dir(dir)
        .map_err(|e| format!("failed to read {}: {e}", dir.display()))?
        .filter_map(|entry| entry.ok())
        .map(|entry| entry.file_name().to_string_lossy().into_owned())
        .filter(|name| name.ends_with(".sql"))
        .collect();
    files.sort();

    let root =
        fs::canonicalize(dir).map_err(|e| format!("failed to resolve {}: {e}", dir.display()))?;

    let mut migrations = String::from("pub const MIGRATIONS: &[(&str, &str)] = &[\n");
    for name in &files {
        let version = match name.split_once('_') {
            Some((v, _)) => v,
            None => return Err(format!("migration {name} has no version prefix").into()),
        };
        migrations.push_str(&format!(
            "    ({:?}, include_str!({:?})),\n",
            version,
            root.join(name).display().to_string()
        ));
    }
    migrations.push_str("];\n");

    let out_dir =
        std::env::var("OUT_DIR").map_err(|e| format!("OUT_DIR is unset in a build script: {e}"))?;
    let out = Path::new(&out_dir).join("migrations.rs");
    fs::write(&out, migrations).map_err(|e| format!("failed to write {}: {e}", out.display()))?;
    Ok(())
}
