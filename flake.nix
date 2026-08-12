{
  description = "QNTX container image";

  inputs = {
    # Pinned to a revision, not a branch, and the revision is chosen for glibc.
    #
    # This rev carries glibc 2.40 and DuckDB 1.4.3. Both numbers are load-bearing
    # and they are not independent:
    #
    #   * libduckdb-sys generates its bindings against one DuckDB release, so the
    #     C library is not a free variable. `assert_library_version` fails the
    #     process if the linked library ever stops matching the bindings.
    #
    #   * libduckdb links glibc, and the deployment ships it to a Debian box
    #     (glibc 2.41) that supplies its own libc. glibc is backward compatible
    #     and not forward compatible, so the build's glibc must be no newer than
    #     the host's.
    #
    # nixpkgs went 2.40-66 -> 2.42-47 in one step (190f166df, 2025-12-30) and
    # never carried 2.41. DuckDB 1.5.4 exists only on the far side of that jump.
    # So "DuckDB 1.5.4" and "a libc that Debian 13 can load" cannot both be had
    # from nixpkgs — choosing the newer DuckDB chose glibc 2.42, and the host
    # answered:
    #   libc.so.6: version `GLIBC_ABI_GNU2_TLS' not found (required by libduckdb.so)
    # Nothing in 1.5.0 touches what QNTX calls (Parquet, JSON, the C API are all
    # unchanged), so the version is the cheaper thing to give up.
    #
    # Moving this rev forward means checking the host's glibc first. Raise it only
    # together with the deployment's libc floor.
    nixpkgs.url = "github:NixOS/nixpkgs/fb7944c166a3b630f177938e478f0378e64ce108";
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
    typegen = {
      url = "github:teranos/typegen";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  # Binary cache configuration
  nixConfig = {
    extra-substituters = [ "https://qntx.cachix.org" ];
    extra-trusted-public-keys = [ "qntx.cachix.org-1:sL1EkSS5871D3ycLjHzuD+/zNddU9G38HGt3qQotAtg=" ];
    extra-experimental-features = [ "impure-derivations" ];
  };

  outputs = { self, nixpkgs, flake-utils, pre-commit-hooks, fenix, typegen }:
    {
      # Shared vendorHash imported from single source of truth
      rootVendorHash = import ./nix/vendor-hash.nix;
    } // (flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # DuckDB 1.4.3 — matches the duckdb crate at 1.4.3, and is built against
        # a glibc the deployment box can load. See the nixpkgs input's comment
        # for why those two facts are the same decision.
        duckdbPinned = pkgs.duckdb;

        # Rust toolchain with wasm32-unknown-unknown target for ats-wasm
        rustWasmToolchain = fenix.packages.${system}.combine [
          fenix.packages.${system}.stable.cargo
          fenix.packages.${system}.stable.rustc
          fenix.packages.${system}.targets.wasm32-unknown-unknown.stable.rust-std
        ];

        # Build ats as WASM module (used by Go via go:embed)
        ats-wasm = (pkgs.makeRustPlatform {
          cargo = rustWasmToolchain;
          rustc = rustWasmToolchain;
        }).buildRustPackage {
          pname = "ats-wasm";
          version = self.rev or "dev";
          src = ./.;

          cargoLock = {
            lockFile = ./Cargo.lock;
          };

          cargoBuildFlags = [ "-p" "ats-wasm" "--target" "wasm32-unknown-unknown" ];
          doCheck = false;

          # buildRustPackage expects binaries in target/release/ but we cross-compile
          installPhase = ''
            mkdir -p $out/lib
            cp target/wasm32-unknown-unknown/release/ats_wasm.wasm $out/lib/ats.wasm
          '';
        };

        # Build ats-sqlite as static library for CGO linking.
        # Uses the same Rust toolchain as ats-wasm (fenix stable).
        ats-sqlite-ffi = (pkgs.makeRustPlatform {
          cargo = fenix.packages.${system}.stable.cargo;
          rustc = fenix.packages.${system}.stable.rustc;
        }).buildRustPackage {
          pname = "ats-sqlite-ffi";
          version = self.rev or "dev";
          src = ./.;

          cargoLock = {
            lockFile = ./Cargo.lock;
          };

          cargoBuildFlags = [ "-p" "ats-sqlite" "--features" "ffi" "--lib" ];
          doCheck = false;

          # buildRustPackage's default installPhase only copies binaries.
          # We need the static/shared library from the build output.
          postBuild = ''
            mkdir -p $out/lib $out/include
            find target -name 'libats_sqlite.a' -exec cp {} $out/lib/ \;
            find target -name 'libats_sqlite.so' -exec cp {} $out/lib/ \;
            cp crates/ats-sqlite/include/storage_ffi.h $out/include/
          '';

          # Skip default install (tries to find binaries, there are none)
          installPhase = "true";
        };

        # Build ats-duckdb as static library for CGO linking (ADR-024).
        # Peer of ats-sqlite-ffi. Uses Nix-provided libduckdb (no bundled compile).
        ats-duckdb-ffi = (pkgs.makeRustPlatform {
          cargo = fenix.packages.${system}.stable.cargo;
          rustc = fenix.packages.${system}.stable.rustc;
        }).buildRustPackage {
          pname = "ats-duckdb-ffi";
          version = self.rev or "dev";
          src = ./.;

          cargoLock = {
            lockFile = ./Cargo.lock;
          };

          cargoBuildFlags = [ "-p" "ats-duckdb" "--features" "ffi" "--lib" ];
          doCheck = false;

          # libduckdb 1.5.4, pinned to match the bindings — dynamic link, no
          # source compile.
          buildInputs = [ duckdbPinned ];

          postBuild = ''
            mkdir -p $out/lib $out/include
            find target -name 'libats_duckdb.a' -exec cp {} $out/lib/ \;
            find target -name 'libats_duckdb.so' -exec cp {} $out/lib/ \;
            cp crates/ats-duckdb/include/duckdb_ffi.h $out/include/
          '';

          installPhase = "true";
        };

        # Common preBuild hook for Go derivations: copy WASM module and Rust FFI libs
        goWasmPreBuild = ''
          export GOWORK=off  # Build without workspace (use go.mod only)
          cp ${ats-wasm}/lib/ats.wasm ats/wasm/ats.wasm
          mkdir -p target/release
          cp ${ats-sqlite-ffi}/lib/libats_sqlite.a target/release/
          cp ${ats-duckdb-ffi}/lib/libats_duckdb.a target/release/
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
                    if ! echo "$COMMIT_MSG" | grep -qE "Verified: ci/flake.nix:[0-9]+"; then
                      echo "❌ REJECTED: Workflow uses the CI container without ci/flake.nix verification"
                      echo ""
                      echo "When modifying workflows that use the Nix container, you MUST:"
                      echo "  1. Read ci/flake.nix — the image ci-image.yml builds"
                      echo "  2. Check BOTH 'contents' AND 'config.Env PATH'"
                      echo "  3. Add verification to commit message"
                      echo ""
                      echo "Required format:"
                      echo "  Verified: ci/flake.nix:29 (contents has pkgs.cargo)"
                      echo "  Verified: ci/flake.nix:69 (PATH includes pkgs.cargo)"
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

        # Build QNTX binary with Nix. VERSION_TAG is read from the environment
        # (release.yml sets it from `git describe --tags` — requires
        # `nix build --impure`). Empty falls back to "dev" so pure-eval
        # developer builds still work; only tagged CI builds get a real tag.
        versionTag = let v = builtins.getEnv "VERSION_TAG"; in
          if v == "" then "dev" else v;
        qntx = pkgs.buildGoModule {
          pname = "qntx";
          version = versionTag;
          src = ./.;

          # Hash of vendored Go dependencies (uses shared rootVendorHash)
          vendorHash = self.rootVendorHash;

          # sqlite3.h needed by sqlite-vec CGO bindings (db/connection.go).
          # libduckdb linked by crates/ats-duckdb — Nix build, no source recompile.
          buildInputs = [ pkgs.sqlite duckdbPinned ];

          preBuild = goWasmPreBuild;

          # Build tags: rustsqlite (ADR-013), qntxwasm (wazero WASM module),
          # rustduckdb (ADR-024 parquet backend via ats-duckdb + duckdbcgo).
          # Without rustduckdb, backend = "parquet" in am.toml would validate
          # but the duckdbcgo wrapper wouldn't be compiled in.
          tags = [ "rustsqlite" "qntxwasm" "rustduckdb" ];

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
            "-X 'github.com/teranos/QNTX/internal/version.VersionTag=${versionTag}'"
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

            # Python for qntx-reduce (PyO3) plugin builds
            pkgs.python313 # TODO: Remove unless plugins need Python runtime

            # System dependencies
            pkgs.openssl # Keep - runtime SSL/TLS
            pkgs.sqlite # Keep - runtime database
            duckdbPinned # Keep - runtime database for parquet backend (ADR-024)
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

          # WASM module (ats compiled to wasm32-unknown-unknown)
          ats-wasm = ats-wasm;

          # Rust FFI static libraries — exposed so CI can build them directly
          # without going through the full qntx binary build.
          ats-sqlite-ffi = ats-sqlite-ffi;
          ats-duckdb-ffi = ats-duckdb-ffi;

          # Static documentation site with provenance and infrastructure docs
          # For CI builds with full provenance, pass additional args
          docs-site = pkgs.callPackage ./sitegen {
            gitRevision = self.rev or self.dirtyRev or "unknown";
            gitShortRev = self.shortRev or self.dirtyShortRev or "unknown";
            gitCommitDate = if self ? lastModified then self.lastModified else null;

            # Nix infrastructure metadata for self-documenting builds
            nixPackages = [
              { name = "qntx"; description = "QNTX CLI - main command-line interface"; }
              { name = "typegen"; description = "Type generator for TypeScript, Python, Rust, and Markdown (github:teranos/typegen)"; }
              { name = "qntx-code"; description = "Code analysis plugin with Git integration"; }
              { name = "ats-wasm"; description = "ats compiled to WASM for Go integration via wazero"; }
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
            # Without this, `cargo clippy` in the dev shell falls through PATH
            # to a rustup clippy-driver, which stands in for rustc while
            # checking and then rejects every artifact this toolchain built
            # (E0514). The versions must come from the same place.
            pkgs.clippy
            pkgs.sqlite
            duckdbPinned
            pkgs.python313
            pkgs.pkg-config
            pkgs.protobuf
            pkgs.onnxruntime
          ] ++ pre-commit-check.enabledPackages;

          # Make Python available to PyO3 builds in dev shell
          PYO3_PYTHON = "${pkgs.python313}/bin/python3";

          # Make ONNX Runtime available to Rust builds (embeddings)
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
                TYPEGEN="${typegen.packages.${system}.default}/bin/typegen"
                echo "Generating types and documentation..."

                # Run typegen for each language in parallel
                echo "Running typegen for all languages in parallel..."
                pids=()
                $TYPEGEN --lang typescript --output types/generated/ & pids+=($!)
                $TYPEGEN --lang python --output types/generated/ & pids+=($!)
                $TYPEGEN --lang rust & pids+=($!)
                $TYPEGEN --lang css & pids+=($!)
                $TYPEGEN --lang markdown & pids+=($!)
                failed=0
                for pid in "''${pids[@]}"; do
                  wait "$pid" || failed=1
                done
                if [ "$failed" -ne 0 ]; then
                  echo "✗ One or more typegen jobs failed" >&2
                  exit 1
                fi

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
                TYPEGEN="${typegen.packages.${system}.default}/bin/typegen"
                # Run typegen check inside dev environment where Go is available.
                #
                # NOTE: typegen uses golang.org/x/tools/go/packages which requires
                # the 'go' command at runtime to load and parse Go packages.
                #
                # Run from repo root so typegen can access server/routing.go for API docs
                ${pkgs.nix}/bin/nix develop .#default --command bash -c "$TYPEGEN check"
              '');
            };

            generate-proto = protoApps.generate-proto;
            generate-proto-go = protoApps.generate-proto-go;
            generate-proto-typescript = protoApps.generate-proto-typescript;
            generate-proto-ocaml = protoApps.generate-proto-ocaml;
          };
      }
    ));
}
