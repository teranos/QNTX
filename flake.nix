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

        # Build QNTX binary with Nix
        qntx = pkgs.buildGoModule {
          pname = "qntx";
          version = self.rev or "dev";
          src = ./.;

          # Hash of vendored Go dependencies (computed from go.sum)
          vendorHash = "sha256-lI/fmwBx3ukxiclu4L0YfOgscA7qUpCVdBv4ex4Uu+o=";

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/qntx" ];
        };

        # CI image with Go + Rust toolchain + prebuilt QNTX binary
        ciImage = pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx";
          tag = "latest";

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

            # Tauri system dependencies (from NixOS Wiki)
            pkgs.webkitgtk_4_1
            pkgs.gtk3
            pkgs.at-spi2-atk
            pkgs.cairo
            pkgs.gdk-pixbuf
            pkgs.glib
            pkgs.harfbuzz
            pkgs.librsvg
            pkgs.libsoup_3
            pkgs.pango
            pkgs.gobject-introspection
            pkgs.openssl
            pkgs.libayatana-appindicator
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
              "PATH=${pkgs.lib.makeBinPath [ qntx pkgs.go pkgs.git pkgs.rustc pkgs.cargo pkgs.rustfmt pkgs.clippy pkgs.pkg-config pkgs.gcc pkgs.gnumake pkgs.coreutils pkgs.diffutils pkgs.findutils pkgs.bash ]}"
              "PKG_CONFIG_PATH=${pkgs.lib.makeSearchPathOutput "dev" "lib/pkgconfig" [ pkgs.glib pkgs.gtk3 pkgs.at-spi2-atk pkgs.cairo pkgs.gdk-pixbuf pkgs.harfbuzz pkgs.librsvg pkgs.libsoup_3 pkgs.pango pkgs.gobject-introspection pkgs.webkitgtk_4_1 pkgs.openssl ]}:${pkgs.lib.concatMapStringsSep ":" (p: "${p}/lib/pkgconfig") [ pkgs.libayatana-appindicator ]}"
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

