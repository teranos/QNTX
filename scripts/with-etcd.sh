#!/usr/bin/env bash
# 「[Opus 5.5] Run a command against an etcd of its own: the member in the
# 「[Opus 5.5] directory named first (nix build .#etcd, server/parity/etcd_<version>_<rev>),」
# 「[Opus 5.5] started on loopback ports nobody else uses, in a data directory nobody else」
# 「[Opus 5.5] uses, with QNTX_ETCD_ENDPOINT naming it, and stopped when the command ends.」
#
#   「[Opus 5.5] scripts/with-etcd.sh result-etcd/bin cargo test --package ats-etcd」」
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

# 「[Opus 5.5] free_port lets each port go before etcd binds it, so something else can
# take one in between (CodeRabbit on #1765). An etcd that lost a port is started
# again on fresh ones, up to five times; one that died otherwise is said, with its
# log, at once.」
started=""
for attempt in 1 2 3 4 5; do
    client="http://127.0.0.1:$(free_port)"
    peer="http://127.0.0.1:$(free_port)"
    rm -rf "$dir/data"
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
            started=yes
            break
        fi
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
        sleep 0.1
    done
    if [ -n "$started" ]; then
        break
    fi
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    pid=""
    if ! grep -q "address already in use" "$dir/etcd.log" 2>/dev/null; then
        cat "$dir/etcd.log" >&2
        exit 1
    fi
    echo "with-etcd: attempt $attempt lost a port before etcd bound it; starting again on fresh ones" >&2
done
if [ -z "$started" ]; then
    cat "$dir/etcd.log" >&2
    exit 1
fi

export QNTX_ETCD_ENDPOINT="$client"
"$@"
