# Nix Development Guide

This guide explains QNTX's Nix-based build system, development workflow, and CI pipeline.

## Overview

QNTX uses [Nix flakes](https://nixos.wiki/wiki/Flakes) to provide:

- **Reproducible builds**: Bit-for-bit identical binaries across machines
- **Multi-architecture support**: Native amd64 and arm64 container images
- **Declarative dependencies**: All build tools versioned in `flake.nix`
- **Development environment**: `nix develop` provides consistent tooling

## Quick Start

### Prerequisites

- Nix with flakes enabled (install from https://nixos.org/download.html)
- Docker (for container image operations)
- Git

### Development Workflow

```bash
# Enter development shell (installs all dependencies)
nix develop

# Pre-commit hooks are automatically installed in Nix shell
# They include: nixpkgs-fmt for Nix formatting

# Build QNTX binary
nix build

# Build CI container image (defaults to your architecture)
nix build .#ci-image

# Build specific architecture
nix build .#ci-image-amd64
nix build .#ci-image-arm64

# Run checks (flake validation, build verification)
nix flake check
```

### Local Git Hooks

For local development outside the Nix shell, install local Git hooks:

```bash
git config core.hooksPath .githooks
```

This enables:
- **pre-commit**: Automatic Go code formatting with `gofmt`
- **post-checkout**: Auto-pull when checking out main branch

See `.githooks/README.md` for details.

## Architecture

### Flake Structure

```
flake.nix
├── inputs
│   ├── nixpkgs (nixos-unstable for Go 1.24+)
│   ├── flake-utils (multi-platform support)
│   └── pre-commit-hooks (Nix-based code formatting)
├── packages
│   ├── qntx (CLI binary)
│   ├── qntx-code (plugin binary)
│   ├── ci-image (auto-detected architecture)
│   ├── ci-image-amd64
│   ├── ci-image-arm64
│   ├── qntx-code-plugin-image (auto-detected architecture)
│   ├── qntx-code-plugin-image-amd64
│   └── qntx-code-plugin-image-arm64
├── devShells
│   └── default (development environment)
└── checks
    ├── pre-commit (Nix formatting)
    ├── qntx-build (verify QNTX CLI compiles)
    ├── qntx-code-build (verify plugin compiles)
    ├── ci-image (verify CI image builds)
    └── qntx-code-plugin-image (verify plugin image builds)
```

### Container Image Contents

The CI image (`ci-image-*`) includes:

**Core Tools:**
- QNTX binary (pre-built)
- Go toolchain (1.24+)
- Git

**Rust Toolchain:**
- rustc, cargo, rustfmt, clippy

**System Dependencies:**
- openssl (for HTTPS)
- patchelf (for binary patching)

**Build Tools:**
- pkg-config, gcc, make, sqlite
- SSL certificates for HTTPS

## CI Pipeline

### Build Process

The `.github/workflows/nix-image.yml` workflow:

1. **Trigger**: On version tags (`v*`) or manual dispatch
2. **Matrix Build**: Parallel builds for amd64 and arm64
3. **Steps per architecture:**
   - Checkout repository
   - Cache Nix store (speeds up builds)
   - Validate flake with `nix flake check`
   - Build image: `nix build .#ci-image-{arch}`
   - **Reproducibility check**: Build twice, verify identical hashes
   - Tag and push: `ghcr.io/teranos/qntx:{version}-{arch}`
4. **Manifest Creation**: Combine amd64/arm64 into multi-arch manifest
   - Push: `ghcr.io/teranos/qntx:latest` (multi-arch)
   - Push: `ghcr.io/teranos/qntx:{version}` (multi-arch)

### Caching Strategy

- **Nix store cache**: Persists `/nix/store` between runs
- **Cache key**: Based on `flake.lock` hash
- **Restore strategy**: Falls back to latest cache for same OS

This typically reduces build time from ~30 minutes to ~5 minutes.

### Reproducible Builds

Every CI run verifies reproducibility:

```bash
nix build .#ci-image-amd64 --rebuild --out-link result-1
nix build .#ci-image-amd64 --rebuild --out-link result-2

# Compare SHA256 hashes - must be identical
nix-hash --type sha256 result-1
nix-hash --type sha256 result-2
```

This ensures:
- No timestamp/hostname embedding in binaries
- Consistent builds across machines
- Supply chain integrity

## Common Tasks

### Updating Go Dependencies

When you modify `go.mod` or `go.sum`:

1. **Update vendorHash in flake.nix:**

   ```bash
   # Run the utility script
   ./.githooks/update-nix-hash.sh

   # Or manually: trigger a build error and extract the hash
   nix build .#qntx  # Will fail with correct hash
   # Copy hash from error message to flake.nix
   ```

2. **Verify the build:**

   ```bash
   nix build .#qntx
   ```

3. **Commit both `flake.nix` and `go.mod`/`go.sum`**

### Upgrading Nixpkgs

To update to the latest nixpkgs-unstable:

```bash
nix flake update nixpkgs

# Test the update
nix build .#qntx
nix flake check

# Commit flake.lock
git add flake.lock
git commit -m "Update nixpkgs"
```

### Adding New Dependencies

**Go dependencies:**
- Add to `go.mod` normally
- Update vendorHash (see above)

**System dependencies (for CI image):**

Edit `flake.nix` in the `mkCiImage` function:

```nix
contents = [
  qntx
  pkgs.go
  pkgs.newTool  # Add here
  # ...
];
```

Update `PATH` or `PKG_CONFIG_PATH` in the `config.Env` section if needed.

## Nix vs Local Development

### Nix Shell

**Pros:**
- Consistent versions across team
- Automatic pre-commit hooks (nixpkgs-fmt)
- Reproducible environment

**Cons:**
- Sandbox restrictions (no network access for some hooks)
- Slower first-time setup

**When to use:**
- CI validation before pushing
- Ensuring reproducible builds
- Nix formatting checks

### Local Git Hooks

**Pros:**
- Fast (no sandbox)
- Network access (Go module downloads)
- Instant feedback

**Cons:**
- Requires local Go installation
- Not enforced automatically

**When to use:**
- Day-to-day development
- Quick formatting before commits
- When working offline with cached modules

**Setup:**

```bash
git config core.hooksPath .githooks
```

## Troubleshooting

### "Hash mismatch" for vendorHash

**Cause:** Go dependencies changed but vendorHash wasn't updated.

**Fix:**

```bash
./.githooks/update-nix-hash.sh
```

Or manually: let the build fail and copy the correct hash from the error.

### "gofmt not found" in pre-commit hook

**Cause:** Go toolchain not installed locally.

**Fix:**

Either:
- Install Go: https://go.dev/doc/install
- Or disable local hooks: `git config --unset core.hooksPath`
- Or use Nix shell: `nix develop` (hooks run automatically)

### CI build fails but local build succeeds

**Cause:** Likely using different nixpkgs versions or missing vendorHash update.

**Fix:**

```bash
# Ensure flake.lock is committed
nix flake update

# Verify builds in clean environment
nix build --rebuild .#ci-image-amd64

# Check reproducibility
nix-build --check --keep-failed
```

### "Network access not allowed" in Nix build

**Cause:** Nix builds run in a sandbox without network access.

**Context:** This is intentional for reproducibility. All dependencies must be declared in `flake.nix`.

**Fix:**
- For Go dependencies: Update vendorHash to include new modules
- For system dependencies: Add to `contents = [ ... ]` in `mkCiImage`

### Multi-arch manifest creation fails

**Cause:** Architecture-specific images weren't pushed successfully.

**Fix:**

```bash
# Verify both arch images exist
docker manifest inspect ghcr.io/teranos/qntx:latest-amd64
docker manifest inspect ghcr.io/teranos/qntx:latest-arm64

# Recreate manifest
docker manifest create ghcr.io/teranos/qntx:latest \
  ghcr.io/teranos/qntx:latest-amd64 \
  ghcr.io/teranos/qntx:latest-arm64

docker manifest push ghcr.io/teranos/qntx:latest
```

## Best Practices

### Development Workflow

1. **Use local Git hooks for rapid iteration:**
   ```bash
   git config core.hooksPath .githooks
   ```

2. **Validate with Nix before pushing:**
   ```bash
   nix flake check  # Fast validation
   nix build        # Full build test
   ```

3. **Update vendorHash immediately after changing Go deps:**
   ```bash
   ./.githooks/update-nix-hash.sh
   git add flake.nix go.mod go.sum
   ```

### CI/CD

1. **Always check CI after tagging:**
   - Monitor build pipeline for both architectures
   - Verify manifest creation completes
   - Test pulling the multi-arch image

2. **Use semver tags:**
   ```bash
   git tag v0.16.14
   git push origin v0.16.14
   ```

3. **Leverage caching:**
   - Flake.lock changes invalidate cache
   - Keep flake.lock updates in separate commits when possible

### Maintenance

1. **Update nixpkgs monthly:**
   ```bash
   nix flake update
   nix build .#qntx  # Test
   git add flake.lock
   git commit -m "Update nixpkgs"
   ```

2. **Verify reproducibility regularly:**
   - CI does this automatically
   - Locally: `nix-build --check`

3. **Document dependency changes:**
   - Explain why new system packages were added
   - Link to upstream requirements (e.g., Tauri docs)

## Resources

- [Nix Flakes](https://nixos.wiki/wiki/Flakes)
- [nixpkgs manual](https://nixos.org/manual/nixpkgs/stable/)
- [Nix Docker Images](https://nixos.org/manual/nixpkgs/stable/#sec-pkgs-dockerTools)
- [GitHub Actions Nix](https://github.com/cachix/install-nix-action)

## Related Documentation

- `.githooks/README.md` - Local Git hooks setup
- `flake.nix` - Nix configuration source of truth
- `.github/workflows/nix-image.yml` - CI pipeline definition
