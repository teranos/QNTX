{
  description = "QNTX Type Generator - generates TypeScript, Python, Rust, CSS, and Markdown types";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    fenix = {
      url = "github:nix-community/fenix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, fenix }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

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
          src = ./..;

          cargoLock = {
            lockFile = ./../Cargo.lock;
          };

          cargoBuildFlags = [ "-p" "qntx-wasm" "--target" "wasm32-unknown-unknown" ];
          doCheck = false;

          installPhase = ''
            mkdir -p $out/lib
            cp target/wasm32-unknown-unknown/release/qntx_wasm.wasm $out/lib/qntx_core.wasm
          '';
        };

        # Build typegen binary
        # Requires qntxwasm tag because typegen compiles the whole codebase
        # via golang.org/x/tools/go/packages (NeedTypes)
        typegen = pkgs.buildGoModule {
          pname = "typegen";
          version = self.rev or "dev";
          src = ./..;

          # Same vendorHash as qntx (shared go.mod)
          # To update: set to `pkgs.lib.fakeHash`, run `nix build ./typegen`, copy the hash from error
          vendorHash = "sha256-Ix5m8m578Jj5mEUy/K1zSWy/wJK9zBO9bGHacPrwOoA=";

          preBuild = ''
            cp ${qntx-wasm}/lib/qntx_core.wasm ats/wasm/qntx_core.wasm
          '';

          # HACK: Need qntxwasm tag because typegen compiles the whole codebase
          # This will be removed when we migrate to protoc-based generation
          tags = [ "qntxwasm" ];

          subPackages = [ "cmd/typegen" ];
        };
      in
      {
        packages = {
          default = typegen;
          typegen = typegen;
          qntx-wasm = qntx-wasm;
        };

        checks = {
          typegen-build = typegen;
        };

        apps.default = {
          type = "app";
          program = "${typegen}/bin/typegen";
        };
      });
}
