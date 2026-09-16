# The Publish @qntx/glyphs to JSR workflow. .github/workflows/jsr.yml is
# emitted from this and is never edited by hand:
#
#   nix eval --json --file ci/jsr.nix | jq . > .github/workflows/jsr.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
{
  name = "Publish @qntx/glyphs to JSR";

  on.push = {
    branches = [ "main" ];
    paths = [ "packages/glyphs/**" ];
  };

  jobs.publish = {
    runs-on = "ubuntu-latest";
    permissions = {
      contents = "read";
      id-token = "write";
    };
    steps = [
      { uses = "actions/checkout@v5"; }
      {
        uses = "oven-sh/setup-bun@v2";
        "with".bun-version = "latest";
      }
      {
        run = "bun install";
        working-directory = "packages/glyphs";
      }
      {
        run = "bun test";
        working-directory = "packages/glyphs";
        env.USE_JSDOM = 1;
      }
      {
        run = "npx jsr publish";
        working-directory = "packages/glyphs";
      }
    ];
  };
}
