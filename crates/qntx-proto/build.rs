use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Use protoc-bin-vendored to avoid needing protoc installed
    std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path()?);

    // QNTX_PROTO_DIR: override proto file location for out-of-workspace builds.
    // Default assumes crates/qntx-proto/ inside the QNTX workspace.
    let proto_dir = match std::env::var("QNTX_PROTO_DIR") {
        Ok(dir) => PathBuf::from(dir),
        Err(_) => PathBuf::from("../../plugin/grpc/protocol"),
    };
    // Resolved, because protoc matches an include against a file path as text:
    // it will not see that ../../plugin/grpc/protocol and a root reached by
    // ../../.. from inside it are the same tree. Its own words for this are
    // that the proto_path must be an exact prefix of the file names.
    let proto_dir = std::fs::canonicalize(&proto_dir)
        .map_err(|e| format!("cannot resolve proto dir {}: {}", proto_dir.display(), e))?;

    let protos: Vec<PathBuf> = std::fs::read_dir(&proto_dir)
        .map_err(|e| format!("cannot read proto dir {}: {}", proto_dir.display(), e))?
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

    // Accept missing fields as defaults (Go omitempty omits zero values).
    // protoc-gen-go writes omitempty on every field, so an empty string field
    // never reaches the wire and a required field would fail to parse.
    for msg in &[
        "Attestation",
        "ScheduleDeclaration",
        "ScheduleTick",
        "ScheduleProgress",
        "ScheduledJob",
    ] {
        config.type_attribute(format!("protocol.{}", msg), "#[serde(default)]");
    }

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
    config.field_attribute("protocol.LogEntry.metadata", struct_serde);

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

    // The include path is the repo root, not the proto directory, because an
    // import between these files is written as the path from the root:
    // plugin/grpc/protocol/x.proto. The Go side runs protoc from the root with
    // no -I and resolves it that way, and one import line has to satisfy both
    // generators — a proto that generates cleanly for Go and TypeScript and
    // then fails the ats build is a long way from the line that caused it.
    //
    // One root and not two: listing both would let protoc reach the same file
    // under two names and call every message in it already defined.
    let repo_root = std::fs::canonicalize(proto_dir.join("../../.."))
        .map_err(|e| format!("cannot resolve the repo root above {}: {}", proto_dir.display(), e))?;
    config.compile_protos(&protos, &[&repo_root])?;

    println!("cargo:rerun-if-changed={}", proto_dir.display());
    for proto in &protos {
        println!("cargo:rerun-if-changed={}", proto.display());
    }

    Ok(())
}
