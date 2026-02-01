use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Proto files are relative to workspace root
    let proto_dir = PathBuf::from("../../plugin/grpc/protocol");

    let protos = [
        proto_dir.join("domain.proto"),
        proto_dir.join("atsstore.proto"),
        proto_dir.join("queue.proto"),
    ];

    // Check that proto files exist
    for proto in &protos {
        if !proto.exists() {
            panic!("Proto file not found: {}", proto.display());
        }
    }

    // Configure prost to generate types with serde support
    let mut config = prost_build::Config::new();

    // Add serde derives for JSON serialization
    config.type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]");
    config.type_attribute(".", "#[serde(rename_all = \"snake_case\")]");

    // Compile protos to OUT_DIR
    config.compile_protos(&protos, &[&proto_dir])?;

    // Rerun if proto files change
    for proto in &protos {
        println!("cargo:rerun-if-changed={}", proto.display());
    }

    println!("cargo:rerun-if-changed=build.rs");

    Ok(())
}
