#!/usr/bin/env bash
# Write the tag into the four version literals Cargo needs, so the crates
# publish as the release they are. docs/release.md.
#
# Cargo cannot read git, so a version in Cargo.toml is unavoidable. This makes
# it something a release writes rather than something a person edits: the tag
# stays the only source, and the literals are its output.
set -euo pipefail

tag=$(git describe --tags --match 'v*' --exact-match 2>/dev/null || true)
if [ -z "$tag" ]; then
    echo "HEAD carries no v* tag. Crates publish as a release or not at all." >&2
    exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
    echo "the tree has uncommitted changes, so the tag does not describe it" >&2
    exit 1
fi

version=${tag#v}
root=$(git rev-parse --show-toplevel)

python3 - "$root/Cargo.toml" "$version" <<'PY'
import sys

path, version = sys.argv[1], sys.argv[2]
lines = open(path).read().split("\n")

# [workspace.package] version, and the three internal crates in
# [workspace.dependencies]. Nothing else in this file carries a version of
# ours; every other one belongs to a crate we depend on.
ours = ("version = ", "ats = { package", "qntx-proto = { path", "qntx-grpc = { path")
wrote = 0
for i, line in enumerate(lines):
    stripped = line.strip()
    if not stripped.startswith(ours):
        continue
    head, sep, tail = line.partition('version = "')
    if not sep:
        continue
    _, _, rest = tail.partition('"')
    lines[i] = head + 'version = "' + version + '"' + rest
    wrote += 1

if wrote != 4:
    print("expected 4 version literals, wrote " + str(wrote), file=sys.stderr)
    sys.exit(1)

open(path, "w").write("\n".join(lines))
print(path + ": " + version)
PY

cargo update --workspace --offline >/dev/null 2>&1 || cargo update --workspace
echo "Cargo.lock: refreshed"
