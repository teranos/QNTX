// The DuckDB version is the pin's, server/parity/duckdb_<version>_<rev>, and
// this crate only refers to it: "Make sure the version remains solely in one
// location, everything else needs to simply ref it".

use std::fs;
use std::path::Path;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let dir = Path::new("../../server/parity");
    println!("cargo:rerun-if-changed={}", dir.display());

    let mut found = Vec::new();
    for entry in fs::read_dir(dir).map_err(|e| format!("failed to read {}: {e}", dir.display()))? {
        let name = entry?.file_name().to_string_lossy().into_owned();
        if let Some(rest) = name.strip_prefix("duckdb_") {
            let (version, _rev) = rest
                .split_once('_')
                .ok_or_else(|| format!("{name} under {} names no commit", dir.display()))?;
            found.push(version.to_string());
        }
    }
    match found.as_slice() {
        [version] => {
            println!("cargo:rustc-env=QNTX_DUCKDB_PIN={version}");
            Ok(())
        }
        [] => Err(format!("duckdb is not pinned under {}", dir.display()).into()),
        _ => Err(format!(
            "duckdb is pinned more than once under {}: {}",
            dir.display(),
            found.join(", ")
        )
        .into()),
    }
}
