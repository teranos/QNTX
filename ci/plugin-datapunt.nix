# The plugin-datapunt workflow. .github/workflows/plugin-datapunt.yml is
# emitted from this and is never edited by hand:
#
#   nix eval --json --file ci/plugin-datapunt.nix | jq . > .github/workflows/plugin-datapunt.yml

# datapunt (teranos/datapunt) is a core with no kind of its own; a company keeps
# its kinds in one CUE file in its own repo. This builds the plugin out of the
# core at one rev and every company file at its rev, and releases it on the
# repo that called, which is the one the node's [plugin] enabled names.
let
  version = "\${{ steps.version.outputs.version }}";
  artifact = "\${{ steps.package.outputs.artifact }}";
in
{
  name = "plugin-datapunt";

  on.workflow_call = {
    inputs = {
      core_ref = {
        description = "The rev of teranos/datapunt to build";
        type = "string";
        required = true;
      };
      schemas = {
        description = "JSON list of {repo, ref, path}: one company CUE file each";
        type = "string";
        required = true;
      };
      ldc = {
        description = "The D compiler setup-dlang installs";
        type = "string";
        default = "ldc-1.41.0";
      };
      runs_on = {
        description = "Runner labels as JSON, e.g. [\"self-hosted\",\"q-box\"]";
        type = "string";
        required = false;
        default = "\"ubuntu-latest\"";
      };
    };
    secrets.schemas_token = {
      description = "Reads the core and the company repos; the caller's, never this repo's";
      required = true;
    };
  };

  jobs.build = {
    runs-on = "\${{ fromJSON(inputs.runs_on) }}";
    timeout-minutes = 30;
    permissions.contents = "write";

    steps = [
      {
        name = "Checkout the core";
        uses = "actions/checkout@v5";
        "with" = {
          repository = "teranos/datapunt";
          ref = "\${{ inputs.core_ref }}";
          path = "core";
          token = "\${{ secrets.schemas_token }}";
        };
      }

      # Each file lands under schemas/ as <repo owner>-<repo>-<basename>, so
      # two companies naming their file the same do not overwrite each other.
      {
        name = "Fetch the company files";
        id = "schemas";
        env = {
          SCHEMAS = "\${{ inputs.schemas }}";
          TOKEN = "\${{ secrets.schemas_token }}";
        };
        run = ''
          mkdir -p schemas
          FILES=""
          for row in $(printf '%s' "$SCHEMAS" | jq -r '.[] | @base64'); do
            repo=$(printf '%s' "$row" | base64 -d | jq -r .repo)
            ref=$(printf '%s' "$row" | base64 -d | jq -r .ref)
            path=$(printf '%s' "$row" | base64 -d | jq -r .path)
            out="schemas/$(printf '%s' "$repo" | tr / -)-$(basename "$path")"
            curl --fail --silent --show-error \
              -H "Authorization: Bearer $TOKEN" \
              -H "Accept: application/vnd.github.raw" \
              "https://api.github.com/repos/$repo/contents/$path?ref=$ref" -o "$out"
            FILES="$FILES $GITHUB_WORKSPACE/$out"
          done
          echo "files=$FILES" >> "$GITHUB_OUTPUT"
          ls -la schemas
        '';
      }

      # Nix brings cue to wind, from the core's pin.
      { uses = "cachix/install-nix-action@v31.11.1"; }

      {
        uses = "dlang-community/setup-dlang@v2.0.0";
        "with".compiler = "\${{ inputs.ldc }}";
      }

      {
        name = "Test";
        working-directory = "core";
        env.DATAPUNT_SCHEMAS = "\${{ steps.schemas.outputs.files }}";
        run = "dub test --config=plugin --compiler=ldc2";
      }

      {
        name = "Build plugin";
        working-directory = "core";
        env.DATAPUNT_SCHEMAS = "\${{ steps.schemas.outputs.files }}";
        run = "dub build --config=plugin --compiler=ldc2 --build=release";
      }

      # The version carries the schema digest, so a released version is not
      # released again; the tags are read so a failing gh stops here.
      {
        name = "Resolve version";
        id = "version";
        env.GH_TOKEN = "\${{ github.token }}";
        env.GH_REPO = "\${{ github.repository }}";
        working-directory = "core";
        run = ''
          VERSION=$(./bin/qntx-datapunt-plugin --version | cut -d' ' -f2)
          echo "version=$VERSION" >> "$GITHUB_OUTPUT"
          TAGS=$(gh release list --limit 1000 --json tagName --jq '.[].tagName')
          FRESH=yes
          for TAG in $TAGS; do
            if [ "$TAG" = "datapunt-v$VERSION" ]; then FRESH=no; fi
          done
          if [ "$FRESH" = no ]; then
            echo "datapunt-v$VERSION is already released — change a schema or bump PLUGIN_VERSION in the core to ship again" >&2
          fi
          echo "fresh=$FRESH" >> "$GITHUB_OUTPUT"
        '';
      }

      # Both names are the fetcher's (plugin/grpc/fetch.go): the asset ends in
      # -<GOOS>-<GOARCH>.tar.gz, and the binary inside is qntx-<name>-plugin.
      {
        name = "Package";
        id = "package";
        working-directory = "core";
        run = ''
          ARTIFACT="qntx-datapunt-plugin-${version}-linux-amd64.tar.gz"
          tar -czf "$ARTIFACT" -C bin qntx-datapunt-plugin
          sha256sum "$ARTIFACT" > "$ARTIFACT.sha256"
          echo "artifact=$ARTIFACT" >> "$GITHUB_OUTPUT"
        '';
      }

      {
        name = "Verify the artifact runs";
        working-directory = "core";
        run = ''
          VERIFY="$RUNNER_TEMP/verify-datapunt"
          rm -rf "$VERIFY" && mkdir -p "$VERIFY"
          tar -xzf "${artifact}" -C "$VERIFY"
          "$VERIFY/qntx-datapunt-plugin" --version
        '';
      }

      {
        name = "Upload artifact";
        "if" = "steps.version.outputs.fresh != 'yes'";
        uses = "actions/upload-artifact@v4";
        "with" = {
          name = artifact;
          path = ''
            core/${artifact}
            core/${artifact}.sha256
          '';
        };
      }

      {
        name = "Publish release";
        "if" = "steps.version.outputs.fresh == 'yes'";
        env.GH_TOKEN = "\${{ github.token }}";
        env.GH_REPO = "\${{ github.repository }}";
        working-directory = "core";
        run = ''
          TAG="datapunt-v${version}"
          gh release create "$TAG" \
            "${artifact}" \
            "${artifact}.sha256" \
            --target "$GITHUB_SHA" --title "$TAG" \
            --notes "teranos/datapunt at ''${{ inputs.core_ref }}, with ''${{ inputs.schemas }}"
        '';
      }
    ];
  };
}
