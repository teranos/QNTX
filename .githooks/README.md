# Git Hooks

This directory contains git hooks for the QNTX repository.

## Available Hooks

### post-checkout

Automatically pulls the latest changes when checking out to the `main` branch.

**What it does:**
- When you run `git checkout main`, it automatically runs `git pull` afterward
- Ensures your local main branch stays in sync with remote

### pre-commit

Automatically updates Nix vendorHash when Go dependencies change.

**What it does:**
- Detects when `go.mod` or `go.sum` are being committed
- Runs `update-nix-hash.sh` to calculate correct vendorHash
- Updates `flake.nix` and stages it automatically
- Prevents CI failures from outdated vendorHash

## Installation

To install the hooks, run from the repository root:

```bash
# Install post-checkout hook
cp .githooks/post-checkout .git/hooks/post-checkout
chmod +x .git/hooks/post-checkout
```

Or install all hooks at once:

```bash
# Set git hooks directory (Git 2.9+)
git config core.hooksPath .githooks
```

The second method is recommended as it automatically uses all hooks in `.githooks/` without manual copying.
