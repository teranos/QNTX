use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Use protoc-bin-vendored to avoid needing protoc installed
    std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path().unwrap());

    // QNTX_PROTO_DIR: override proto file location for out-of-workspace builds.
    // Default assumes crates/qntx-proto/ inside the QNTX workspace.
    let proto_dir = match std::env::var("QNTX_PROTO_DIR") {
        Ok(dir) => PathBuf::from(dir),
        Err(_) => PathBuf::from("../../plugin/grpc/protocol"),
    };
    let protos: Vec<PathBuf> = std::fs::read_dir(&proto_dir)
        .unwrap_or_else(|e| panic!("cannot read proto dir {}: {}", proto_dir.display(), e))
        .filter_map(|entry| {
            let path = entry.ok()?.path();
            if path.extension().is_some_and(|ext| ext == "proto") {
                Some(path)
            } else {
                None
            }
        })
        .collect();

    let mut config = prost_build::Config::new();
    config.type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]");
    config.type_attribute(".", "#[serde(rename_all = \"snake_case\")]");

    // Attestation: accept missing fields as defaults (Go omitempty omits zero values)
    config.type_attribute("protocol.Attestation", "#[serde(default)]");

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
    config.field_attribute("protocol.WriteToGroundRequest.attributes", struct_serde);

    // Repeated string fields: accept JSON null as empty vec (Go nil slices marshal to null)
    let vec_default = "#[serde(default)]";
    for msg in &[
        "Attestation",
        "AttestationCommand",
        "AttestationFilter",
        "WriteToGroundRequest",
    ] {
        for field in &["subjects", "predicates", "contexts", "actors"] {
            config.field_attribute(format!("protocol.{}.{}", msg, field), vec_default);
        }
    }

    // Signature: Go's []byte JSON-marshals as base64 string; use custom serde
    config.field_attribute(
        "protocol.Attestation.signature",
        concat!(
            "#[serde(",
            "default, ",
            "serialize_with = \"crate::base64_serde::serialize\", ",
            "deserialize_with = \"crate::base64_serde::deserialize\"",
            ")]"
        ),
    );
    config.field_attribute("protocol.Attestation.signer_did", "#[serde(default)]");

    config.compile_protos(&protos, &[&proto_dir])?;

    println!("cargo:rerun-if-changed={}", proto_dir.display());
    for proto in &protos {
        println!("cargo:rerun-if-changed={}", proto.display());
    }

    Ok(())
}
