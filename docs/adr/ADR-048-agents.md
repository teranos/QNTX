# ADR-048: Agents

Date: 2026-10-04
Status: Proposed

QNTX hosts agents.

"an agent is itself"

- In A2A's terms QNTX is the A2A Server: the endpoint, authentication and
  authorization are the node's. Each agent it hosts is a card, reached by its
  tenant (AgentInterface.tenant).
- An agent has its own DID and attests as itself: `by` on what it writes is
  the agent.
- What an agent may reach is its own lines in system, as any actor's are.
- An agent's work outlives the request that brought it, and every call it
  makes carries its own credential. The credential of whoever sent the work
  never reaches what the agent calls (ADR-038).
- A Task has two actors: who sent it, and the agent doing it. Who may see the
  Task is the sender's admission; what the agent touches is the agent's.
- An agent stands in a namespace, and nothing crosses (ADR-026).
- To A2A an agent is a card, and its skills are the signa it reaches
  (ADR-039).

## The host agent

"the host agent is QNTX as ROOT would talk to it."

"It is the ROOT agent, one persistent session agent that is QNTX itself as
expressed to ROOT."

- It has a DID of its own and attests as itself, as any agent does.
- It is not one of the agents the node hosts. A ritual's performer is an agent
  of its own.

"we cant just have any ai be the ROOT agent"

- It is Claude Code, installed on the box and run by the node, on Opus 5.5 at
  low effort. Only Claude can be it.
- It is one session: `--resume` continues it, and Claude Code compacts its own
  context.
- It reaches sigils through the node's own MCP, with a token of its own.
- It runs with Ground.

"ROOT is ROOT"

"i want to be able to do anything"

- It is privileged, and it has hands on the box as well as sigils: a package
  installed, an address reached, a plugin developed against the node it is.

"anything that can talk to the ROOT agent can say anything to it"

- Who may talk to it is the whole of what guards it.

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

1. Claude Code on the node, answering on Opus 5.5 at low effort with the token
   the box holds in SSM. Which Claude Code is pinned in parity, and the node
   fetches that binary itself: none is carried in a QNTX release.
2. The ROOT agent's own DID, and its token for the node's MCP.
3. The node starts, resumes and stops the ROOT agent's one session.
4. A way to say something to it, reached by ROOT, and by SUPER until the work
   merges. In the UI it is the Claude element.
5. Ground on the box beside it, so its session reads as a transcript. Ground's
   last release is behind its main, so this takes a Ground release.
6. SUPER removed.
7. The box upgraded.

The node does not see the model traffic of an agent Claude Code runs.

"for a later phase we make sure it always runs with ground"

"Upgrading the box is in scope"
