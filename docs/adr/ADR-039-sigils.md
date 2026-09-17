# ADR-039: SIGILS

Date: 2026-09-17
Status: Proposed

- A sigil is the one place something QNTX does is defined. It is an HTTP API
  endpoint and an MCP tool, one tool per sigil.
- A signum holds sigils: watchers is a signum, and list, create, read, update
  and delete are its sigils.
- They live in server/sigil. The reach table names sigils.
- Some routes are not sigils: .well-known, the /auth ceremony, / and /health,
  /g/, /s/, the sockets, /mcp.
