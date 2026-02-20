use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Use protoc-bin-vendored to avoid needing protoc installed
    std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path().unwrap());

    let protocol_dir = PathBuf::from("../../plugin/grpc/protocol");
    let core_proto_dir = PathBuf::from("../../proto");

    let protocol_protos = [
        protocol_dir.join("domain.proto"),
        protocol_dir.join("atsstore.proto"),
        protocol_dir.join("queue.proto"),
        protocol_dir.join("server.proto"),
    ];

    let core_protos = [
        core_proto_dir.join("sym.proto"),
    ];

    let mut config = prost_build::Config::new();
    config.type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]");
    config.type_attribute(".", "#[serde(rename_all = \"snake_case\")]");

    // Compile plugin protocol protos
    config.compile_protos(&protocol_protos, &[&protocol_dir])?;

    // Compile core protos (sym, etc.)
    config.compile_protos(&core_protos, &[&core_proto_dir])?;

    Ok(())
}
