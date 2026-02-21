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
    fenix = {
      url = "github:nix-community/fenix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  # Binary cache configuration
  nixConfig = {
    extra-substituters = [ "https://qntx.cachix.org" ];
    extra-trusted-public-keys = [ "qntx.cachix.org-1:sL1EkSS5871D3ycLjHzuD+/zNddU9G38HGt3qQotAtg=" ];
    extra-experimental-features = [ "impure-derivations" ];
  };

  outputs = { self, nixpkgs, flake-utils, pre-commit-hooks, fenix }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Rust toolchain with wasm32-unknown-unknown target for qntx-wasm
        rustWasmToolchain = fenix.packages.${system}.combine [
          fenix.packages.${system}.stable.cargo
          fenix.packages.${system}.stable.rustc
          fenix.packages.${system}.targets.wasm32-unknown-unknown.stable.rust-std
        ];

        # Build qntx-core as WASM module (used by Go via go:embed)
        qntx-wasm = (pkgs.makeRustPlatform {
          cargo = rustWasmToolchain;
          rustc = rustWasmToolchain;
        }).buildRustPackage {
          pname = "qntx-wasm";
          version = self.rev or "dev";
          src = ./.;

          cargoLock = {
            lockFile = ./Cargo.lock;
          };

          cargoBuildFlags = [ "-p" "qntx-wasm" "--target" "wasm32-unknown-unknown" ];
          doCheck = false;

          # buildRustPackage expects binaries in target/release/ but we cross-compile
          installPhase = ''
            mkdir -p $out/lib
            cp target/wasm32-unknown-unknown/release/qntx_wasm.wasm $out/lib/qntx_core.wasm
          '';
        };

        # Common preBuild hook for Go derivations: copy WASM module for go:embed
        goWasmPreBuild = ''
          cp ${qntx-wasm}/lib/qntx_core.wasm ats/wasm/qntx_core.wasm
        '';

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

        # Build QNTX binary with Nix
        qntx = pkgs.buildGoModule {
          pname = "qntx";
          version = self.rev or "dev";
          src = ./.;

          # Hash of vendored Go dependencies (computed from go.sum)
          # To update: set to `pkgs.lib.fakeHash`, run `nix build .#qntx`, copy the hash from error
          vendorHash = "sha256-WjzqBFBy1E404t6cb5y6J4VZ0PFMLUok7d2Om6RMswU=";

          # sqlite3.h needed by sqlite-vec CGO bindings (db/connection.go)
          buildInputs = [ pkgs.sqlite ];

          preBuild = goWasmPreBuild;

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/qntx" ];
        };

        mkQNTXImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx";
          tag = "latest";
          architecture = arch;

          contents = [

            qntx

            # Go toolchain
            pkgs.go # TODO: Remove - build-time only
            pkgs.git # TODO: Remove - build-time only

            # Proto compiler for proto-based builds
            pkgs.protobuf # TODO: Remove - build-time only

            # Complete Rust toolchain
            pkgs.rustc # TODO: Remove - build-time only
            pkgs.cargo # TODO: Remove - build-time only

            # Python for qntx-python plugin builds
            pkgs.python313 # TODO: Remove unless plugins need Python runtime

            # System dependencies
            pkgs.openssl # Keep - runtime SSL/TLS
            pkgs.sqlite # Keep - runtime database
            pkgs.gcc # TODO: Remove - build-time only (but might be needed for CGO plugins)
            pkgs.gnumake # TODO: Remove - build-time only
            pkgs.coreutils # Keep - basic shell utilities
            pkgs.findutils # TODO: Remove - build-time only (but might be used in scripts)
            pkgs.bash # Keep - shell
            pkgs.curl # Keep - might be needed for runtime HTTP

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

        # Application image with detected architecture
        QNTXImage = mkQNTXImage dockerArch;

      in
      {
        packages = {
          # QNTX CLI binary
          qntx = qntx;

          # WASM module (qntx-core compiled to wasm32-unknown-unknown)
          qntx-wasm = qntx-wasm;

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
              { name = "qntx-wasm"; description = "qntx-core compiled to WASM for Go integration via wazero"; }
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
                name = "Image";
                description = "QNTX Application Image";
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
            ];
          };

          # Default: CLI binary for easy installation
          default = qntx;
        } // pkgs.lib.optionalAttrs pkgs.stdenv.isLinux {

          qntx-image = QNTXImage;
          qntx-image-amd64 = mkQNTXImage "amd64";
          qntx-image-arm64 = mkQNTXImage "arm64";
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
          ] ++ pre-commit-check.enabledPackages;

          # Make Python available to PyO3 builds in dev shell
          PYO3_PYTHON = "${pkgs.python313}/bin/python3";

          # Make ONNX Runtime available to Rust builds (vidstream)
          shellHook = pre-commit-check.shellHook + ''
            export LD_LIBRARY_PATH="${pkgs.onnxruntime}/lib:''${LD_LIBRARY_PATH:-}"
            export DYLD_LIBRARY_PATH="${pkgs.onnxruntime}/lib:''${DYLD_LIBRARY_PATH:-}"
            export ORT_DYLIB_PATH="${pkgs.onnxruntime}/lib"
            export ORT_LIB_LOCATION="${pkgs.onnxruntime}/lib"
          '';
        };

        # Expose pre-commit checks
        checks = {
          pre-commit = pre-commit-check;
          qntx-build = qntx; # Ensure QNTX builds
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
          qntx-image = QNTXImage; # Ensure QNTX image builds
        };

        # Formatter for 'nix fmt'
        formatter = pkgs.nixpkgs-fmt;

        # Apps for common tasks
        apps =
          let
            protoApps = import ./proto.nix { inherit pkgs; };
          in
          {
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
                elif [ cmd/typegen/main.go -nt ./result/bin/typegen ] || [ typegen -nt ./result/bin/typegen ]; then
                  echo "Typegen source changed, rebuilding..."
                  REBUILD_NEEDED=1
                fi

                # Build typegen only if needed
                if [ $REBUILD_NEEDED -eq 1 ]; then
                  ${pkgs.nix}/bin/nix build ./typegen
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

            generate-proto = protoApps.generate-proto;
            generate-proto-go = protoApps.generate-proto-go;
            generate-proto-typescript = protoApps.generate-proto-typescript;
          };
      }
    );
}
