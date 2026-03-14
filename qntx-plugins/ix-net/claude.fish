#!/usr/bin/env fish
# Launch Claude Code through ix-net proxy.
# Reads proxy_port from am.toml in the QNTX root (two levels up from this script).

set -l IX_NET_DIR (dirname (status filename))
set -l QNTX_ROOT "$IX_NET_DIR/../.."
set -l AM_TOML "$QNTX_ROOT/am.toml"

# Read proxy_port from [ix-net] section, default 9100
set -l PROXY_PORT 9100
if test -f "$AM_TOML"
    set -l in_section 0
    for line in (cat "$AM_TOML")
        if string match -q '[ix-net]' -- $line
            set in_section 1
        else if string match -q '[*' -- $line
            set in_section 0
        else if test $in_section -eq 1
            set -l val (string match -r 'proxy_port\s*=\s*(\d+)' -- $line)
            if test (count $val) -gt 1
                set PROXY_PORT $val[2]
            end
        end
    end
end

set -x HTTPS_PROXY "http://localhost:$PROXY_PORT"
set -x NODE_EXTRA_CA_CERTS "$IX_NET_DIR/certs/ca.pem"

claude $argv
