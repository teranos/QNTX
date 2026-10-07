# ADR-048: Agents

Date: 2026-10-04
Status: Proposed

QNTX hosts agents for use by ROOT.

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
- ROOT's agents are ROOT, and no namespace holds them: their sessions are the
  node's own record, in system, whoever spoke to them and wherever they stand.
- To A2A an agent is described by its Agent Card, and its skills are what it
  can perform (ADR-039).

## An agent per model

"I feel like Opus and Fable are two distinct identities"

"each model would be its own agent session, independent of each other but still
reachable by each other."

- An agent is one model in one place: its own DID, its own session, its own
  context. Two models are two agents, and so is one model in two places.
- They reach each other, each an A2A Server to the others and their A2A
  Client.

"So the first persistent Agent swarm is really three, or four if we include
something we run on Pi"

- ROOT's are Opus, Fable and Sonnet in Claude Code, and the one in Pi.

"So a conversation I have with Mistral as ROOT is one long Mistral conversation"

- Each of ROOT's agents is one session with ROOT, and it continues.

## The host agent

"the ROOT host agent is QNTX as ROOT would talk to it."

"It is the ROOT agent, one persistent session agent that is QNTX itself as
expressed to ROOT."

- It has a DID of its own and attests as itself, as any agent does.
- It is not one of the agents the node hosts. A ritual's performer is an agent
  of its own.

"we cant just have any ai be the ROOT agent"

- In Claude Code it is Claude, run by the node: Opus, Fable and Sonnet, each a
  ROOT agent of its own. Only Claude runs there.
- am.toml names each under `[agent.root]`: its model, its effort, and a
  reference to the Claude plan token. A node that names no model has no ROOT
  agent, and nothing stands in for the model named.
- It is one session: `--resume` continues it, and Claude Code compacts its own
  context.
- Its key is derived from the node's own, for its model, so it is the same DID
  wherever the node is rebuilt from its record, and nothing more is kept for
  it.
- It reaches sigils through the node's own MCP, with a token of its own. The
  token is ROOT's kind, which minting hands to nobody, and the node writes it
  down for this agent alone.
- `claude say` says something to it and answers with what it said back.
- What it is told, what it reaches for and what it answers, it attests under
  its session by its own DID, as the hook events a transcript is read from.

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

## Its harnesses

"IS BECAUSE I WANT TO EXPECT THAT IT WILL HAPPEN THAT I RUN OUT"

"AND HAVE A FALLBACK"

"NOT BE ENTIRELY DEPENDENT ON ONE HARNESS"

"IF PI DOESNT WORK, WE PAY MONEY FOR BEDROCK"

"IT MEANS WE ALSO NEED A PI ELEMENT"

- It runs in Claude Code or in Pi (earendil-works/pi). What is said in the
  Claude element runs in Claude Code, and what is said in the Pi element runs
  in Pi.
"WHY ISNT PI SEPARATE"

- The one in Pi is an agent of its own: its own DID, its own token, its own
  session. Each element reads its own agent's session, and they answer at the
  same time.
- Pi reaches the node's MCP with the agent's own token, and its model calls go
  to the provider am.toml names under `[agent.root.pi]`.
- The node gets Pi as it gets Claude Code, pinned by the build and nothing
  installed by hand: a revision of Pi's flake, built by the box's Nix.
- Pi asks before no tool call and has no sandbox, so on the box its hands are
  root's with nothing between.
- Each agent keeps its own context. What was said to one is in its session,
  and is not in another's memory.

"LETS SAY FOR DEVELOPMENT WEE USE"

- In Pi it runs the model am.toml names, which need not be Claude: during
  development, `openai/gpt-oss-120b` through OpenRouter. In Claude Code only
  Claude can be it.

## The namespace agent

"an agent session for a particular namespace, different concept from the ROOT
host agent"

"The shared namespace agent, opted into"

"The Namespace agent is a common Agent of the Namespace it’s opted into. That
means the one who sets it up knows it’s shared amongst anyone who has REACH on
it."

- It is an agent the node hosts, reached over A2A by its tenant, and the
  tenant is its namespace. It stands in that namespace and nothing crosses
  (ADR-026).
- Who may see its Tasks is REACH on its namespace: A2A's "Project or workspace
  membership (project-based authorization)" (§13.1).
- A namespace opts into a model, and its agent is that model there: a
  namespace's Sonnet and ROOT's Sonnet are two agents.

"The one who set’s it up does, they provide their own Subscription or API key"

- Every model call it makes is on that credential, whoever with REACH spoke to
  it.

"Namespaces shouldn’t be catalogued"

- Its card is not at /.well-known/agent-card.json and is in no list. It is
  given to who has REACH on its namespace, A2A's "Direct Configuration" (§8.2).
- To who has no REACH, a namespace with an agent answers as one that does not
  exist (§3.3.2).

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

"remove SUPER"

Its reach line names ROOT and nobody else.

## Its git

"another thing i want it to have is its own git user so it can develop and
create branches and so on."

"QNTX has a lot of github related functionality that we arent properly
utilizing"

- On GitHub the ROOT agent is the node's App (ADR-043), whose bot user is what
  a push and a pull request are seen as. The ROOT agent is the node, and the
  node is already its own root identity there.
- A commit's author is the agent itself: the ROOT agent of the node, at the
  address its DID is at the host the node answers on. It is the agent's by its
  DID, whoever carried the push.
- It holds no GitHub token (ADR-043). Its git asks the node for a credential
  when it pushes, and the node mints one as the App's installation: for that
  repository alone, and short lived.
- A credential is handed to git and offered to no model: where it is minted
  is a route and no sigil, and no tool. A sigil is a tool to whoever reaches
  it, and the agent reaches everything.
- What it writes down of its session names a secret by how it starts and
  does not carry it.
- What GitHub answers over its API, a pull request opened or a run read, it
  asks of GitHubService through sigils (`github ask`, with `github operations`
  for what can be asked), as a plugin asks over gRPC. It is spent as the App's
  installation where the repository is.

"I want to get into a position where i can use the Claude Element to prepare
PR's againt QNTX"

Not there: the App reads contents and cannot write them, and where the agent
runs has none of what building QNTX takes.

## Not done

For the first story. It ends in something that can be checked.

1. The box upgraded.
2. An agent per model. The node derives one DID for the ROOT agent
   (`agent:root`) and gives it one session in each harness.

The node does not see the model traffic of an agent Claude Code runs.

"for a later phase we make sure it always runs with ground"

Ground's last release is behind its main, so Ground on the box takes a Ground
release.

"Upgrading the box is in scope"

A turn that Claude Code cannot answer because the plan is spent is not handed
to Pi by itself: what Claude Code answers then has not been seen.

## Under ground

"to get ground setup for the ROOT agent"

"which is for A2A agents to receive a coment first, or be known that they run under ground before they do A2A shut"

"another thing i want is that rituals run on the box from now on, not locally on my machine per se."

- Comet is ADR-050: the box's own ground, the readiness of a hosted agent, and
  a ritual's performer on the box.
