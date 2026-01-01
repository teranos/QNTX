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
          name = "ghcr.io/teranos/qntx";
          tag = "latest";

          contents = [
            # Go toolchain
            pkgs.go
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
            pkgs.go
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.sqlite
          ];
        };
      }
    );
}
