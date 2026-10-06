# The Pi workflow. .github/workflows/pi.yml is emitted from this and is never
# edited by hand: nix eval --json --file ci/pi.nix | jq . > .github/workflows/pi.yml

# JSON is YAML, so GitHub reads the emitted file as it is.
{
  name = "Pi";

  # The ROOT agent runs in Pi too (ADR-048). The box downloads Pi from the qntx
  # cache and never builds it: a build there took the node down.

  # Built when the pin moves, and by hand.
  on = {
    push.paths = [ "internal/pi/pinned-flake" ];
    workflow_dispatch = null;
  };

  jobs.build = {
    # The box is amd64.
    runs-on = "ubuntu-latest";

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
        name = "Setup Cachix";
        uses = "cachix/cachix-action@v14";
        "with" = {
          name = "qntx";
          authToken = "\${{ secrets.CACHIX_AUTH_TOKEN }}";
        };
      }

      {
        name = "Build the pinned Pi into the cache";
        run = ''
          FLAKE="$(tr -d '[:space:]' < internal/pi/pinned-flake)"
          echo "Building $FLAKE"
          nix build "$FLAKE" --print-build-logs --print-out-paths --no-link | cachix push qntx
        '';
      }
    ];
  };
}
