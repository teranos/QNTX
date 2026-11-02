# libACP

A C11 [Agent Client Protocol](https://agentclientprotocol.com) library, agent and client
side, with a C ABI in `include/acp.h`. Ground's `ground code` speaks ACP through it, as it
speaks MCP through [libmcp](https://github.com/teranos/libmcp). MIT, like Ground.

The client side is what a client, QNTX among them, runs an ACP agent with as a harness:
"I would like any harness to be pluggable". Each of its writers is read back by the agent
side's reader of the same method, and each of its readers reads what the agent side writes.

It is held to two schemas: v1 at
[schema-v1.26.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.26.0),
and v2 at
[schema-v2.0.0-alpha.9](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.9).

## Build and link

```sh
nix develop
make
make test
nix build .#libacp --no-link
make install PREFIX="$HOME/.local"
```

Outside Nix: C11, make, pkg-config and json-c.

## Method names

`acp_methods.h`, which `acp.h` includes, names every method as the release's own `meta.json`
does: `ACP_AGENT_SESSION_NEW` is `"session/new"`, `ACP_CLIENT_TERMINAL_KILL` is
`"terminal/kill"`. `make` generates it from `schema/v1/meta.json` with `tools/methods.c`, as the
TypeScript SDK generates its `AGENT_METHODS` and `CLIENT_METHODS` from the same file. `nix build`
checks that file against schema-v1.26.0's `meta.json` by its sha256.

## Versions

One protocol version per connection, chosen at `initialize`, as the
[migration guide](https://agentclientprotocol.com/protocol/v2/migration#supporting-v1-and-v2-side-by-side)
asks of an agent that implements v2:

- `acp_initialize_read` reads the request in the shape of the version the client
  asked for: `clientCapabilities` for v1, `capabilities` for v2.
- It selects the highest version the agent supports at or below the one requested.
  None at or below it is a `-32022` error, with `requested` and `supported` in its data.
- `acp_initialize_result` writes the result in the selected version's shape:
  `agentCapabilities` and `agentInfo` for v1, `capabilities` and `info` for v2.

The agent names the versions it supports. An agent that names only 1 answers a
v2 client with v1, which the guide allows of a v1-only agent.

## Scope

The methods and updates ported from `ground code` are listed in [SURFACE.md](SURFACE.md).
`initialize` reads and writes both version shapes. Each subsequent row states the
implemented v1 surface and its v2 differences; documenting v2 is not implementing it.
