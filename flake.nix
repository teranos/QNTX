{
  description = "QNTX container image";

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

            # System files for GitHub Actions compatibility
            pkgs.glibc
            pkgs.dockerTools.fakeNss
            (pkgs.writeTextDir "etc/os-release" "ID=nixos\n")
          ];

          extraCommands = ''
            # GitHub Actions compatibility: symlink dynamic linker
            mkdir -p lib64
            ln -sf ${pkgs.glibc}/lib/ld-linux-x86-64.so.2 lib64/ld-linux-x86-64.so.2

            # Create tmp directories for Go and other build tools
            mkdir -p tmp var/tmp
            chmod 1777 tmp var/tmp
          '';

          config = {
            Env = [
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
              "LD_LIBRARY_PATH=${pkgs.lib.makeLibraryPath [ pkgs.stdenv.cc.cc ]}"
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

