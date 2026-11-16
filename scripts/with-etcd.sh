#!/usr/bin/env bash
# 「[Opus 5.5] Run a command against an etcd of its own: the member in the
# directory named first (nix build .#etcd, server/parity/etcd_<version>_<rev>),
# started on loopback ports nobody else uses, in a data directory nobody else
# uses, with QNTX_ETCD_ENDPOINT naming it, and stopped when the command ends.
#
#   scripts/with-etcd.sh result-etcd/bin cargo test --package ats-etcd」
set -euo pipefail

if [ $# -lt 2 ]; then
    echo "usage: $0 <etcd bin dir> <command...>" >&2
    exit 2
fi
bin=$(cd "$1" && pwd)
shift

dir=$(mktemp -d)
pid=""
stop() {
    if [ -n "$pid" ]; then
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    rm -rf "$dir"
}
trap stop EXIT

free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
client="http://127.0.0.1:$(free_port)"
peer="http://127.0.0.1:$(free_port)"

"$bin/etcd" \
    --name with-etcd \
    --data-dir "$dir/data" \
    --listen-client-urls "$client" --advertise-client-urls "$client" \
    --listen-peer-urls "$peer" --initial-advertise-peer-urls "$peer" \
    --initial-cluster "with-etcd=$peer" \
    --log-outputs "$dir/etcd.log" >/dev/null 2>&1 &
pid=$!

for _ in $(seq 1 100); do
    if "$bin/etcdctl" --endpoints "$client" endpoint health >/dev/null 2>&1; then
        break
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
        cat "$dir/etcd.log" >&2
        exit 1
    fi
    sleep 0.1
done
"$bin/etcdctl" --endpoints "$client" endpoint health >/dev/null || {
    cat "$dir/etcd.log" >&2
    exit 1
}

export QNTX_ETCD_ENDPOINT="$client"
"$@"
