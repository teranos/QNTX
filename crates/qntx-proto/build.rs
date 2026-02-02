use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_dir = PathBuf::from("../../plugin/grpc/protocol");
    let protos = [
        proto_dir.join("domain.proto"),
        proto_dir.join("atsstore.proto"),
        proto_dir.join("queue.proto"),
        proto_dir.join("server.proto"),
    ];

    let mut config = prost_build::Config::new();
    config.type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]");
    config.type_attribute(".", "#[serde(rename_all = \"snake_case\")]");
    config.compile_protos(&protos, &[&proto_dir])?;
    Ok(())
}
