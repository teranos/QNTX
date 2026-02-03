# Nix Development

**Why Nix?** Eliminates "works on my machine" by pinning every dependency - from Go version to system libraries - in a single file. Build today, rebuild in 5 years, get identical binaries.

## Quick Start

```bash
# Enter dev shell (installs all dependencies)
nix develop

# Pre-commit hooks are automatically installed in Nix shell
# They include: nixpkgs-fmt for Nix formatting

# Build QNTX binary
nix build

# Run checks (flake validation, build verification)
nix flake check
```

## How CI Image Builds Work

1. Tag a version → triggers `.github/workflows/ci-image.yml`
2. Builds amd64 + arm64
3. Rebuilds each architecture twice, verifies hashes match (catches non-determinism)
4. Pushes to Cachix (binary cache) so future builds are instant
5. Creates Docker multi-arch manifest (one tag works on all platforms)

## Cachix

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

```bash
nix flake update nixpkgs  # Updates flake.lock
nix build .#qntx          # Test build still works
nix flake check           # Verify all packages build

git add flake.lock
git commit -m "Update nixpkgs"
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

## Resources

- [Nix Flakes](https://nixos.wiki/wiki/Flakes)
- [nixpkgs manual](https://nixos.org/manual/nixpkgs/stable/)
- [Nix Docker Images](https://nixos.org/manual/nixpkgs/stable/#sec-pkgs-dockerTools)
- [GitHub Actions Nix](https://github.com/cachix/install-nix-action)

## Related Documentation

- `.githooks/README.md` - Git hooks setup
- `flake.nix` - QNTX app image
- `.github/workflows/nix-image.yml`
- `ci/flake.nix` - CI image
- `.github/workflows/ci-image.yml`
