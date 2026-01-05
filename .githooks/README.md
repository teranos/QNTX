# Git Hooks

This directory contains git hooks for the QNTX repository.

## Available Hooks

### pre-commit

Runs Go formatting checks on staged files.

**What it does:**
- Automatically runs `gofmt` on all staged Go files
- Formats files and re-stages them if needed
- Works locally outside Nix sandbox (has network access for Go modules)
- Ensures consistent Go code formatting before commits

### post-checkout

Automatically pulls the latest changes when checking out to the `main` branch.

**What it does:**
- When you run `git checkout main`, it automatically runs `git pull` afterward
- Ensures your local main branch stays in sync with remote

### update-nix-hash.sh (utility script)

Updates Nix vendorHash when Go dependencies change.

**Usage:**
- Can be run manually: `./.githooks/update-nix-hash.sh`
- Detects correct vendorHash by building with empty hash
- Updates `flake.nix` automatically

## Installation

**Local Git hooks** (recommended for local development):
```bash
# Set git hooks directory (Git 2.9+)
git config core.hooksPath .githooks
```

This enables:
- `pre-commit`: Go formatting checks
- `post-checkout`: Auto-pull on main branch

**Nix-based pre-commit hooks** (CI environment):
```bash
nix develop  # Automatically installs pre-commit hooks in Nix shell
```

Note: Nix hooks run in a sandbox without network access. The local Git hooks (above) are recommended for development as they support Go module downloads.
