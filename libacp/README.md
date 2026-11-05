# 「[Sonnet 5.5] libACP」

「[Opus 5.5] A C11 [Agent Client Protocol](https://agentclientprotocol.com) library, agent and client」
「[Opus 5.5] side, with a C ABI in `include/acp.h`. Ground's `ground code` speaks ACP through it, as it」
「[Opus 5.5] speaks MCP through [libmcp](https://github.com/teranos/libmcp). MIT, like Ground.」

「[Opus 5.5] The client side is what a client, QNTX among them, runs an ACP agent with as a harness:」
「[Opus 5.5] "I would like any harness to be pluggable". Each of its writers is read back by the agent」
「[Opus 5.5] side's reader of the same method, and each of its readers reads what the agent side writes.」

「[Opus 5.5] It is held to two schemas: v1 at」
「[Opus 5.5] [schema-v1.26.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.26.0),」
「[Opus 5.5] and v2 at」
「[Opus 5.5] [schema-v2.0.0-alpha.9](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.9).」

## 「[Opus 5.5] Build and link」

```sh
nix develop
make
make test
nix build .#libacp --no-link
make install PREFIX="$HOME/.local"
```

「[Opus 5.5] Outside Nix: C11, make, pkg-config and json-c.」

## 「[Opus 5.5] Method names」

「[Opus 5.5] `acp_methods.h`, which `acp.h` includes, names every method as the release's own `meta.json`」
「[Opus 5.5] does: `ACP_AGENT_SESSION_NEW` is `"session/new"`, `ACP_CLIENT_TERMINAL_KILL` is」
「[Opus 5.5] `"terminal/kill"`. `make` generates it from `schema/v1/meta.json` with `tools/methods.c`, as the」
「[Opus 5.5] TypeScript SDK generates its `AGENT_METHODS` and `CLIENT_METHODS` from the same file. `nix build`」
「[Opus 5.5] checks that file against schema-v1.26.0's `meta.json` by its sha256.」

## 「[Opus 5.5] Versions」

「[Opus 5.5] One protocol version per connection, chosen at `initialize`, as the」
「[Opus 5.5] [migration guide](https://agentclientprotocol.com/protocol/v2/migration#supporting-v1-and-v2-side-by-side)」
「[Opus 5.5] asks of an agent that implements v2:」

- 「[Opus 5.5] `acp_initialize_read` reads the request in the shape of the version the client」
  「[Opus 5.5] asked for: `clientCapabilities` for v1, `capabilities` for v2.」
- 「[Opus 5.5] It selects the highest version the agent supports at or below the one requested.」
  「[Opus 5.5] None at or below it is a `-32022` error, with `requested` and `supported` in its data.」
- 「[Opus 5.5] `acp_initialize_result` writes the result in the selected version's shape:」
  「[Opus 5.5] `agentCapabilities` and `agentInfo` for v1, `capabilities` and `info` for v2.」

「[Opus 5.5] The agent names the versions it supports. An agent that names only 1 answers a」
「[Opus 5.5] v2 client with v1, which the guide allows of a v1-only agent.」

## 「[Opus 5.5] Scope」

「[Opus 5.5] The methods and updates ported from `ground code` are listed in [SURFACE.md](SURFACE.md).」
「[Opus 5.5] `initialize` reads and writes both version shapes. Each subsequent row states the」
「[Opus 5.5] implemented v1 surface and its v2 differences; documenting v2 is not implementing it.」
