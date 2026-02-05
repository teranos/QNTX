{
  description = "QNTX Python Plugin - Python runtime plugin for QNTX";

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

        # Build qntx-python plugin binary (Rust + PyO3)
        qntx-python = pkgs.rustPlatform.buildRustPackage {
          pname = "qntx-python-plugin";
          version = self.rev or "dev";
          # Include full repo root because build.rs needs ../plugin/grpc/protocol/*.proto
          src = ./..; # Root of QNTX repo

          cargoLock = {
            lockFile = ./../Cargo.lock;
          };

          buildInputs = with pkgs; [
            protobuf
            python313
            openssl
          ];

          nativeBuildInputs = with pkgs; [
            pkg-config
            protobuf
          ];

          # Set Python for PyO3
          PYO3_PYTHON = "${pkgs.python313}/bin/python3";

          # Build only the qntx-python-plugin package
          cargoBuildFlags = [ "-p" "qntx-python-plugin" ];
          cargoTestFlags = [ "-p" "qntx-python-plugin" ];

          # Set rpath/install_name to find Python at runtime
          postFixup = pkgs.lib.optionalString pkgs.stdenv.isLinux ''
            patchelf --set-rpath "${pkgs.lib.makeLibraryPath [ pkgs.python313 ]}:$(patchelf --print-rpath $out/bin/qntx-python-plugin)" \
              $out/bin/qntx-python-plugin
          '' + pkgs.lib.optionalString pkgs.stdenv.isDarwin ''
            install_name_tool -add_rpath "${pkgs.lib.makeLibraryPath [ pkgs.python313 ]}" \
              $out/bin/qntx-python-plugin
          '';
        };

        # Helper function to build qntx-python plugin image for specific architecture
        mkPythonImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-python-plugin";
          tag = "latest";
          architecture = arch;

          contents = [
            qntx-python
            pkgs.python313
            # Minimal runtime dependencies
            pkgs.cacert
            pkgs.dockerTools.fakeNss
            (pkgs.writeTextDir "etc/os-release" "ID=nixos\n")
          ];

          extraCommands = ''
            mkdir -p tmp var/tmp
            chmod 1777 tmp var/tmp
          '';

          config = {
            Entrypoint = [ "${qntx-python}/bin/qntx-python-plugin" ];
            Cmd = [ "--port" "9000" ];
            Env = [
              "PYTHONUNBUFFERED=1"
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
            ];
            ExposedPorts = {
              "9000/tcp" = { };
            };
            WorkingDir = "/";
          };

          # Docker images are Linux-only
          meta.platforms = [ "x86_64-linux" "aarch64-linux" ];
        };

        # Architecture detection
        dockerArch =
          if system == "x86_64-linux" then "amd64"
          else if system == "aarch64-linux" then "arm64"
          else "amd64";

        pythonImage = mkPythonImage dockerArch;

        # Clippy check derivation
        qntx-python-clippy = pkgs.stdenv.mkDerivation {
          pname = "qntx-python-clippy";
          version = self.rev or "dev";
          src = ../.;

          nativeBuildInputs = with pkgs; [
            rustPlatform.rust.cargo
            rustPlatform.rust.rustc
            pkg-config
            protobuf
            clippy
          ];

          buildInputs = with pkgs; [
            protobuf
            python313
            openssl
          ];

          PYO3_PYTHON = "${pkgs.python313}/bin/python3";

          buildPhase = ''
            cd qntx-python
            cargo clippy --all-targets -- -D warnings
          '';

          installPhase = ''
            mkdir -p $out
            echo "Clippy passed" > $out/clippy-passed
          '';
        };
      in
      {
        packages = {
          default = qntx-python;
          qntx-python-plugin = qntx-python;

          # Docker images (Linux-only)
          qntx-python-plugin-image = pythonImage;
          qntx-python-plugin-image-amd64 = mkPythonImage "amd64";
          qntx-python-plugin-image-arm64 = mkPythonImage "arm64";
        };

        checks = {
          clippy = qntx-python-clippy;
        };

        apps.default = {
          type = "app";
          program = "${qntx-python}/bin/qntx-python-plugin";
        };
      });
}
