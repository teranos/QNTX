# The Branch Latest workflow. .github/workflows/branch-latest.yml is emitted
# from this and is never edited by hand:
#
#   nix eval --json --file ci/branch-latest.nix | jq . > .github/workflows/branch-latest.yml
#
# JSON is YAML, so GitHub reads the emitted file as it is.
{
  name = "Branch Latest";

  # What this ships and why: docs/release.md

  # push is the normal trigger. workflow_dispatch exists because a push event
  # that GitHub records but never turns into a run leaves the branch with no way
  # to build its own tip — the only recovery was a commit with nothing in it.
  on = {
    # A docs change builds no binary.
    push = {
      branches = [ "**" ];
      paths-ignore = [ "docs/**" "**/*.md" ];
    };
    workflow_dispatch = null;
  };

  # A newer push to the same branch makes this run's binary stale before it ships.
  concurrency = {
    group = "\${{ github.workflow }}-\${{ github.ref }}";
    cancel-in-progress = true;
  };

  jobs.build = {
    # "when i do something, i want that the system understands that i am not to be scrutinised"
    # A vault edit QNTX syncs (syncbvh) changes no code: no binary is built for it.
    "if" = "!(github.event_name == 'push' && github.event.head_commit.author.name == 'sbvh-qntx[bot]' && startsWith(github.event.head_commit.message, 'syncbvh:'))";
    runs-on = "\${{ matrix.runner }}";
    permissions.contents = "write";
    strategy = {
      # Serialize the two arch builds so they don't race on
      # create-or-update of the shared branch-<name>-latest release.
      max-parallel = 1;
      matrix.include = [
        {
          arch = "amd64";
          runner = "ubuntu-latest";
        }
        {
          arch = "arm64";
          runner = "ubuntu-24.04-arm";
        }
      ];
    };

    steps = [
      {
        name = "Checkout repository";
        uses = "actions/checkout@v5";
        # 「[Opus 5.5] The tags, so git describe names the build by the one it is past.」
        "with".fetch-depth = 0;
      }

      {
        # "you need to make release be set with the tag for non dev deployments, like, q and k"
        # 「[Opus 5.5] docs/release.md」
        name = "Compute version tag";
        id = "version";
        run = ''
          echo "tag=$(git describe --tags --match 'v*' --dirty)" >> "$GITHUB_OUTPUT"
        '';
      }

      {
        name = "Compute branch name";
        id = "branch";
        run = ''
          # Sanitize the ref for use in a git tag: replace '/' with '-'.
          BRANCH_NAME="''${GITHUB_REF#refs/heads/}"
          SAFE_NAME="''${BRANCH_NAME//\//-}"
          echo "name=$SAFE_NAME" >> $GITHUB_OUTPUT
          echo "tag=branch-''${SAFE_NAME}-latest" >> $GITHUB_OUTPUT
          echo "Branch: $BRANCH_NAME  Tag: branch-''${SAFE_NAME}-latest"
        '';
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
        name = "Build QNTX binary";
        env.VERSION_TAG = "\${{ steps.version.outputs.tag }}";
        run = ''
          echo "Building QNTX $VERSION_TAG for Linux ''${{ matrix.arch }}..."
          nix build .#qntx --impure --print-build-logs
        '';
      }

      {
        name = "Cache binary and its Rust to Cachix";
        # The Rust libraries and their compiled dependencies are build-time
        # inputs of qntx, so pushing qntx alone never carried them: every node
        # building qntx compiled every crate again.
        env.VERSION_TAG = "\${{ steps.version.outputs.tag }}";
        run = "nix build .#qntx .#ats-ffi-deps .#ats-ffi .#ats-wasm-deps .#ats-wasm --impure --print-out-paths --no-link | cachix push qntx";
      }

      {
        name = "Package binary with bundled libduckdb";
        # 「[Opus 5.5] docs/release.md」
        run = ''
          mkdir -p release/lib
          cp result/bin/qntx release/qntx
          chmod +w release/qntx
          DUCKDB_SO=$(ldd result/bin/qntx | awk '/libduckdb/ {print $3}')
          if [ -z "$DUCKDB_SO" ]; then
            echo "libduckdb not found in qntx dependencies"
            ldd result/bin/qntx
            exit 1
          fi
          # 「[Opus 5.5] The libduckdb nix builds needs the glibc of the nixpkgs it came from; a node」
          # 「[Opus 5.5] runs on its distribution's. DuckDB's own build of the same version runs on both.」
          DUCKDB_VERSION=$(echo "$DUCKDB_SO" | grep -oE 'duckdb-[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d- -f2)
          if [ -z "$DUCKDB_VERSION" ]; then
            echo "no DuckDB version in $DUCKDB_SO"
            exit 1
          fi
          # 「[Opus 5.5] DuckDB's own host first: GitHub's release asset for arm64 answered 504 on every ask on 2026-11-08.」
          curl -fsSL -o libduckdb.zip "https://install.duckdb.org/v''${DUCKDB_VERSION}/libduckdb-linux-''${{ matrix.arch }}.zip" \
            || curl -fsSL -o libduckdb.zip "https://github.com/duckdb/duckdb/releases/download/v''${DUCKDB_VERSION}/libduckdb-linux-''${{ matrix.arch }}.zip"
          # 「[Opus 5.5] Each archive is held to the hash DuckDB's two hosts agreed on (2026-11-11); a version pinned nowhere here is refused.」
          case "$DUCKDB_VERSION-''${{ matrix.arch }}" in
            1.5.5-amd64) WANT=1fb8ce388157d84a25abe685a8a2520bf00c00321821968e4bb398fd766e7abb ;;
            1.5.5-arm64) WANT=abe4f6f005ee0b448a058322f4263584b4bd1b6faf7ab4637b79eeaf978f8e9c ;;
            *) echo "no hash pinned for libduckdb $DUCKDB_VERSION ''${{ matrix.arch }}"; exit 1 ;;
          esac
          echo "$WANT  libduckdb.zip" | sha256sum -c -
          unzip -o libduckdb.zip libduckdb.so -d release/lib/
          # 「[Opus 5.5] Debian 13's glibc, which the node runs on, is the newest a release may need.」
          NEEDS=$(objdump -T release/lib/libduckdb.so | grep -oE 'GLIBC_2\.[0-9]+' | sort -t. -k2 -n -u | tail -1)
          if [ "''${NEEDS#GLIBC_2.}" -gt 41 ]; then
            echo "libduckdb $DUCKDB_VERSION needs $NEEDS, past the node's GLIBC_2.41"
            exit 1
          fi
          echo "libduckdb $DUCKDB_VERSION, needing $NEEDS"
          if [ "''${{ matrix.arch }}" = "arm64" ]; then
            INTERP=/lib/ld-linux-aarch64.so.1
          else
            INTERP=/lib64/ld-linux-x86-64.so.2
          fi
          nix run nixpkgs#patchelf -- \
            --set-interpreter "$INTERP" \
            --set-rpath '$ORIGIN/lib' \
            release/qntx
          chmod +x release/qntx
          cd release
          tar -czf qntx-''${{ steps.branch.outputs.name }}-linux-''${{ matrix.arch }}.tar.gz qntx lib/
          sha256sum qntx-''${{ steps.branch.outputs.name }}-linux-''${{ matrix.arch }}.tar.gz \
            > qntx-''${{ steps.branch.outputs.name }}-linux-''${{ matrix.arch }}.tar.gz.sha256
        '';
      }

      # First matrix job (amd64) deletes any prior rolling release so we start
      # fresh at this commit. Second job (arm64) skips deletion (release now
      # exists from step below) and just uploads its asset via gh release upload.
      {
        name = "Reset rolling release (first arch only)";
        "if" = "matrix.arch == 'amd64'";
        env.GH_TOKEN = "\${{ secrets.GITHUB_TOKEN }}";
        run = ''
          gh release delete ''${{ steps.branch.outputs.tag }} \
            --cleanup-tag --yes 2>/dev/null || true
          gh release create ''${{ steps.branch.outputs.tag }} \
            --prerelease \
            --title "Branch ''${{ steps.branch.outputs.name }} (rolling)" \
            --target ''${{ github.sha }} \
            --notes "Rolling pre-release for the tip of \`''${{ steps.branch.outputs.name }}\`.

          **URL scheme (stable across pushes):**
          \`\`\`
          https://github.com/teranos/QNTX/releases/download/''${{ steps.branch.outputs.tag }}/qntx-''${{ steps.branch.outputs.name }}-linux-<arch>.tar.gz
          \`\`\`

          **Commit:** \`''${{ github.sha }}\`

          Not a stable release — for deployment tracking only. Use a \`v*\` tag for production."
        '';
      }

      {
        name = "Upload arch asset to rolling release";
        env.GH_TOKEN = "\${{ secrets.GITHUB_TOKEN }}";
        run = ''
          gh release upload ''${{ steps.branch.outputs.tag }} \
            release/qntx-''${{ steps.branch.outputs.name }}-linux-''${{ matrix.arch }}.tar.gz \
            release/qntx-''${{ steps.branch.outputs.name }}-linux-''${{ matrix.arch }}.tar.gz.sha256 \
            --clobber
        '';
      }
    ];
  };
}
