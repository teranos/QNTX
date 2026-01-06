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
  };

  # Binary cache configuration
  nixConfig = {
    extra-substituters = [ "https://qntx.cachix.org" ];
    extra-trusted-public-keys = [ "qntx.cachix.org-1:sL1EkSS5871D3ycLjHzuD+/zNddU9G38HGt3qQotAtg=" ];
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
          vendorHash = "sha256-hpiL3bOtYDFhGcPeSaBdXR0nI0cXllpkF4uPVmhBc7Q=";

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/qntx" ];
        };

        # Build qntx-code plugin binary
        qntx-code = pkgs.buildGoModule {
          pname = "qntx-code-plugin";
          version = self.rev or "dev";
          src = ./.;

          # Same vendorHash as qntx (shared go.mod)
          vendorHash = "sha256-hpiL3bOtYDFhGcPeSaBdXR0nI0cXllpkF4uPVmhBc7Q=";

          ldflags = [
            "-X 'github.com/teranos/QNTX/internal/version.BuildTime=nix-build'"
            "-X 'github.com/teranos/QNTX/internal/version.CommitHash=${self.rev or "dirty"}'"
          ];

          subPackages = [ "cmd/plugins/code" ];

          # Rename binary to qntx-code-plugin
          postInstall = ''
            mv $out/bin/code $out/bin/qntx-code-plugin
          '';
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
            pkgs.curl
            pkgs.unzip

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
        dockerArch =
          if system == "x86_64-linux" then "amd64"
          else if system == "aarch64-linux" then "arm64"
          else "amd64";

        # CI image with detected architecture
        ciImage = mkCiImage dockerArch;

        # Helper function to build qntx-code plugin image for specific architecture
        mkCodeImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-code";
          tag = "latest";
          architecture = arch;

          contents = [
            # The qntx-code plugin binary
            qntx-code

            # Runtime dependencies
            pkgs.gopls           # Go language server (spawned as subprocess)
            pkgs.git             # Git operations for ixgest
            pkgs.gh              # GitHub CLI for PR operations

            # Base utilities
            pkgs.bash
            pkgs.coreutils

            # CA certificates for HTTPS
            pkgs.cacert

            # System files for container compatibility
            pkgs.dockerTools.fakeNss
          ];

          extraCommands = ''
            # Create tmp directory for runtime
            mkdir -p tmp
            chmod 1777 tmp
          '';

          config = {
            Entrypoint = [ "${qntx-code}/bin/qntx-code-plugin" ];
            Cmd = [ "--port" "9000" ];
            Env = [
              "PATH=${pkgs.lib.makeBinPath [ qntx-code pkgs.gopls pkgs.git pkgs.gh pkgs.bash pkgs.coreutils ]}"
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
            ];
            ExposedPorts = {
              "9000/tcp" = {};
            };
            WorkingDir = "/workspace";
          };
        };

        # qntx-code image with detected architecture
        codeImage = mkCodeImage dockerArch;

      in
      {
        packages = {
          # QNTX CLI binary
          qntx = qntx;

          # qntx-code plugin binary
          qntx-code = qntx-code;

          # CI Docker images (full dev environment)
          ci-image = ciImage;
          ci-image-amd64 = mkCiImage "amd64";
          ci-image-arm64 = mkCiImage "arm64";

          # qntx-code plugin Docker images (minimal runtime)
          qntx-code-image = codeImage;
          qntx-code-image-amd64 = mkCodeImage "amd64";
          qntx-code-image-arm64 = mkCodeImage "arm64";

          # Default: CLI binary for easy installation
          default = qntx;
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
          qntx-build = qntx; # Ensure QNTX builds
          qntx-code-build = qntx-code; # Ensure qntx-code plugin builds
          ci-image = ciImage; # Ensure CI image builds
          qntx-code-image = codeImage; # Ensure qntx-code image builds
        };
      }
    );
}

