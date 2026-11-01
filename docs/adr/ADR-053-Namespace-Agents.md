# ADR-053: Namespace Agents

Date: 2026-10-18
Status: Proposed

"an agent session for a particular namespace, different concept from the ROOT host agent (see: [[ADR-048-Node-Agent]])"

"The shared namespace agent, opted into"

"The Namespace agent is a common Agent of the Namespace it’s opted into. That
means the one who sets it up knows it’s shared amongst anyone who has REACH on
it."

- It is an agent the node hosts, reached over A2A by its tenant, and the
  tenant is its namespace. It stands in that namespace and nothing crosses
  ([[ADR-026-Namespaces]]).
- Who may see its Tasks is REACH on its namespace: A2A's "Project or workspace
  membership (project-based authorization)" (§13.1).
- A namespace opts into a model, and its agent is that model there: a  namespace's Sonnet and ROOT's Sonnet are two agents.

"The one who set’s it up does, they provide their own Subscription or API key"

- Every model call it makes is on that credential, whoever with REACH spoke to
  it.

"Namespaces shouldn’t be catalogued"

- Its card is not at /.well-known/agent-card.json and is in no list. It is
  given to who has REACH on its namespace, A2A's "Direct Configuration" (§8.2).
- To who has no REACH, a namespace with an agent answers as one that does not
  exist (§3.3.2).

## Pi

"I want Pi to also serv the function of namespace agent alongside Claude code
namespace agent"

- A namespace opts into an agent in Pi beside the one in Claude Code: an AGENT
  line whose predicate is pi, naming the model, the thinking level and the
  provider.
- Its model calls are made at that provider on the namespace's key of the
  provider's name (ADR-051), handed to Pi's process in the variable Pi reads
  it from.
- It reaches the node's MCP with its own token, and its session is written in
  the namespace.

## Codex

“make Codex selectable and runnable as a namespace agent, including configuration, protocol responses, session persistence, and signed transcripts.”

“Auth would be another PR, but hold off on it”

An AGENT line with predicate `codex` names the model and explicit sandbox. The
existing namespace operations select it with `harness=codex`. It uses its own
MCP token and Ground, and keeps its QNTX transcript session separate from the
Codex thread used for resumption. See the [runtime documentation](https://github.com/teranos/QNTX/blob/main/internal/codex/README.md)
for prerequisites and limits. Authentication setup is deferred.

## Not done

- “ok, for namespace, but we wont develop this out yet.”

  Story 28: USER runs the namespace agent at Anthropic

  ```gherkin
  Feature: USER runs the namespace agent at Anthropic

    "oidc for each namespace each with their own rpid"
    "we will not be doing API key anymore"

    Scenario: USER registers the namespace's issuer
      Given I am standing in a namespace
      When I register the namespace's issuer at https://platform.claude.com/settings/workload-identity-federation
      And I give the namespace its rule and service account
      Then the namespace's agent runs in Anthropic's environment, off the box
      And my Anthropic organization pays for its model calls
      And no Anthropic API key is kept
  ```

- The namespace agent: its own DID derived from the node's key for that
  namespace and model, its own home, session and token, signed in through
  claude login (ADR-051), a sandbox around the turn, and one test of the whole
  claim: it fails to reach the node's environment, its config, another
  namespace, the box's AWS role and its own credential file, and its model
  call succeeds on its own sign-in. [15-user-goes-on-to-the-namespace-agent.feature](https://github.com/teranos/QNTX-Web/blob/main/stories/15-user-goes-on-to-the-namespace-agent.feature)
