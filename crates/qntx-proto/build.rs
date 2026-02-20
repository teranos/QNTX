use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Use protoc-bin-vendored to avoid needing protoc installed
    std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path().unwrap());

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

    // google.protobuf.Struct fields need custom serde because prost_types::Struct
    // doesn't implement Serialize/Deserialize. Use our serde_struct helper module.
    let struct_serde = concat!(
        "#[serde(",
        "serialize_with = \"crate::serde_struct::serialize_option_struct\", ",
        "deserialize_with = \"crate::serde_struct::deserialize_option_struct\", ",
        "default",
        ")]"
    );
    config.field_attribute("protocol.Attestation.attributes", struct_serde);
    config.field_attribute("protocol.AttestationCommand.attributes", struct_serde);

    config.compile_protos(&protos, &[&proto_dir])?;
    Ok(())
}
