#!/usr/bin/env bash
# Automatically update Nix vendorHash when Go dependencies change
set -euo pipefail

FLAKE_FILE="flake.nix"

echo "🔍 Detecting correct Nix vendorHash..."

# Backup original file
cp "$FLAKE_FILE" "$FLAKE_FILE.bak"

# Set to empty hash to trigger Nix error with correct hash
sed -i.tmp 's/vendorHash = "sha256-[A-Za-z0-9+/=]*"/vendorHash = ""/' "$FLAKE_FILE"
rm -f "$FLAKE_FILE.tmp"

# Build and capture error containing correct hash
if ! output=$(nix build .#qntx 2>&1); then
  # Extract correct hash from error message
  # Format: "got:    sha256-xxxxx"
  correct_hash=$(echo "$output" | grep -oE 'got:\s+sha256-[A-Za-z0-9+/=]+' | sed 's/got:\s*//' | head -1)

  if [ -n "$correct_hash" ]; then
    # Restore backup and update with correct hash
    mv "$FLAKE_FILE.bak" "$FLAKE_FILE"
    sed -i.tmp "s/vendorHash = \"sha256-[A-Za-z0-9+/=]*\"/vendorHash = \"$correct_hash\"/" "$FLAKE_FILE"
    rm -f "$FLAKE_FILE.tmp"
    echo "✓ Updated vendorHash to: $correct_hash"
    exit 0
  else
    # Restore backup if we couldn't find the hash
    mv "$FLAKE_FILE.bak" "$FLAKE_FILE"
    echo "⚠️  Could not extract hash from Nix output"
    echo "$output"
    exit 1
  fi
else
  # Build succeeded with empty hash (unexpected)
  mv "$FLAKE_FILE.bak" "$FLAKE_FILE"
  echo "✓ vendorHash already correct"
  exit 0
fi
