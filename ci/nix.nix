# The Nix workflow. .github/workflows/nix.yml is emitted from this and is never
# edited by hand:
#
#   nix eval --json --file ci/nix.nix | jq . > .github/workflows/nix.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
let
  # Every job starts from the same three steps.
  setup = [
    {
      name = "Checkout repository";
      uses = "actions/checkout@v5";
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
  ];

  # What a build job does after setup, the same on every runner.
  build = [
    # Builds against the toolchain flake.lock pins, so every run uses the
    # same rustc.
    {
      name = "Build \${{ matrix.package }}";
      run = "nix build .#\${{ matrix.package }} --show-trace";
    }

    {
      name = "Push to Cachix";
      run = "nix build .#\${{ matrix.package }} --print-out-paths --no-link | cachix push qntx";
    }
  ];

  # "when i do something, i want that the system understands that i am not to be scrutinised"
  # The PR QNTX opens from the vault (syncbvh, docs-pr) is not built or checked.
  notTheVault = "!(github.event_name == 'pull_request' && github.event.pull_request.user.login == 'sbvh-qntx[bot]' && github.head_ref == 'docs-pr')";
in
{
  name = "Nix";

  # A newer push to the same ref makes this run's answer stale.
  concurrency = {
    group = "\${{ github.workflow }}-\${{ github.ref }}";
    cancel-in-progress = true;
  };

  on = {
    push.branches = [ "main" ];
    # Only a change to nix itself: docs have the Docs CI, Go the Go CI.
    pull_request.paths = [
      "**.nix"
      "flake.lock"
      ".github/workflows/nix.yml"
    ];
    # Monday 04:00 UTC, the week's first day: the one run that builds on macOS.
    schedule = [{ cron = "0 4 * * 1"; }];
    # The macOS build asked for by hand, between two Mondays.
    workflow_dispatch = { };
  };

  jobs = {
    # Core Go binaries, on Linux for every push to main and every nix change.
    build-go = {
      "if" = notTheVault;
      strategy.matrix.package = [ "qntx" ];
      runs-on = "ubuntu-latest";
      steps = setup ++ build;
    };

    # "Decrease dependency on gh actions for macOS 3-core by changing to weekly."
    # The same build on macOS, on the weekly schedule or when asked by hand, and
    # on no push and no pull request.
    build-go-macos = {
      "if" = "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'";
      strategy.matrix.package = [ "qntx" ];
      runs-on = "macos-latest";
      steps = setup ++ build;
    };

    # Docker images (Linux only, optional for PRs)
    build-docker = {
      "if" = "github.ref == 'refs/heads/main'";
      runs-on = "ubuntu-latest";
      strategy.matrix.image = [ "ci-image" ];
      steps = setup ++ [
        {
          name = "Build \${{ matrix.image }}";
          run = "nix build --show-trace ./ci#\${{ matrix.image }}-amd64";
        }
      ];
    };
  };
}
