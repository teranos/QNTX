#!/usr/bin/env fish
# Launch Claude Code through ix-net proxy.
# Assumes proxy is already running on port 9100.

set -l IX_NET_DIR (dirname (status filename))

set -x HTTPS_PROXY "http://localhost:9100"
set -x NODE_EXTRA_CA_CERTS "$IX_NET_DIR/certs/ca.pem"

claude $argv
