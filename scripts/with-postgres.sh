#!/usr/bin/env bash
# Run a command against a Postgres of its own: the server in the directory
# named first (make parity's is the pinned Supabase Postgres, nix build
# .#postgres), started on a socket in a directory nobody else uses, with
# QNTX_POSTGRES_URL naming it, and stopped when the command ends.
#
#   scripts/with-postgres.sh result-postgres/bin go test ./...
set -euo pipefail

if [ $# -lt 2 ]; then
    echo "usage: $0 <postgres bin dir> <command...>" >&2
    exit 2
fi
# Absolute: initdb finds its share directory from where it was run, and the
# server it starts reads those files from its own data directory, where a
# relative path no longer resolves.
bin=$(cd "$1" && pwd)
shift

dir=$(mktemp -d)
stop() {
    "$bin/pg_ctl" -D "$dir/data" -m immediate stop >/dev/null 2>&1 || true
    rm -rf "$dir"
}
trap stop EXIT

"$bin/initdb" -U postgres --auth=trust -E UTF8 -D "$dir/data" >"$dir/initdb.log" || {
    cat "$dir/initdb.log" >&2
    exit 1
}
"$bin/pg_ctl" -D "$dir/data" -l "$dir/server.log" -w \
    -o "-k $dir -c listen_addresses=''" start >/dev/null || {
    cat "$dir/server.log" >&2
    exit 1
}

export QNTX_POSTGRES_URL="host=$dir user=postgres dbname=postgres sslmode=disable"
"$@"
