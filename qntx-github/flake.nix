{
  description = "QNTX GitHub Plugin - GitHub integration for repository events";

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

        # Build qntx-github plugin binary
        qntx-github = pkgs.buildGoModule {
          pname = "qntx-github-plugin";
          version = self.rev or "dev";
          src = ../.; # Root of QNTX repo (needs parent code for imports)

          vendorHash = "sha256-7r1EjXKs6GCG1wxQdLdFgZ9FPF8E5ZuE0gVXc2lkk3o=";

          # Disable workspace for Nix vendoring
          preBuild = ''
            export GOWORK=off
          '';

          subPackages = [ "qntx-github/cmd/qntx-github-plugin" ];
        };

        # Helper function to build qntx-github plugin image for specific architecture
        mkGitHubImage = arch: pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/teranos/qntx-github-plugin";
          tag = "latest";
          architecture = arch;

          contents = [
            qntx-github
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
            Entrypoint = [ "${qntx-github}/bin/qntx-github-plugin" ];
            Cmd = [ "--port" "9002" ];
            Env = [
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
            ];
            ExposedPorts = {
              "9002/tcp" = { };
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

        githubImage = mkGitHubImage dockerArch;
      in
      {
        packages = {
          default = qntx-github;
          qntx-github-plugin = qntx-github;

          # Docker images (Linux-only)
          qntx-github-plugin-image = githubImage;
          qntx-github-plugin-image-amd64 = mkGitHubImage "amd64";
          qntx-github-plugin-image-arm64 = mkGitHubImage "arm64";
        };

        apps.default = {
          type = "app";
          program = "${qntx-github}/bin/qntx-github-plugin";
        };
      });
}
