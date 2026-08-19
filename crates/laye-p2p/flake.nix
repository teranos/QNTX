{
  description = "laye-p2p — browser social plugin (identity + libp2p + DOM). `nix run` builds + serves scratch page.";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    rust-overlay.url = "github:oxalica/rust-overlay";
    rust-overlay.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, flake-utils, rust-overlay }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ (import rust-overlay) ];
        };
        rust = pkgs.rust-bin.stable.latest.default.override {
          targets = [ "wasm32-unknown-unknown" ];
        };
        serve = pkgs.writeShellApplication {
          name = "laye-p2p-serve";
          runtimeInputs = [ rust pkgs.wasm-bindgen-cli pkgs.python3 pkgs.git ];
          text = ''
            set -eu
            root="$(git rev-parse --show-toplevel)"
            cd "$root"
            cargo build --target wasm32-unknown-unknown --release --lib --package laye-p2p
            wasm-bindgen target/wasm32-unknown-unknown/release/laye_p2p.wasm \
              --target web --out-dir crates/laye-p2p/dist --no-typescript
            cp crates/laye-p2p/web/index.html crates/laye-p2p/dist/
            echo
            echo "laye-p2p scratch page — http://localhost:8001/"
            echo
            python3 -m http.server 8001 --directory crates/laye-p2p/dist
          '';
        };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = [ rust pkgs.rust-analyzer pkgs.wasm-bindgen-cli pkgs.python3 ];
        };
        apps.default = {
          type = "app";
          program = "${serve}/bin/laye-p2p-serve";
        };
      });
}
