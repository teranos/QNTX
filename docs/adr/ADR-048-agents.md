# ADR-048: Agents

Date: 2026-10-04
Status: Proposed

QNTX hosts agents.

"an agent is itself"

- In A2A's terms QNTX is the A2A Server: the endpoint, authentication and
  authorization are the node's. Each agent it hosts is described by an Agent
  Card and reached by its tenant (AgentInterface.tenant).
- An agent has its own DID and attests as itself: `by` on what it writes is
  the agent.
- What an agent may reach is its own lines in system, as any actor's are.
- An agent's work outlives the request that brought it, and every call it
  makes carries its own credential. The credential of whoever sent the work
  never reaches what the agent calls (ADR-038).
- A Task's messages have two roles, user and agent: the user is who sent it,
  the agent is the one doing it. Who may see the Task is the sender's
  admission; what the agent touches is the agent's.
- An agent stands in a namespace, and nothing crosses (ADR-026).
- To A2A an agent is described by its Agent Card, and its skills are what it
  can perform (ADR-039).

## The host agent

"the host agent is QNTX as ROOT would talk to it."

"It is the ROOT agent, one persistent session agent that is QNTX itself as
expressed to ROOT."

- It has a DID of its own and attests as itself, as any agent does.
- It is not one of the agents the node hosts. A ritual's performer is an agent
  of its own.

"we cant just have any ai be the ROOT agent"

- It is Claude Code, run by the node, on Opus 5.5 at low effort. Only Claude
  can be it.
- am.toml names it under `[agent.root]`: the model, the effort, and a
  reference to the Claude plan token. A node that names no model has no ROOT
  agent, and nothing stands in for the model named.
- It is one session: `--resume` continues it, and Claude Code compacts its own
  context.
- Its key is derived from the node's own, so it is the same DID wherever the
  node is rebuilt from its record, and nothing more is kept for it.
- It reaches sigils through the node's own MCP, with a token of its own. The
  token is ROOT's kind, which minting hands to nobody, and the node writes it
  down for this agent alone.
- `claude say` says something to it and answers with what it said back.
- What it is told, what it reaches for and what it answers, it attests under
  its session by its own DID, as the hook events a transcript is read from,
  where whoever spoke to it stands.

"ROOT is ROOT"

"i want to be able to do anything"

- It is privileged, and it has hands on the box as well as sigils: a package
  installed, an address reached, a plugin developed against the node it is.

"anything that can talk to the ROOT agent can say anything to it"

- Who may talk to it is the whole of what guards it.

"make sure --permission-mode is configurable in the Claude Element"

- The permission mode a turn runs in is named by whoever speaks, and the
  Claude element offers every mode Claude Code has.
- am.toml gives the mode used when none is named (`permission_mode`) and the
  tools it may use without being asked (`allow`), by Claude Code's own names.
- Claude Code refuses `bypassPermissions` to a process run as root, which is
  how the node runs on the box. Its hands there are what `allow` names.

## The loop

The loop is Claude Code's, not the node's: "accepted." The node hosts the
process. It starts it, resumes it and stops it.

## First uses

- A ritual's performer, in place of `claude --bg` on the operator's machine
  (teranos/ground RITUAL.md): the same runner, on the box.
- A2A: work sent to an agent the node hosts.

## First: ROOT talks to QNTX

As ROOT, I open the node and say something to it. It answers as the node, and
when I ask it to do something it does it itself: a sigil, or a command on the
box. Each turn is attested by the ROOT agent's own DID, and tomorrow it is the
same session, readable as a transcript.

"for development purposes SUPER will be allowed temporarily, but SUPER needs to
be removed before the work get's merged."

## Not done

For the first story, in order. Each ends in something that can be checked.

1. SUPER removed.
2. The box upgraded.

The node does not see the model traffic of an agent Claude Code runs.

"for a later phase we make sure it always runs with ground"

Ground's last release is behind its main, so Ground on the box takes a Ground
release.

"Upgrading the box is in scope"
