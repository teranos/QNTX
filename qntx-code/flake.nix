{
  description = "QNTX Code Plugin - Code analysis plugin for QNTX";

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

        # Build qntx-code plugin binary
        qntx-code = pkgs.buildGoModule {
          pname = "qntx-code-plugin";
          version = self.rev or "dev";
          src = ..; # Root of QNTX repo

          # Same vendorHash as main QNTX
          vendorHash = "sha256-tEJdJ/d8bcGVBzOqNupNJz4ueO4fAK/FD2CiqNvPR4s=";

          buildInputs = with pkgs; [
            openssl
          ] ++ pkgs.lib.optionals pkgs.stdenv.isDarwin [
            darwin.apple_sdk.frameworks.IOKit
            darwin.apple_sdk.frameworks.Security
          ];

          subPackages = [ "qntx-code/cmd/qntx-code-plugin" ];
        };

        # Helper function to build qntx-code plugin image for specific architecture
        mkCodeImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-code-plugin";
          tag = "latest";
          architecture = arch;

          contents = [
            qntx-code
            # Minimal runtime dependencies
            pkgs.cacert
            pkgs.dockerTools.fakeNss
            (pkgs.writeTextDir "etc/os-release" "ID=nixos\n")
          ];

          extraCommands = ''
            mkdir -p tmp var/tmp
            chmod 1777 tmp var/tmp
          '';

          config = {
            Entrypoint = [ "${qntx-code}/bin/qntx-code-plugin" ];
            Cmd = [ "--port" "9000" ];
            Env = [
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
            ];
            ExposedPorts = {
              "9000/tcp" = { };
            };
            WorkingDir = "/";
          };

          # Docker images are Linux-only
          meta.platforms = [ "x86_64-linux" "aarch64-linux" ];
        };

        # Architecture detection
        dockerArch =
          if system == "x86_64-linux" then "amd64"
          else if system == "aarch64-linux" then "arm64"
          else "amd64";

        codeImage = mkCodeImage dockerArch;
      in
      {
        packages = {
          default = qntx-code;
          qntx-code-plugin = qntx-code;

          # Docker images (Linux-only)
          qntx-code-plugin-image = codeImage;
          qntx-code-plugin-image-amd64 = mkCodeImage "amd64";
          qntx-code-plugin-image-arm64 = mkCodeImage "arm64";
        };

        apps.default = {
          type = "app";
          program = "${qntx-code}/bin/qntx-code-plugin";
        };
      });
}
