# ADR-053: Namespace Agents

Date: 2026-10-18
Status: Proposed

"Right now we arent treating namespace agent with respect, it requires digging
by you to separate the two properly, so Namespace Agents need to be it's own
ADR"

"an agent session for a particular namespace, different concept from the ROOT
host agent"

"The shared namespace agent, opted into"

"The Namespace agent is a common Agent of the Namespace it's opted into. That
means the one who sets it up knows it's shared amongst anyone who has REACH on
it."

- It is an agent the node hosts, reached over A2A by its tenant, and the
  tenant is its namespace. It stands in that namespace and nothing crosses
  (ADR-026).
- Who may see its Tasks is REACH on its namespace: A2A's "Project or workspace
  membership (project-based authorization)" (§13.1).
- A namespace opts into a model, and its agent is that model there: a
  namespace's Sonnet and ROOT's Sonnet are two agents.
- The Node Agent is ROOT's and ADR-048's. A namespace agent is not the Node
  Agent, and is not kept, configured or run as a kind of it.

"The one who set's it up does, they provide their own Subscription or API key"

- Every model call it makes is on that credential, whoever with REACH spoke to
  it.

"Node and Namespace configuration should be considered entirely different
things" (ADR-051)

- Which model it is and how it runs is a line in the namespace's own store.
  Nothing of it is in am.toml.

"Namespaces shouldn't be catalogued"

- Its card is not at /.well-known/agent-card.json and is in no list. It is
  given to who has REACH on its namespace, A2A's "Direct Configuration" (§8.2).
- To who has no REACH, a namespace with an agent answers as one that does not
  exist (§3.3.2).

## Pi

"I want Pi to just serve as Namespace Agent"

- Short term, Pi is a namespace agent's harness, and not the Node Agent's.

## What it may do

"this high level of permissiveness should be reserved for stronger families of
Models exclusively."

"Other models, like the weaker gpt-oss-120b Ought to be under deny by default,
and the attempt should be recorded. And then later on, when I feel like it, I
approve docs.trusted.website"

"Opus and Fable, in the Agent element, through real table or tablet of reach
attestation DSL lines"

"Deny by default for everything it wants to do."

## Its way out

"any way of getting to the outside happens via QNTX by any means"

"the model needs to not change anything, not remember anything, doesn't even
know all of it's searches and external requests go through qntx
unconditionally"

## Not here

"I want to defer fine grained permission control for when I have managed to
make ground work with Pi"
