#!/usr/bin/env bash
# Refuse a Tauri build when the Rust `tauri` crate and NPM `@tauri-apps/api`
# disagree on major.minor. Tauri CLI errors with "Found version mismatched
# Tauri packages" and only after the ~15-minute Cargo build wastes CI time;
# this preflight catches it in one second at the start of the job.
#
# No regex — grep -F for fixed strings, awk with string ops (== and index()).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Rust: Cargo.lock has one [[package]] entry whose name line is exactly
# `name = "tauri"`. Print the line after it (the `version = "..."` line),
# then split on the double-quote character to get the version string.
RUST_VER_LINE=$(awk '$0 == "name = \"tauri\"" { getline; print; exit }' \
  "$REPO_ROOT/Cargo.lock")
RUST_VER=$(echo "$RUST_VER_LINE" | awk -F '"' '{print $2}')

# NPM: package.json is authoritative for the version range the resolver
# will honour; the lock's exact resolution follows from that range.
NPM_LINE=$(awk 'index($0, "@tauri-apps/api") { print; exit }' \
  "$REPO_ROOT/web/package.json")
NPM_RANGE=$(echo "$NPM_LINE" | awk -F '"' '{print $4}')
# Strip semver range prefix (`^` or `~`) — one at most, no regex.
NPM_VER="${NPM_RANGE#^}"
NPM_VER="${NPM_VER#~}"

rust_mm=$(echo "$RUST_VER" | awk -F '.' '{print $1"."$2}')
npm_mm=$(echo "$NPM_VER" | awk -F '.' '{print $1"."$2}')

echo "Rust tauri crate:    $RUST_VER   (major.minor $rust_mm)"
echo "NPM @tauri-apps/api: $NPM_RANGE   (major.minor $npm_mm)"

if [ "$rust_mm" != "$npm_mm" ]; then
  echo
  echo "ERROR: Tauri major.minor drift."
  echo "  web/src-tauri/Cargo.toml  → resolves to tauri $RUST_VER"
  echo "  web/package.json          → @tauri-apps/api $NPM_RANGE"
  echo
  echo "Fix: bump whichever side is behind to the same major.minor,"
  echo "regenerate lockfiles, and try again."
  exit 1
fi

echo
echo "OK: Tauri versions aligned on $rust_mm.x"
