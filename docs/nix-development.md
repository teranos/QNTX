# Nix Development Guide

**Why Nix?** Eliminates "works on my machine" by pinning every dependency - from Go version to system libraries - in a single file. Build today, rebuild in 5 years, get identical binaries.

**When to use:**
- Building Docker images for multi-arch (auto-handles arm64/amd64)
- Verifying reproducible builds before CI
- Quick environment setup without installing Go/Rust locally

**When not to use:**
- Rapid iteration (local Go build is faster)
- Network-dependent operations (Nix sandbox blocks network)

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

## How CI Builds Work

**Why reproducible builds?** Proves the binary you download matches what CI built. Rebuild the same commit later → identical SHA256 hash. No hidden changes.

**How it works:**
1. Tag a version → triggers `.github/workflows/nix-image.yml`
2. Builds amd64 + arm64 in parallel (Nix handles cross-compilation)
3. Rebuilds each architecture twice, verifies hashes match (catches non-determinism)
4. Pushes to Cachix (binary cache) so future builds are instant
5. Creates Docker multi-arch manifest (one tag works on all platforms)

**Why Cachix?** First build takes ~30 min (compiles everything). Cachix caches binaries. Next build: ~5 min (just downloads from cache).

**Caching strategy:**
- Cache key: `flake.lock` hash (nixpkgs version)
- Change `flake.lock` → rebuild everything (new cache key)
- Don't change `flake.lock` → instant downloads from Cachix

## Common Tasks

### Updating Go Dependencies

**Why vendorHash?** Nix downloads your Go modules during build. Hash proves you got what you expected (security). Wrong hash = build fails.

**How to update:**
```bash
# After changing go.mod/go.sum, run this:
./.githooks/update-nix-hash.sh

# Or manually: let it fail, copy new hash from error
nix build .#qntx  # Fails with "got: sha256-ABC..."
# Copy "got" hash to vendorHash in flake.nix

# Verify
nix build .#qntx

# Commit together
git add flake.nix go.mod go.sum
```

### Upgrading Nixpkgs

**Why upgrade?** Get newer Go/Rust versions, security patches, bug fixes in build tools.

**When to upgrade?** Monthly, or when you need a specific package version.

```bash
nix flake update nixpkgs  # Updates flake.lock
nix build .#qntx          # Test build still works
nix flake check           # Verify all packages build

git add flake.lock
git commit -m "Update nixpkgs"
```

**Warning:** This invalidates Cachix cache (new packages = rebuild everything once).

### Adding System Dependencies to Docker Image

**When needed:** CI tests require a new CLI tool (e.g., `jq` for JSON processing).

**How to add:**
1. Edit `flake.nix` → find `mkCiImage` function → add to `contents = [...]`
2. If binary needs to be in PATH, add to `config.Env` PATH list
3. Test: `nix build .#ci-image && docker load < result`

**Example:** Adding `jq`:
```nix
contents = [
  qntx
  pkgs.jq  # Add this
  # ...
];

config.Env = [
  "PATH=${pkgs.lib.makeBinPath [ qntx pkgs.jq ... ]}"  # Add to PATH
];
```

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

### "Hash mismatch for vendor derivation"

**Why:** Nix hashes your Go modules to detect tampering. You changed `go.mod` but didn't update the hash → security check fails.

**Fix:** `./.githooks/update-nix-hash.sh` or copy "got:" hash from error to `flake.nix`.

### "Network access not allowed"

**Why:** Nix sandbox blocks network to force reproducibility. Can't download during build → must declare all deps upfront.

**Fix:** Update vendorHash (Go deps) or add to `contents = [...]` (system deps).

### CI build fails, local succeeds

**Why:** You're on different nixpkgs version. CI uses `flake.lock`, you might have uncommitted `flake.lock`.

**Fix:** `git add flake.lock` and commit it. Or `nix flake update` to match CI.

## Workflow Recommendations

**Daily development:**
- Use local Go build (`make cli`) for speed
- Use local git hooks (`git config core.hooksPath .githooks`) for instant formatting
- Save Nix for final validation before push

**Before pushing:**
- Run `nix flake check` to catch Nix-specific breakage
- If you changed `go.mod`: run `./.githooks/update-nix-hash.sh`
- Commit `flake.lock` when modified (ensures CI matches your build)

**Monthly maintenance:**
- `nix flake update` to get latest packages (security patches, Go version bumps)
- Invalidates Cachix → first CI build will be slow, subsequent ones fast again

**After tagging:**
- Always watch CI pipeline (both architectures must succeed)
- Don't retag on failure - increment patch version instead (preserves history)

## Resources

- [Nix Flakes](https://nixos.wiki/wiki/Flakes)
- [nixpkgs manual](https://nixos.org/manual/nixpkgs/stable/)
- [Nix Docker Images](https://nixos.org/manual/nixpkgs/stable/#sec-pkgs-dockerTools)
- [GitHub Actions Nix](https://github.com/cachix/install-nix-action)

## Related Documentation

- `.githooks/README.md` - Local Git hooks setup
- `flake.nix` - Nix configuration source of truth
- `.github/workflows/nix-image.yml` - CI pipeline definition
