#!/usr/bin/env bash
# 「[Opus 5.5] scripts/with-etcd.sh against an etcd that loses its port the first time:
# 「[Opus 5.5] free_port lets each port go before etcd binds it, and something else can take it」
# 「[Opus 5.5] in between (CodeRabbit on #1765). The script starts again on fresh ports; an etcd」
# 「[Opus 5.5] that dies for any other reason is said, with its log, at once.」」
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: want $3, got $2"; fail=1; fi; }

fake=$(mktemp -d)
trap 'rm -rf "$fake"' EXIT

# 「[Opus 5.5] etcd: the first start says what etcd says of a port taken, and exits;
# 「[Opus 5.5] every later one stays up. Each start is counted in $fake/starts.」」
cat > "$fake/etcd" <<'EOF'
#!/usr/bin/env bash
dir=$(dirname "$0")
n=$(( $(cat "$dir/starts" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$dir/starts"
log=""
for ((i = 1; i <= $#; i++)); do
    if [ "${!i}" = "--log-outputs" ]; then j=$((i + 1)); log=${!j}; fi
done
if [ "$(cat "$dir/mode")" = "dies" ] || { [ "$(cat "$dir/mode")" = "taken-once" ] && [ "$n" = 1 ]; }; then
    if [ "$(cat "$dir/mode")" = "dies" ]; then
        echo '{"level":"fatal","msg":"the data dir is not writable"}' > "$log"
    else
        echo '{"level":"fatal","msg":"listen tcp 127.0.0.1:1234: bind: address already in use"}' > "$log"
    fi
    exit 1
fi
touch "$dir/up"
exec sleep 30
EOF
cat > "$fake/etcdctl" <<'EOF'
#!/usr/bin/env bash
[ -e "$(dirname "$0")/up" ]
EOF
chmod +x "$fake/etcd" "$fake/etcdctl"

echo taken-once > "$fake/mode"
out=$("$here/with-etcd.sh" "$fake" sh -c 'echo "endpoint=$QNTX_ETCD_ENDPOINT"' 2>&1)
check "a port taken once is started again" "$?" "0"
check "etcd was started twice" "$(cat "$fake/starts")" "2"
check "the command ran with the endpoint" "$(echo "$out" | grep -c '^endpoint=http://127.0.0.1:')" "1"

rm -f "$fake/starts" "$fake/up"
echo dies > "$fake/mode"
out=$("$here/with-etcd.sh" "$fake" true 2>&1)
check "an etcd that dies otherwise fails" "$?" "1"
check "it was started once" "$(cat "$fake/starts")" "1"
check "its log is said" "$(echo "$out" | grep -c 'the data dir is not writable')" "1"

exit $fail
