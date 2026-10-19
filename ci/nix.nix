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
  };

  jobs = {
    # Fast formatting and pre-commit checks (run on both OS)
    lint = {
      "if" = notTheVault;
      strategy.matrix.os = [ "ubuntu-latest" "macos-latest" ];
      runs-on = "\${{ matrix.os }}";
      steps = setup ++ [
        {
          name = "Check Nix formatting";
          run = "nix run nixpkgs#nixpkgs-fmt -- --check *.nix";
        }

        {
          name = "Run pre-commit checks";
          run = "nix build --show-trace .#checks.\${{ matrix.os == 'ubuntu-latest' && 'x86_64-linux' || 'aarch64-darwin' }}.pre-commit";
        }
      ];
    };

    # Core Go binaries (parallel builds per OS)
    build-go = {
      "if" = notTheVault;
      strategy.matrix = {
        os = [ "ubuntu-latest" "macos-latest" ];
        package = [ "qntx" ];
      };
      runs-on = "\${{ matrix.os }}";
      steps = setup ++ [
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
