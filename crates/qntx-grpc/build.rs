fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Only compile protos if the plugin feature is enabled
    #[cfg(feature = "plugin")]
    {
        use std::path::PathBuf;

        // QNTX_PROTO_DIR: override proto file location for out-of-workspace builds.
        // Default assumes crates/qntx-grpc/ inside the QNTX workspace.
        let proto_dir = match std::env::var("QNTX_PROTO_DIR") {
            Ok(dir) => PathBuf::from(dir),
            Err(_) => PathBuf::from("../../plugin/grpc/protocol"),
        };
        // Resolved, because protoc matches an include against a file path as
        // text: it will not see that ../../plugin/grpc/protocol and a root
        // reached by ../../.. from inside it are the same tree.
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

        // The repo root is the include path, and it has to be: an import
        // between these files is written as the path from the root, because
        // that is how the Go generator resolves one. Same as qntx-proto's
        // build script — both compile the same files and must read them the
        // same way.
        let repo_root = std::fs::canonicalize(proto_dir.join("../../..")).map_err(|e| {
            format!(
                "cannot resolve the repo root above {}: {}",
                proto_dir.display(),
                e
            )
        })?;

        // Compile only gRPC services (not message types - those come from qntx-proto)
        tonic_build::configure()
            .build_server(true)
            .build_client(true)
            // Skip generating message types - we get those from qntx-proto
            .compile_well_known_types(false)
            // Use extern_path to reference types from qntx-proto instead of generating
            .extern_path(".protocol", "::qntx_proto")
            .compile_protos(&protos, &[&repo_root])?;

        // Rerun if proto files change or new ones are added
        println!("cargo:rerun-if-changed={}", proto_dir.display());
        for proto in &protos {
            println!("cargo:rerun-if-changed={}", proto.display());
        }
    }

    println!("cargo:rerun-if-changed=build.rs");

    Ok(())
}
