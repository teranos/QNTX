{
  description = "QNTX Reduce Plugin - UMAP dimensionality reduction for QNTX";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        # Python with umap-learn and dependencies
        pythonEnv = pkgs.python313.withPackages (ps: with ps; [
          umap-learn
          numpy
          scikit-learn
        ]);

        # Build qntx-reduce plugin binary (Rust + PyO3)
        qntx-reduce = pkgs.rustPlatform.buildRustPackage {
          pname = "qntx-reduce-plugin";
          version = self.rev or "dev";
          # Include full repo root because build.rs needs ../plugin/grpc/protocol/*.proto
          src = ./..;

          cargoLock = {
            lockFile = ./../Cargo.lock;
          };

          buildInputs = with pkgs; [
            protobuf
            pythonEnv
            openssl
          ];

          nativeBuildInputs = with pkgs; [
            pkg-config
            protobuf
            makeWrapper
          ];

          # Set Python for PyO3 — points to env with umap-learn pre-installed
          PYO3_PYTHON = "${pythonEnv}/bin/python3";

          # Build only the qntx-reduce-plugin package
          cargoBuildFlags = [ "-p" "qntx-reduce-plugin" ];
          cargoTestFlags = [ "-p" "qntx-reduce-plugin" ];

          # 1. Set rpath/install_name so the binary finds libpython at runtime
          # 2. Wrap with PYTHONHOME so the embedded interpreter finds
          #    withPackages site-packages (umap-learn, numpy, scikit-learn)
          postFixup = pkgs.lib.optionalString pkgs.stdenv.isLinux ''
            patchelf --set-rpath "${pkgs.lib.makeLibraryPath [ pythonEnv ]}:$(patchelf --print-rpath $out/bin/qntx-reduce-plugin)" \
              $out/bin/qntx-reduce-plugin
          '' + pkgs.lib.optionalString pkgs.stdenv.isDarwin ''
            install_name_tool -add_rpath "${pkgs.lib.makeLibraryPath [ pythonEnv ]}" \
              $out/bin/qntx-reduce-plugin
          '' + ''
            wrapProgram $out/bin/qntx-reduce-plugin \
              --set PYTHONHOME "${pythonEnv}"
          '';
        };
      in
      {
        packages = {
          default = qntx-reduce;
          qntx-reduce-plugin = qntx-reduce;
        };

        apps.default = {
          type = "app";
          program = "${qntx-reduce}/bin/qntx-reduce-plugin";
        };
      });
}
