fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Only compile protos if the plugin feature is enabled
    #[cfg(feature = "plugin")]
    {
        use std::path::PathBuf;

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

        // Compile only gRPC services (not message types - those come from qntx-proto)
        tonic_build::configure()
            .build_server(true)
            .build_client(true)
            // Skip generating message types - we get those from qntx-proto
            .compile_well_known_types(false)
            // Use extern_path to reference types from qntx-proto instead of generating
            .extern_path(".protocol", "::qntx_proto")
            .compile_protos(&protos, &[&proto_dir])?;

        // Rerun if proto files change
        for proto in &protos {
            println!("cargo:rerun-if-changed={}", proto.display());
        }
    }

    println!("cargo:rerun-if-changed=build.rs");

    Ok(())
}
