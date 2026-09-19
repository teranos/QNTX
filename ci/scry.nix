# The scry workflow. .github/workflows/scry.yml is emitted from this and is
# never edited by hand:
#
#   nix eval --json --file ci/scry.nix | jq . > .github/workflows/scry.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
#
# Scry is scheduled to be moved into its own repository, teranos/scry. Its
# triggers are commented out until then.
#
# let
#   scryPaths = [
#     "qntx-plugins/scry/**"
#     "plugin/grpc/protocol/domain.proto"
#     "plugin/grpc/protocol/llm.proto"
#     "plugin/grpc/protocol/proto.cmake"
#   ];
# in
{
  name = "scry";

  # on = {
  #   pull_request.paths = scryPaths ++ [ ".github/workflows/scry.yml" ];
  #   push = {
  #     branches = [ "main" ];
  #     paths = scryPaths;
  #   };
  # };
  on.workflow_dispatch = null;

  jobs.build = {
    strategy.matrix.os = [ "ubuntu-latest" "macos-latest" ];
    runs-on = "\${{ matrix.os }}";
    steps = [
      {
        name = "Checkout repository";
        uses = "actions/checkout@v5";
        "with".submodules = "recursive";
      }

      {
        name = "Install Nix";
        uses = "cachix/install-nix-action@v30";
        "with".extra_nix_config = ''
          experimental-features = nix-command flakes
        '';
      }

      {
        name = "Setup Cachix";
        uses = "cachix/cachix-action@v14";
        "with" = {
          name = "qntx";
          authToken = "\${{ secrets.CACHIX_AUTH_TOKEN }}";
        };
      }

      {
        name = "Build scry plugin";
        run = "nix build ./qntx-plugins/scry --show-trace";
      }

      {
        name = "Verify binary";
        run = "./result/bin/qntx-scry-plugin --version";
      }

      {
        name = "Push to Cachix";
        run = "nix build ./qntx-plugins/scry --print-out-paths --no-link | cachix push qntx";
      }
    ];
  };
}
