{
  description = "QNTX CI container image";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # CI image with Go + Rust toolchain
        ciImage = pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-ci";
          tag = "latest";

          contents = [
            # Go toolchain
            pkgs.go_1_23  # nixpkgs may not have 1.24 yet, use latest stable
            pkgs.git

            # Rust toolchain with rustfmt
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt

            # Build tools and utilities
            pkgs.sqlite
            pkgs.gcc
            pkgs.gnumake
            pkgs.coreutils
            pkgs.findutils
            pkgs.bash

            # CA certificates for HTTPS
            pkgs.cacert
          ];

          config = {
            Env = [
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
              "PATH=/usr/bin:/bin"
            ];
            WorkingDir = "/workspace";
          };
        };

      in
      {
        packages = {
          ci-image = ciImage;
          default = ciImage;
        };

        # Development shell with same tools
        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go_1_23
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.sqlite
          ];
        };
      }
    );
}
