# The TypeScript workflow. .github/workflows/ts.yml is emitted from this and is
# never edited by hand:
#
#   nix eval --json --file ci/ts.nix | jq . > .github/workflows/ts.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
let
  tsPaths = [
    "**/*.ts"
    "web/package.json"
  ];
in
{
  name = "TypeScript";

  on = {
    pull_request.paths = tsPaths ++ [ ".github/workflows/ts.yml" ];
    push = {
      branches = [ "main" ];
      paths = tsPaths;
    };
    workflow_dispatch = null;
  };

  jobs.test = {
    name = "TypeScript Tests";
    runs-on = "ubuntu-latest";
    steps = [
      {
        name = "Checkout repository";
        uses = "actions/checkout@v5";
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
        run = "cargo install wasm-pack";
      }

      {
        name = "Build WASM modules";
        run = "make ats laye";
      }

      {
        name = "Set up Bun";
        uses = "oven-sh/setup-bun@v2";
        "with".bun-version = "latest";
      }

      {
        name = "Install dependencies";
        run = "cd web && bun install";
      }

      {
        name = "Type check";
        run = "cd web && bun run typecheck";
      }

      {
        name = "Run tests";
        run = "cd web && bun test";
        env.USE_JSDOM = 1;
      }
    ];
  };
}
