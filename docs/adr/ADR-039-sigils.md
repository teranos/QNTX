# ADR-039: SIGILS

Date: 2026-09-17
Status: Proposed

- A sigil is the one place something QNTX does is defined: what it is for, what
  goes in, what comes out and how it refuses.
- The HTTP API and MCP are surfaces of a sigil. Both do the same thing.
- A2A describes the node to other agents.
- The A2A card says what the node is and how to reach it. MCP is one of those
  ways.
- An a2a:<signum> line decides which skills a caller sees. am card shows the
  card a caller would get.
- The public card at /.well-known/agent-card.json is read by anyone. It shows
  the signa the node names for it, each requiring the bearer token, and is
  refused whole while it lacks a field the spec requires.
- A signum holds the sigils of one subject: watchers is a signum, and list,
  create, read, update and delete are its sigils. To A2A a signum is a skill.
- Their shape is proto and nothing else. What is not a shape, the function that
  answers, lives in server/sigil.
- Reach lines name sigils and signa, per surface where that matters. The const
  table is the floor, lines in system govern at runtime, and a caller is shown
  only what they reach.
- A2A is ROOT's alone for the foreseeable future, and it is for the owner's own
  agents first: an agent is a token, and what it says is written under its own
  DID. The boundary to other systems is deferred, and it will be runtime lines
  in the attestation DSL.
- Some routes are not sigils: .well-known, the /auth ceremony, / and /health,
  /g/, /s/, the sockets, /mcp.

## The node extension, v1

- One AgentExtension on the card carries what AgentCard has no field for.
- params.did is the node's DID, as /.well-known/did.json serves it.
- params.health is ok, degraded or down, as /health says it.
- params.syscap is what am syscap answers.
- params.mcp is where the node's MCP answers, and the MCP version it speaks. An AgentInterface is a way to speak A2A, so the MCP is said here.
- It is not required: an agent that does not read it loses nothing else.
