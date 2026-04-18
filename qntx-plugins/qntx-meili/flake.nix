{
  description = "QNTX MeiliSearch Plugin - Search provider plugin for QNTX";

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

        qntx-meili = pkgs.rustPlatform.buildRustPackage {
          pname = "qntx-meili";
          version = self.rev or "dev";
          src = ./../..;

          cargoLock = {
            lockFile = ./../../Cargo.lock;
          };

          buildInputs = with pkgs; [
            protobuf
            openssl
          ];

          nativeBuildInputs = with pkgs; [
            pkg-config
            protobuf
          ];

          cargoBuildFlags = [ "-p" "qntx-meili" ];
          cargoTestFlags = [ "-p" "qntx-meili" ];
        };

        qntx-meili-clippy = pkgs.rustPlatform.buildRustPackage {
          pname = "qntx-meili-clippy";
          version = self.rev or "dev";
          src = ../../.;

          cargoLock = {
            lockFile = ./../../Cargo.lock;
          };

          nativeBuildInputs = with pkgs; [
            pkg-config
            protobuf
            clippy
          ];

          buildInputs = with pkgs; [
            protobuf
            openssl
          ];

          buildPhase = ''
            cargo clippy -p qntx-meili --all-targets -- -D warnings
          '';

          installPhase = ''
            mkdir -p $out
            echo "Clippy passed" > $out/result
          '';

          doCheck = false;
        };
      in
      {
        packages = {
          default = qntx-meili;
          qntx-meili = qntx-meili;
        };

        checks = {
          clippy = qntx-meili-clippy;
        };

        apps.default = {
          type = "app";
          program = "${qntx-meili}/bin/qntx-meili";
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            rustc
            cargo
            rustfmt
            clippy
            pkg-config
            protobuf
            openssl
          ];
        };
      });
}
