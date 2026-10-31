# The Go workflow. .github/workflows/go.yml is emitted from this and is never
# edited by hand:
#
#   nix eval --json --file ci/go.nix | jq . > .github/workflows/go.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
let
  checkout = {
    name = "Checkout repository";
    uses = "actions/checkout@v5";
  };

  # Every job builds the same toolchain before it does its own thing.
  toolchain = [
    {
      name = "Set up Go";
      uses = "actions/setup-go@v5";
      "with".go-version-file = "go.mod";
    }

    {
      name = "Cache Go modules";
      uses = "actions/cache@v4";
      "with" = {
        path = "~/go/pkg/mod";
        key = "\${{ runner.os }}-go-\${{ hashFiles('**/go.sum') }}";
        restore-keys = ''
          ''${{ runner.os }}-go-
        '';
      };
    }

    {
      name = "Download dependencies";
      run = "go mod download";
    }

    {
      name = "Install Rust toolchain";
      uses = "dtolnay/rust-toolchain@stable";
      "with".targets = "wasm32-unknown-unknown";
    }

    {
      name = "Cache Rust dependencies";
      uses = "swatinem/rust-cache@v2";
    }

    {
      name = "Install wasm-pack";
      run = "cargo install wasm-pack --locked";
    }

    {
      name = "Build WASM module";
      run = "make ats";
    }

    {
      name = "Build Rust SQLite library";
      run = "make rust-sqlite";
    }
  ];

  goPaths = [
    "**/*.go"
    "go.mod"
    "go.sum"
  ];
in
{
  name = "Go";

  # A newer push to the same ref makes this run's answer stale.
  concurrency = {
    group = "\${{ github.workflow }}-\${{ github.ref }}";
    cancel-in-progress = true;
  };

  on = {
    pull_request = {
      branches = [ "main" ];
      # Rust has the Rust CI.
      paths = goPaths ++ [ ".github/workflows/go.yml" ];
    };
    push = {
      branches = [ "main" ];
      paths = goPaths;
    };
    workflow_dispatch = null;
  };

  jobs = {
    test = {
      name = "Go Tests";
      runs-on = "ubuntu-latest";
      steps = [ checkout ] ++ toolchain ++ [
        {
          name = "Run tests";
          # Test with core tags to ensure we test what we ship
          run = ''
            export LD_LIBRARY_PATH=$PWD/target/release:$LD_LIBRARY_PATH
            go test -tags "rustsqlite,qntxwasm,qntxquote" ./...
          '';
        }
      ];
    };

    integration = {
      name = "Go Integration Tests";
      runs-on = "ubuntu-latest";
      steps = [ checkout ] ++ toolchain ++ [
        {
          name = "Run integration tests";
          run = ''
            export LD_LIBRARY_PATH=$PWD/target/release:$LD_LIBRARY_PATH
            go test -tags="rustsqlite,qntxwasm,qntxquote,integration" ./...
          '';
        }
      ];
    };

    build = {
      name = "Go Build";
      runs-on = "ubuntu-latest";
      steps = [ checkout ] ++ toolchain ++ [
        {
          name = "Build";
          run = "go build ./...";
        }
      ];
    };
  };
}
