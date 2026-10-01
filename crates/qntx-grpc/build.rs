fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Only compile protos if the plugin feature is enabled
    #[cfg(feature = "plugin")]
    {
        use std::path::PathBuf;

        // Which protos and which include root come from qntx-proto's build
        // script, not from this one. A service compiled here whose messages
        // qntx-proto left out does not compile, so there is one list.
        let protos_var = std::env::var_os("DEP_QNTX_PROTO_PROTOS")
            .ok_or("DEP_QNTX_PROTO_PROTOS not set: qntx-proto's build script did not run")?;
        let include_var = std::env::var_os("DEP_QNTX_PROTO_INCLUDE")
            .ok_or("DEP_QNTX_PROTO_INCLUDE not set: qntx-proto's build script did not run")?;
        let protos: Vec<PathBuf> = std::env::split_paths(&protos_var).collect();
        let repo_root = PathBuf::from(include_var);

        // Compile only gRPC services (not message types - those come from qntx-proto)
        tonic_build::configure()
            .build_server(true)
            .build_client(true)
            // Skip generating message types - we get those from qntx-proto
            .compile_well_known_types(false)
            // Use extern_path to reference types from qntx-proto instead of generating
            .extern_path(".protocol", "::qntx_proto")
            .compile_protos(&protos, &[&repo_root])?;

        println!("cargo:rerun-if-env-changed=DEP_QNTX_PROTO_PROTOS");
        println!("cargo:rerun-if-env-changed=DEP_QNTX_PROTO_INCLUDE");
        for proto in &protos {
            println!("cargo:rerun-if-changed={}", proto.display());
        }
    }

    println!("cargo:rerun-if-changed=build.rs");

    Ok(())
}
