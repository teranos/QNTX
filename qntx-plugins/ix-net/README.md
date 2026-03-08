# ix-net

HTTPS MITM proxy for Claude Code API traffic capture.

Intercepts `api.anthropic.com` traffic, extracts model, token usage, and base64 images from request/response payloads.

## Usage

```fish
# Generate certs (once)
cd certs && fish anthropic.fish

# Start proxy
./bin/qntx-ix-net-plugin --standalone

# Launch Claude Code through proxy
fish claude.fish
```

## Research

- [Claude Code API Wire Format](../../docs/research/claude-api-wire-format.md) — request/response structure captured from live traffic
