{
  description = "QNTX container image";

  inputs = {
    # Use unstable for latest Go version (1.24+)
    # Stable channels (24.11) only have Go 1.23
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    pre-commit-hooks = {
      # Use latest pre-commit-hooks compatible with nixpkgs 24.11
      url = "github:cachix/pre-commit-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  # Binary cache configuration
  nixConfig = {
    extra-substituters = [ "https://qntx.cachix.org" ];
    extra-trusted-public-keys = [ "qntx.cachix.org-1:sL1EkSS5871D3ycLjHzuD+/zNddU9G38HGt3qQotAtg=" ];
  };

  outputs = { self, nixpkgs, flake-utils, pre-commit-hooks }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Pre-commit hooks configuration
        pre-commit-check = pre-commit-hooks.lib.${system}.run {
          src = ./.;
          hooks = {
            # Nix formatting
            nixpkgs-fmt.enable = true;

            # Go hooks disabled - require network access to download modules
            # which isn't available in Nix sandbox. Use local git hooks instead.
            # gofmt.enable = true;
            # govet.enable = true;
          };
        };

        # Build QNTX binary with Nix
        qntx = pkgs.buildGoModule {
          pname = "qntx";
          version = self.rev or "dev";
          src = ./.;

          # Hash of vendored Go dependencies (computed from go.sum)
          vendorHash = "sha256-jdpkm1mu4K4DjTZ3/MpbYE2GfwEhNH22d71PFNyes/Q=";

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/qntx" ];
        };

        # Build typegen binary (standalone, no plugins/CGO)
        typegen = pkgs.buildGoModule {
          pname = "typegen";
          version = self.rev or "dev";
          src = ./.;

          # Same vendorHash as qntx (shared go.mod)
          vendorHash = "sha256-jdpkm1mu4K4DjTZ3/MpbYE2GfwEhNH22d71PFNyes/Q=";

          subPackages = [ "cmd/typegen" ];
        };

        # Build qntx-code plugin binary
        qntx-code = pkgs.buildGoModule {
          pname = "qntx-code-plugin";
          version = self.rev or "dev";
          src = ./.;

          # Same vendorHash as qntx (shared go.mod)
          vendorHash = "sha256-jdpkm1mu4K4DjTZ3/MpbYE2GfwEhNH22d71PFNyes/Q=";

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "qntx-code/cmd/qntx-code-plugin" ];
        };

        # Build qntx-python plugin binary (Rust + PyO3)
        qntx-python = pkgs.rustPlatform.buildRustPackage {
          pname = "qntx-python-plugin";
          version = self.rev or "dev";
          # Include full repo root because build.rs needs ../plugin/grpc/protocol/*.proto
          src = ./.;

          # Create workspace-style Cargo.toml/lock at root for Nix
          postUnpack = ''
                        # Copy files from qntx-python to root
                        cp $sourceRoot/qntx-python/Cargo.lock $sourceRoot/
                        # Create minimal workspace Cargo.toml
                        cat > $sourceRoot/Cargo.toml <<'EOF'
            [workspace]
            members = ["qntx-python"]
            EOF
          '';

          cargoLock = {
            lockFile = ./qntx-python/Cargo.lock;
          };

          nativeBuildInputs = [
            pkgs.pkg-config
            pkgs.protobuf
          ];

          buildInputs = [
            pkgs.python313
          ];

          # Point PyO3 to Nix Python
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

        # Helper function to build CI image for specific architecture
        mkCiImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx";
          tag = "latest";
          architecture = arch;

          contents = [
            # Prebuilt QNTX binary
            qntx
            # Go toolchain
            pkgs.go
            pkgs.git

            # Complete Rust toolchain
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.clippy

            # Python for qntx-python plugin builds
            pkgs.python313

            # System dependencies
            pkgs.openssl
            pkgs.patchelf

            # Build tools and utilities
            pkgs.pkg-config
            pkgs.sqlite
            pkgs.gcc
            pkgs.gnumake
            pkgs.coreutils
            pkgs.diffutils
            pkgs.findutils
            pkgs.bash
            pkgs.curl
            pkgs.unzip
            pkgs.protobuf

            # CA certificates for HTTPS
            pkgs.cacert

            # System files for GitHub Actions compatibility
            pkgs.glibc
            pkgs.dockerTools.fakeNss
            (pkgs.writeTextDir "etc/os-release" "ID=nixos\n")
          ];

          extraCommands = ''
            # Create tmp directories for Go and other build tools
            mkdir -p tmp var/tmp
            chmod 1777 tmp var/tmp
          '';

          config = {
            Env = [
              "PATH=${pkgs.lib.makeBinPath [ qntx pkgs.go pkgs.git pkgs.rustc pkgs.cargo pkgs.rustfmt pkgs.clippy pkgs.python313 pkgs.pkg-config pkgs.gcc pkgs.gnumake pkgs.coreutils pkgs.diffutils pkgs.findutils pkgs.bash ]}"
              "PKG_CONFIG_PATH=${pkgs.lib.makeSearchPathOutput "dev" "lib/pkgconfig" [ pkgs.openssl ]}"
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
              "LD_LIBRARY_PATH=${pkgs.lib.makeLibraryPath [ pkgs.stdenv.cc.cc ]}"
            ];
            WorkingDir = "/workspace";
          };

          # Docker images are Linux-only
          meta.platforms = [ "x86_64-linux" "aarch64-linux" ];
        };

        # Architecture detection for Docker images
        dockerArch =
          if system == "x86_64-linux" then "amd64"
          else if system == "aarch64-linux" then "arm64"
          else "amd64";

        # CI image with detected architecture
        ciImage = mkCiImage dockerArch;

        # Helper function to build qntx-code plugin image for specific architecture
        mkCodeImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-code-plugin";
          tag = "latest";
          architecture = arch;

          contents = [
            # The qntx-code plugin binary
            qntx-code

            # Runtime dependencies
            pkgs.gopls # Go language server (spawned as subprocess)
            pkgs.git # Git operations for ixgest
            pkgs.gh # GitHub CLI for PR operations

            # Base utilities
            pkgs.bash
            pkgs.coreutils

            # CA certificates for HTTPS
            pkgs.cacert

            # System files for container compatibility
            pkgs.dockerTools.fakeNss
          ];

          extraCommands = ''
            # Create tmp directory for runtime
            mkdir -p tmp
            chmod 1777 tmp
          '';

          config = {
            Entrypoint = [ "${qntx-code}/bin/qntx-code-plugin" ];
            Cmd = [ "--port" "9000" ];
            Env = [
              "PATH=${pkgs.lib.makeBinPath [ qntx-code pkgs.gopls pkgs.git pkgs.gh pkgs.bash pkgs.coreutils ]}"
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
            ];
            ExposedPorts = {
              "9000/tcp" = { };
            };
            WorkingDir = "/workspace";
          };

          # Docker images are Linux-only
          meta.platforms = [ "x86_64-linux" "aarch64-linux" ];
        };

        # qntx-code image with detected architecture
        codeImage = mkCodeImage dockerArch;

        # Helper function to build qntx-python plugin image for specific architecture
        mkPythonImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-python-plugin";
          tag = "latest";
          architecture = arch;

          contents = [
            # The qntx-python plugin binary
            qntx-python

            # Python runtime (required by PyO3-embedded code)
            pkgs.python313

            # Base utilities
            pkgs.bash
            pkgs.coreutils

            # CA certificates for HTTPS
            pkgs.cacert

            # System files for container compatibility
            pkgs.dockerTools.fakeNss
          ];

          extraCommands = ''
            # Create tmp directory for runtime
            mkdir -p tmp
            chmod 1777 tmp
          '';

          config = {
            Entrypoint = [ "${qntx-python}/bin/qntx-python-plugin" ];
            Cmd = [ "--port" "9000" ];
            Env = [
              "PATH=${pkgs.lib.makeBinPath [ qntx-python pkgs.python313 pkgs.bash pkgs.coreutils ]}"
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
              "LD_LIBRARY_PATH=${pkgs.lib.makeLibraryPath [ pkgs.python313 ]}"
            ];
            ExposedPorts = {
              "9000/tcp" = { };
            };
            WorkingDir = "/workspace";
          };
        };

        # qntx-python image with detected architecture
        pythonImage = mkPythonImage dockerArch;

      in
      {
        packages = {
          # QNTX CLI binary
          qntx = qntx;

          # Typegen binary (standalone, no plugins/CGO)
          typegen = typegen;

          # Plugin binaries
          qntx-code = qntx-code;
          qntx-python = qntx-python;

          # Static documentation site
          docs-site = pkgs.callPackage ./sitegen.nix { };

          # Default: CLI binary for easy installation
          default = qntx;
        } // pkgs.lib.optionalAttrs pkgs.stdenv.isLinux {
          # Docker images are Linux-only
          # CI Docker images (full dev environment)
          ci-image = ciImage;
          ci-image-amd64 = mkCiImage "amd64";
          ci-image-arm64 = mkCiImage "arm64";

          # qntx-code plugin Docker images (minimal runtime)
          qntx-code-plugin-image = codeImage;
          qntx-code-plugin-image-amd64 = mkCodeImage "amd64";
          qntx-code-plugin-image-arm64 = mkCodeImage "arm64";

          # qntx-python plugin Docker images (minimal runtime)
          qntx-python-plugin-image = pythonImage;
          qntx-python-plugin-image-amd64 = mkPythonImage "amd64";
          qntx-python-plugin-image-arm64 = mkPythonImage "arm64";
        };

        # Development shell with same tools
        devShells.default = pkgs.mkShell {
          inherit (pre-commit-check) shellHook;

          buildInputs = [
            pkgs.go
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.sqlite
            pkgs.python313
            pkgs.pkg-config
            pkgs.protobuf
          ] ++ pre-commit-check.enabledPackages;

          # Make Python available to PyO3 builds in dev shell
          PYO3_PYTHON = "${pkgs.python313}/bin/python3";
        };

        # Expose pre-commit checks
        checks = {
          pre-commit = pre-commit-check;
          qntx-build = qntx; # Ensure QNTX builds
          typegen-build = typegen; # Ensure typegen builds
          qntx-code-build = qntx-code; # Ensure qntx-code plugin builds
          qntx-python-build = qntx-python; # Ensure qntx-python plugin builds
          docs-site-builds = self.packages.${system}.docs-site; # Ensure docs site builds
          docs-site-links = pkgs.runCommand "docs-site-link-check"
            {
              nativeBuildInputs = [ pkgs.lychee ];
              docsSite = self.packages.${system}.docs-site;
            }
            ''
              # Check internal links only (offline mode)
              ${pkgs.lychee}/bin/lychee --offline --no-progress $docsSite/*.html $docsSite/**/*.html || {
                echo "Link validation failed. Some internal links are broken."
                exit 1
              }

              # Success marker
              touch $out
            '';
        } // pkgs.lib.optionalAttrs pkgs.stdenv.isLinux {
          # Docker image checks are Linux-only
          ci-image = ciImage; # Ensure CI image builds
          qntx-code-plugin-image = codeImage; # Ensure qntx-code plugin image builds
          qntx-python-plugin-image = pythonImage; # Ensure qntx-python plugin image builds
        };

        # Apps for common tasks
        apps.build-docs-site = {
          type = "app";
          program = toString (pkgs.writeShellScript "build-docs-site" ''
            set -e
            echo "Building documentation site..."
            ${pkgs.nix}/bin/nix build .#docs-site

            echo "Copying to web/site/..."
            mkdir -p web/site
            chmod -R +w web/site 2>/dev/null || true
            rm -rf web/site/*
            cp -r result/* web/site/
            chmod -R +w web/site

            echo "Documentation site built and copied to web/site/"
            echo "Files:"
            ls -lh web/site/
          '');
        };
      }
    );
}

