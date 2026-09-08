# Release

The app is [QNTX-App](https://github.com/teranos/QNTX-App), and how it is
released is written there.

Unix-like hosts only — see [ADR-029](adr/ADR-029-windows.md).

## What ships
- Docker images: `ghcr.io/teranos/qntx:latest` and `ghcr.io/teranos/qntx:{version}`
- Linux CLI tarballs on tagged releases: `qntx-{version}-linux-{amd64,arm64}.tar.gz` (built natively per arch on amd64 + `ubuntu-24.04-arm` runners)
- **Rolling per-branch pre-releases:** on any push to any branch, `.github/workflows/branch-latest.yml` publishes/updates a pre-release tagged `branch-<name>-latest` with Linux amd64/arm64 tarballs at a stable URL:
  ```
  https://github.com/teranos/QNTX/releases/download/branch-<name>-latest/qntx-<name>-linux-<arch>.tar.gz
  ```
  For deployments that want the tip of a branch (e.g. a staging host tracking `main`) without waiting for a tag.
- macOS CLI tarballs on tagged releases: `qntx-{version}-darwin-{amd64,arm64}.tar.gz`, built on `macos-13` and `macos-14` runners. QNTX-App consumes these.
- The web bundle, built by `.github/workflows/deploy-web.yml` and called by whoever is deploying it.

Branch and tag tarballs have the same shape. Linux only, because Linux is what gets deployed.

Every tarball bundles Nix's `libduckdb.so` in `lib/` beside the binary, with the RPATH rewritten to `$ORIGIN/lib`, so it runs on any Linux host with no `LD_LIBRARY_PATH` and no wrapper.

## Version

The version is the tag. `git describe --tags --match 'v*' --dirty` is asked once, in the Makefile, and reaches Nix as `VERSION_TAG` — which needs `nix build --impure`, since pure eval cannot read the environment. Without it a binary calls itself `dev`.

