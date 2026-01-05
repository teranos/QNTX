# Git Hooks

This directory contains git hooks for the QNTX repository.

## Available Hooks

### post-checkout

Automatically pulls the latest changes when checking out to the `main` branch.

**What it does:**
- When you run `git checkout main`, it automatically runs `git pull` afterward
- Ensures your local main branch stays in sync with remote

### update-nix-hash.sh (utility script)

Updates Nix vendorHash when Go dependencies change.

**Usage:**
- Called automatically by Nix pre-commit hooks (via `nix develop`)
- Can also be run manually: `./.githooks/update-nix-hash.sh`
- Detects correct vendorHash by building with empty hash
- Updates `flake.nix` automatically

## Installation

**Nix-based pre-commit hooks** (recommended):
```bash
nix develop  # Automatically installs all pre-commit hooks
```

Includes: nixpkgs-fmt, gofmt, govet, and vendorHash auto-update.

**Manual installation** (for non-Nix hooks like post-checkout):
```bash
# Set git hooks directory (Git 2.9+)
git config core.hooksPath .githooks
```
