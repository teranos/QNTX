# The Docs workflow, emitted to .github/workflows/docs.yml and never edited by hand:
#   nix eval --json --file ci/docs.nix | jq . > .github/workflows/docs.yml

# "ci for docs, only the one docs ci"
let
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

  # The PR QNTX opens from the vault is not built or checked, as the Nix workflow says it.
  notTheVault = (import ./nix.nix).jobs.lint."if";
in
{
  name = "Docs";

  # A newer push to the same ref makes this run's answer stale.
  concurrency = {
    group = "\${{ github.workflow }}-\${{ github.ref }}";
    cancel-in-progress = true;
  };

  on = {
    push = {
      branches = [ "main" ];
      paths = [ "docs/**" "sitegen/**" ];
    };
    pull_request.paths = [
      "docs/**"
      "sitegen/**"
      ".github/workflows/docs.yml"
    ];
  };

  # Documentation, with its links checked.
  jobs.build-docs = {
    "if" = notTheVault;
    runs-on = "ubuntu-latest";
    steps = setup ++ [
      {
        name = "Build and validate docs";
        run = ''
          nix build --show-trace .#checks.x86_64-linux.docs-site-builds
          nix build --show-trace .#checks.x86_64-linux.docs-site-links
        '';
      }

      {
        name = "Push to Cachix";
        "if" = "github.ref == 'refs/heads/main'";
        run = "nix build .#docs-site --print-out-paths --no-link | cachix push qntx";
      }
    ];
  };
}
