# The Build CI Image workflow. .github/workflows/ci-image.yml is emitted from
# this and is never edited by hand:
#
#   nix eval --json --file ci/ci-image.nix | jq . > .github/workflows/ci-image.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
let
  ghcrLogin = {
    name = "Log in to GHCR";
    uses = "docker/login-action@v3";
    "with" = {
      registry = "ghcr.io";
      username = "\${{ github.actor }}";
      password = "\${{ secrets.GITHUB_TOKEN }}";
    };
  };

  permissions = {
    packages = "write";
    contents = "read";
  };
in
{
  name = "Build CI Image";

  on = {
    push.paths = [
      "ci/**"
      ".github/workflows/ci-image.yml"
    ];
    workflow_dispatch = null;
  };

  jobs = {
    build-push = {
      runs-on = "ubuntu-latest";
      inherit permissions;
      strategy.matrix.arch = [ "amd64" "arm64" ];

      steps = [
        {
          name = "Checkout repository";
          uses = "actions/checkout@v5";
        }

        {
          name = "Install Nix";
          uses = "cachix/install-nix-action@v26";
          "with".extra_nix_config = ''
            experimental-features = nix-command flakes
          '';
        }

        {
          name = "Build CI image";
          run = "nix build ./ci#ci-image-\${{ matrix.arch }}";
        }

        {
          name = "Load image into Docker";
          run = "docker load < result";
        }

        ghcrLogin

        {
          name = "Tag and push";
          run = ''
            docker tag ghcr.io/teranos/qntx-ci:latest ghcr.io/teranos/qntx-ci:latest-''${{ matrix.arch }}
            docker push ghcr.io/teranos/qntx-ci:latest-''${{ matrix.arch }}
          '';
        }
      ];
    };

    create-manifest = {
      runs-on = "ubuntu-latest";
      needs = "build-push";
      inherit permissions;

      steps = [
        ghcrLogin

        {
          name = "Create and push multi-arch manifest";
          run = ''
            docker manifest create ghcr.io/teranos/qntx-ci:latest \
              ghcr.io/teranos/qntx-ci:latest-amd64 \
              ghcr.io/teranos/qntx-ci:latest-arm64

            docker manifest push ghcr.io/teranos/qntx-ci:latest
          '';
        }
      ];
    };
  };
}
