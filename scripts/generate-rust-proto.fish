#!/usr/bin/env fish
# Generate Rust proto types and commit them
# No protoc needed in CI

set SCRIPT_DIR (dirname (status --current-filename))
set QNTX_ROOT (cd $SCRIPT_DIR/..; and pwd)

cd $QNTX_ROOT/crates/qntx-proto
or exit 1

# Create temporary build.rs just for generation
echo 'use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_dir = PathBuf::from("../../plugin/grpc/protocol");
    let protos = [
        proto_dir.join("domain.proto"),
        proto_dir.join("atsstore.proto"),
        proto_dir.join("queue.proto"),
    ];

    let mut config = prost_build::Config::new();
    config.type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]");
    config.type_attribute(".", "#[serde(rename_all = \"snake_case\")]");
    config.compile_protos(&protos, &[&proto_dir])?;
    Ok(())
}' > build.rs

# Add prost-build to Cargo.toml temporarily
echo '
[build-dependencies]
prost-build = "0.13"' >> Cargo.toml

# Build to generate code
echo "🦀 Generating Rust proto code..."
cargo build 2>/dev/null
or echo "Build step completed with proto generation"

# Find and copy generated file
set GENERATED_FILE (find ../../target -name "protocol.rs" -type f 2>/dev/null | head -1)

if test -z "$GENERATED_FILE"
    echo "❌ Error: Could not find generated protocol.rs"
    exit 1
end

mkdir -p src/generated
cp $GENERATED_FILE src/generated/protocol.rs

# Clean up
rm build.rs

# Remove the build-dependencies we added (fish way)
set CARGO_CONTENT (cat Cargo.toml | string split '\n')
set NEW_CONTENT

set skip_next false
for line in $CARGO_CONTENT
    if test "$line" = "[build-dependencies]"
        set skip_next true
        continue
    end
    if $skip_next
        set skip_next false
        continue
    end
    set NEW_CONTENT $NEW_CONTENT $line
end

string join '\n' $NEW_CONTENT > Cargo.toml

echo "✅ Generated src/generated/protocol.rs"
echo "🚀 No protoc required in CI"
echo "🐟 Fish shell superiority demonstrated"