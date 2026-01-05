{
  description = "QNTX container image";

  inputs = {
    # Pin to stable release channel for reproducibility
    # Update to newer releases (24.11, 25.05, etc.) as needed
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";
    flake-utils.url = "github:numtide/flake-utils";
    pre-commit-hooks = {
      url = "github:cachix/pre-commit-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
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

            # Go formatting
            gofmt.enable = true;
            govet.enable = true;

            # Auto-update vendorHash when go.mod/go.sum changes
            vendorHash-update = {
              enable = true;
              name = "Update Nix vendorHash";
              entry = "${./.githooks/update-nix-hash.sh}";
              files = "\\.(mod|sum)$";
              pass_filenames = false;
            };
          };
        };

        # Build QNTX binary with Nix
        qntx = pkgs.buildGoModule {
          pname = "qntx";
          version = self.rev or "dev";
          src = ./.;

          # Hash of vendored Go dependencies (computed from go.sum)
          vendorHash = "sha256-W2SPkC8HMfzgldH+kiEJVzIDdKHMY0gicsDJpoC02kM=";

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/qntx" ];
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

        # Architecture detection for Docker images
        dockerArch = if system == "x86_64-linux" then "amd64"
                     else if system == "aarch64-linux" then "arm64"
                     else "amd64";

        # CI image with detected architecture
        ciImage = mkCiImage dockerArch;

      in
      {
        packages = {
          ci-image = ciImage;
          ci-image-amd64 = mkCiImage "amd64";
          ci-image-arm64 = mkCiImage "arm64";
          default = ciImage;
        };

        # Development shell with same tools
        devShells.default = pkgs.mkShell {
          inherit (pre-commit-check) shellHook;

          buildInputs = [
            pkgs.go
            pkgs.rustc
            pkgs.cargo
            pkgs.rustfmt
            pkgs.sqlite
          ] ++ pre-commit-check.enabledPackages;
        };

        # Expose pre-commit checks
        checks = {
          pre-commit = pre-commit-check;
        };
      }
    );
}

