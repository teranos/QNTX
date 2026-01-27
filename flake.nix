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
    extra-experimental-features = [ "impure-derivations" ];
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

            # Rust formatting
            rustfmt.enable = true;

            # Nix container workflow verification
            verify-nix-workflows = {
              enable = true;
              name = "Verify Nix container changes";
              entry = toString (pkgs.writeShellScript "verify-nix-workflows" ''
                # commit-msg hook receives message file as $1
                COMMIT_MSG_FILE="$1"

                # Get staged workflow files
                WORKFLOWS=$(git diff --cached --name-only | grep "^\.github/workflows/.*\.yml$" || true)

                if [ -z "$WORKFLOWS" ]; then
                  exit 0
                fi

                # Check if any workflow file uses ghcr.io/teranos/qntx container
                for workflow in $WORKFLOWS; do
                  # Check file content, not diff - triggers on ANY change to workflows using Nix container
                  if [ -f "$workflow" ] && grep -q "ghcr.io/teranos/qntx" "$workflow"; then
                    # Read commit message from file
                    COMMIT_MSG=$(cat "$COMMIT_MSG_FILE" 2>/dev/null || echo "")

                    # Check for verification evidence
                    if ! echo "$COMMIT_MSG" | grep -qE "Verified: flake.nix:[0-9]+"; then
                      echo "❌ REJECTED: Workflow uses ghcr.io/teranos/qntx without flake.nix verification"
                      echo ""
                      echo "When modifying workflows that use the Nix container, you MUST:"
                      echo "  1. Read flake.nix mkCiImage (lines ~137-197)"
                      echo "  2. Check BOTH 'contents' AND 'config.Env PATH'"
                      echo "  3. Add verification to commit message"
                      echo ""
                      echo "Required format:"
                      echo "  Verified: flake.nix:166 (contents has pkgs.curl)"
                      echo "  Verified: flake.nix:187 (PATH includes pkgs.curl)"
                      exit 1
                    fi
                  fi
                done
              '');
              stages = [ "commit-msg" ];
              # Note: `files` parameter doesn't work with commit-msg hooks
              # File filtering is done inside the script via git diff --cached
              always_run = true;
            };

            # TypeScript type checking
            # TODO(#273): Disabled due to vendored d3 causing 83 module resolution errors
            # ts-typecheck = {
            #   enable = true;
            #   name = "TypeScript typecheck";
            #   entry = "${pkgs.nodePackages.typescript}/bin/tsc --project web/tsconfig.json --noEmit";
            #   files = "\\.ts$";
            #   pass_filenames = false;
            # };

            # Go hooks disabled - require network access to download modules
            # which isn't available in Nix sandbox. Use local git hooks instead.
            # gofmt.enable = true;
            # govet.enable = true;
          };
        };

        # Build QNTX binary with Nix (requires Rust CGO libraries)
        qntx = pkgs.buildGoModule {
          pname = "qntx";
          version = self.rev or "dev";
          src = ./.;

          # Hash of vendored Go dependencies (computed from go.sum)
          # To update: set to `lib.fakeHash`, run `nix build .#qntx`, copy the hash from error
          vendorHash = "sha256-jdpkm1mu4K4DjTZ3/MpbYE2GfwEhNH22d71PFNyes/Q=";

          # Depend on Rust libraries
          buildInputs = [
            rustLibs.fuzzy
            rustLibs.sqlite
            rustLibs.vidstream
          ];

          # Enable CGO via preBuild (CGO_ENABLED cannot be set as top-level attribute)
          preBuild = ''
            export CGO_ENABLED=1
            export CGO_LDFLAGS="-L${rustLibs.fuzzy}/lib -L${rustLibs.sqlite}/lib -L${rustLibs.vidstream}/lib"
          '';

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/qntx" ];
        };

        # Rust libraries (CGO dependencies)
        rustLibs = {
          # Fuzzy matching library (depends on qntx-core from workspace)
          fuzzy = pkgs.rustPlatform.buildRustPackage {
            pname = "qntx-fuzzy";
            version = self.rev or "dev";
            src = ./.;  # Use workspace root (fuzzy-ax is excluded from workspace)

            cargoLock = {
              lockFile = ./ats/ax/fuzzy-ax/Cargo.lock;
            };

            # Build from ats/ax/fuzzy-ax subdirectory
            buildAndTestSubdir = "ats/ax/fuzzy-ax";

            # Build only the library
            cargoBuildFlags = [ "--lib" ];

            # Install static library
            postInstall = ''
              mkdir -p $out/lib
              cp target/release/libqntx_fuzzy.a $out/lib/
            '';
          };

          # SQLite storage library
          sqlite = pkgs.rustPlatform.buildRustPackage {
            pname = "qntx-sqlite";
            version = self.rev or "dev";
            src = ./.;  # Workspace root (qntx-sqlite is part of workspace)

            cargoLock = {
              lockFile = ./Cargo.lock;
            };

            nativeBuildInputs = [ pkgs.sqlite ];

            # Build only qntx-sqlite with FFI feature
            cargoBuildFlags = [ "-p" "qntx-sqlite" "--features" "ffi" ];

            # Install static library
            postInstall = ''
              mkdir -p $out/lib
              cp target/release/libqntx_sqlite.a $out/lib/
            '';
          };

          # Video processing library (ONNX support)
          vidstream = pkgs.rustPlatform.buildRustPackage {
            pname = "qntx-vidstream";
            version = self.rev or "dev";
            src = ./ats/vidstream;

            cargoLock = {
              lockFile = ./ats/vidstream/Cargo.lock;
            };

            nativeBuildInputs = [ pkgs.pkg-config ];
            buildInputs = [ pkgs.onnxruntime ];

            # Build with ONNX feature
            cargoBuildFlags = [ "--lib" "--features" "onnx" ];

            # Install static library
            postInstall = ''
              mkdir -p $out/lib
              cp target/release/libqntx_vidstream.a $out/lib/
            '';
          };
        };

        # Build typegen binary (standalone, no plugins/CGO)
        typegen = pkgs.buildGoModule {
          pname = "typegen";
          version = self.rev or "dev";
          src = ./.;

          # Same vendorHash as qntx (shared go.mod)
          # To update: set to `lib.fakeHash`, run `nix build .#typegen`, copy the hash from error
          vendorHash = "sha256-jdpkm1mu4K4DjTZ3/MpbYE2GfwEhNH22d71PFNyes/Q=";

          subPackages = [ "cmd/typegen" ];
        };

        # Build qntx-code plugin binary (requires Rust CGO libraries via ats/ax/fuzzy-ax)
        qntx-code = pkgs.buildGoModule {
          pname = "qntx-code-plugin";
          version = self.rev or "dev";
          src = ./.;

          # Same vendorHash as qntx (shared go.mod)
          # To update: set to `lib.fakeHash`, run `nix build .#qntx-code`, copy the hash from error
          vendorHash = "sha256-jdpkm1mu4K4DjTZ3/MpbYE2GfwEhNH22d71PFNyes/Q=";

          # Depend on Rust libraries
          buildInputs = [
            rustLibs.fuzzy
            rustLibs.sqlite
            rustLibs.vidstream
          ];

          # Enable CGO via preBuild (CGO_ENABLED cannot be set as top-level attribute)
          preBuild = ''
            export CGO_ENABLED=1
            export CGO_LDFLAGS="-L${rustLibs.fuzzy}/lib -L${rustLibs.sqlite}/lib -L${rustLibs.vidstream}/lib"
          '';

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
          # and qntx-python is part of the workspace
          src = ./.;

          cargoLock = {
            lockFile = ./Cargo.lock;
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
              "PATH=${pkgs.lib.makeBinPath [ qntx pkgs.go pkgs.git pkgs.rustc pkgs.cargo pkgs.rustfmt pkgs.clippy pkgs.python313 pkgs.pkg-config pkgs.gcc pkgs.gnumake pkgs.coreutils pkgs.diffutils pkgs.findutils pkgs.bash pkgs.curl ]}"
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

          # Rust CGO libraries
          rust-fuzzy = rustLibs.fuzzy;
          rust-sqlite = rustLibs.sqlite;
          rust-vidstream = rustLibs.vidstream;

          # Plugin binaries
          qntx-code = qntx-code;
          qntx-python = qntx-python;

          # Static documentation site with provenance and infrastructure docs
          # For CI builds with full provenance, pass additional args
          docs-site = pkgs.callPackage ./sitegen.nix {
            gitRevision = self.rev or self.dirtyRev or "unknown";
            gitShortRev = self.shortRev or self.dirtyShortRev or "unknown";
            gitCommitDate = if self ? lastModified then self.lastModified else null;

            # Nix infrastructure metadata for self-documenting builds
            nixPackages = [
              { name = "qntx"; description = "QNTX CLI - main command-line interface"; }
              { name = "typegen"; description = "Type generator for TypeScript, Python, Rust, and Markdown"; }
              { name = "qntx-code"; description = "Code analysis plugin with Git integration"; }
              { name = "qntx-python"; description = "Python runtime plugin with PyO3"; }
              { name = "docs-site"; description = "Static documentation website"; }
            ];

            nixApps = [
              { name = "build-docs-site"; description = "Build documentation and copy to web/site/"; }
              { name = "generate-types"; description = "Generate types for all languages"; }
              { name = "check-types"; description = "Verify generated types are up-to-date"; }
              { name = "generate-proto"; description = "Generate gRPC code from proto files"; }
            ];

            nixContainers = [
              {
                name = "CI Image";
                description = "Full development environment for CI/CD pipelines";
                image = "ghcr.io/teranos/qntx:latest";
                architectures = [ "amd64" "arm64" ];
                ports = [ ];
              }
              {
                name = "qntx-code Plugin";
                description = "Code analysis plugin container";
                image = "ghcr.io/teranos/qntx-code-plugin:latest";
                architectures = [ "amd64" "arm64" ];
                ports = [ "9000/tcp" ];
              }
              {
                name = "qntx-python Plugin";
                description = "Python runtime plugin container";
                image = "ghcr.io/teranos/qntx-python-plugin:latest";
                architectures = [ "amd64" "arm64" ];
                ports = [ "9000/tcp" ];
              }
            ];
          };

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
          buildInputs = [
            pkgs.go
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.sqlite
            pkgs.python313
            pkgs.pkg-config
            pkgs.protobuf
            pkgs.onnxruntime
            # WASM tooling
            pkgs.wasm-pack
            pkgs.wasm-bindgen-cli
          ] ++ pre-commit-check.enabledPackages;

          # Make Python available to PyO3 builds in dev shell
          PYO3_PYTHON = "${pkgs.python313}/bin/python3";

          # Make ONNX Runtime available to Rust builds (vidstream)
          shellHook = pre-commit-check.shellHook + ''
            export LD_LIBRARY_PATH="${pkgs.onnxruntime}/lib:''${LD_LIBRARY_PATH:-}"
            export DYLD_LIBRARY_PATH="${pkgs.onnxruntime}/lib:''${DYLD_LIBRARY_PATH:-}"
            export ORT_DYLIB_PATH="${pkgs.onnxruntime}/lib"
            export ORT_LIB_LOCATION="${pkgs.onnxruntime}/lib"

            # Ensure WASM target is available
            if ! rustup target list --installed | grep -q wasm32-unknown-unknown; then
              echo "Installing wasm32-unknown-unknown target..."
              rustup target add wasm32-unknown-unknown 2>/dev/null || true
            fi
          '';
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

        # Formatter for 'nix fmt'
        formatter = pkgs.nixpkgs-fmt;

        # Apps for common tasks
        apps = {
          build-docs-site = {
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

          generate-types = {
            type = "app";
            program = toString (pkgs.writeShellScript "generate-types" ''
              set -e
              echo "Generating types and documentation..."

              # Check if typegen binary exists and is up-to-date
              REBUILD_NEEDED=0
              if [ ! -e ./result/bin/typegen ]; then
                echo "Typegen binary not found, building..."
                REBUILD_NEEDED=1
              elif [ code/typegen -nt ./result/bin/typegen ]; then
                echo "Typegen source changed, rebuilding..."
                REBUILD_NEEDED=1
              fi

              # Build typegen only if needed
              if [ $REBUILD_NEEDED -eq 1 ]; then
                ${pkgs.nix}/bin/nix build .#typegen
              else
                echo "Using existing typegen binary (skip rebuild)"
              fi

              # Run typegen for each language in parallel
              # Note: Rust types now output directly to crates/qntx/src/types/ (no --output flag)
              # CSS types output directly to web/css/generated/ (no --output flag)
              echo "Running typegen for all languages in parallel..."
              (
                ./result/bin/typegen --lang typescript --output types/generated/ &
                ./result/bin/typegen --lang python --output types/generated/ &
                ./result/bin/typegen --lang rust &
                ./result/bin/typegen --lang css &
                ./result/bin/typegen --lang markdown &
                wait
              )

              echo "✓ TypeScript types generated in types/generated/typescript/"
              echo "✓ Python types generated in types/generated/python/"
              echo "✓ Rust types generated in crates/qntx/src/types/"
              echo "✓ CSS symbols generated in web/css/generated/"
              echo "✓ Markdown docs generated in docs/types/"
            '');
          };

          check-types = {
            type = "app";
            program = toString (pkgs.writeShellScript "check-types" ''
              set -e
              # Run typegen check inside dev environment where Go is available.
              #
              # NOTE: typegen uses golang.org/x/tools/go/packages which requires
              # the 'go' command at runtime to load and parse Go packages. This is
              # a known limitation of go/packages - it shells out to 'go list' for
              # module resolution and type checking.
              #
              # Current approach: Run inside 'nix develop' where Go is in PATH.
              # More proper solution: Wrap the typegen binary with makeWrapper to
              # include Go in its runtime closure. This would make the binary truly
              # self-contained but requires changes to the typegen package definition.
              ${pkgs.nix}/bin/nix develop .#default --command bash -c "go run ./cmd/typegen check"
            '');
          };

          generate-proto = {
            type = "app";
            program = toString (pkgs.writeShellScript "generate-proto" ''
              set -e
              echo "Generating gRPC code from proto files..."

              # Use protoc from nixpkgs with Go plugins
              ${pkgs.protobuf}/bin/protoc \
                --plugin=${pkgs.protoc-gen-go}/bin/protoc-gen-go \
                --plugin=${pkgs.protoc-gen-go-grpc}/bin/protoc-gen-go-grpc \
                --go_out=. --go_opt=paths=source_relative \
                --go-grpc_out=. --go-grpc_opt=paths=source_relative \
                plugin/grpc/protocol/domain.proto

              echo "✓ Proto files generated in plugin/grpc/protocol/"
            '');
          };
        };
      }
    );
}

