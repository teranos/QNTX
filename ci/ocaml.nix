# The OCaml workflow. .github/workflows/ocaml.yml is emitted from this and is
# never edited by hand:
#
#   nix eval --json --file ci/ocaml.nix | jq . > .github/workflows/ocaml.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
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

  # One job per plugin: build it on both OSes, then push what was built.
  plugin = { buildName, path }: {
    strategy.matrix.os = [ "ubuntu-latest" "macos-latest" ];
    runs-on = "\${{ matrix.os }}";
    steps = setup ++ [
      {
        name = buildName;
        run = "nix build ${path} --show-trace";
      }

      {
        name = "Push to Cachix";
        run = "nix build ${path} --print-out-paths --no-link | cachix push qntx";
      }
    ];
  };
in
{
  name = "OCaml";

  on = {
    push.branches = [ "main" ];
    pull_request.paths = [
      "qntx-plugins/kern/**"
      "qntx-plugins/loom/**"
      "plugin/grpc/ocaml/**"
      ".github/workflows/ocaml.yml"
    ];
  };

  jobs = {
    build-kern = plugin {
      buildName = "Build kern plugin";
      path = "./qntx-plugins/kern";
    };

    build-loom = plugin {
      buildName = "Build loom plugin (with tests)";
      path = "./qntx-plugins/loom";
    };
  };
}
