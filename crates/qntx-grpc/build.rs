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

        // Compile only gRPC services (not message types - those come from qntx-proto)
        // Use project root as include path so proto imports resolve.
        tonic_build::configure()
            .build_server(true)
            .build_client(true)
            // Skip generating message types - we get those from qntx-proto
            .compile_well_known_types(false)
            // Use extern_path to reference types from qntx-proto instead of generating
            .extern_path(".protocol", "::qntx_proto")
            .compile_protos(&protos, &[&proto_dir])?;

        // Rerun if proto files change or new ones are added
        println!("cargo:rerun-if-changed={}", proto_dir.display());
        for proto in &protos {
            println!("cargo:rerun-if-changed={}", proto.display());
        }
    }

    println!("cargo:rerun-if-changed=build.rs");

    Ok(())
}
