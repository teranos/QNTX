# ADR-039: SIGILS

Date: 2026-09-17
Status: Proposed

- A sigil is the one place something QNTX does is defined: what it is for, what
  goes in, what comes out and how it refuses.
- The HTTP API and MCP are surfaces of a sigil, and A2A will be one. A sigil is
  one endpoint and one tool, and every surface does the same thing and refuses
  the same way.
- A signum holds the sigils of one subject: watchers is a signum, and list,
  create, read, update and delete are its sigils. To A2A a signum is a skill.
- They live in server/sigil, and they cross a boundary as proto.
- Reach lines name sigils and signa, per surface where that matters. The const
  table is the floor, lines in system govern at runtime, and a caller is shown
  only what they reach.
- A2A is ROOT's alone for the foreseeable future, and it is for the owner's own
  agents first: an agent is a token, and what it says is written under its own
  DID. The boundary to other systems is deferred, and it will be runtime lines
  in the attestation DSL.
- Some routes are not sigils: .well-known, the /auth ceremony, / and /health,
  /g/, /s/, the sockets, /mcp.
