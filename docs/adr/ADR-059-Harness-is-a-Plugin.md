# Harness is a plugin that speaks gRPC A2A

"Harness is a plugin that speaks grpc A2A"

"NOT BE ENTIRELY DEPENDENT ON ONE HARNESS", "WHY ISNT PI SEPARATE" ([[ADR-048-Node-Agent]]).

## Status

Proposed.

## Decision

Each harness (Claude Code, Pi, Codex, agy) is a plugin that provides a service, as an `LLMProvider` does ([[ADR-001-domain-plugin-architecture]]): "A plugin that provides a service registers it with core. Other plugins can consume these services through `ServiceRegistry` without knowing which plugin implemented the `.proto`." The `.proto` is A2A's own `A2AService`, served over gRPC (A2A §10), a2a-go's `a2agrpc.NewHandler`.

"Core mediates everything" ([[ADR-003-plugin-communication]]). Core speaks to a harness with a2a-go's client and `a2agrpc.WithGRPCTransport`, as it speaks to a peer's agent and to the agent a card names (`server/a2a_client.go`); the node's own A2A executor hands a turn to the harness the agent runs in, and a plugin that wants an agent, a Rooms plugin among them ([[ADR-057-Agent-Rooms]]), asks core, not the harness.

A harness is added, configured, swapped and built as every plugin is ([[ADR-002-plugin-configuration]]), in any language gRPC speaks: Pi's own included.

A harness that waits on a person, a permission it is asked for, says so as A2A does: `TASK_STATE_AUTH_REQUIRED` (§7.6) or `TASK_STATE_INPUT_REQUIRED` (§3.4). "Via Agent" approvals are not started ([[ADR-052-Approvals]]).

## Already answered

- **Records.** A plugin writes into the node's store, through `ServiceRegistry` ([[ADR-003-plugin-communication]]); the node signs what it writes (`signer_did`, `atsstore.proto`), in the store of its namespace ([[ADR-046-a-plugin-stands-in-a-namespace]]). A Task's lines stay where they are.
- **Keys.** A key leaves the node "into the environment of the harness process the node starts for that namespace" ([[ADR-051-namespace-keys]]).
- **Spend.** "The node does not see the model traffic of an agent Claude Code runs" ([[ADR-048-Node-Agent]]); what a turn spent is what it answers with, as now.

## Open

- **A node with no harness.** "QNTX runs in minimal core mode without any plugins", "plugins are optional enhancements, not essential features" ([[ADR-001-domain-plugin-architecture]]). With every harness a plugin, such a node has no Node Agent ([[ADR-048-Node-Agent]]).
- **A turn across a restart.** A restart kills the plugin's process group and starts a new process ([[ADR-018-Plugin-lifecycle]]); today a turn outlives the node and the next node reads on from its stream.
- **Keys into the plugin.** A plugin is given its config at Initialize, a map of strings (`domain.proto`).
- **Who a Task's lines are by.** The agent attests as itself ([[ADR-048-Node-Agent]]); the store takes the actors a plugin names.
- **Whose Task id.** An A2A server names the Tasks it makes: the harness names its own, and the node's Task is the one its people see.

## Related

- [[ADR-001-domain-plugin-architecture]]
- [[ADR-003-plugin-communication]]
- [[ADR-014-llm-as-plugin-provided-service]]
- [[ADR-018-Plugin-lifecycle]]
- [[ADR-053-Namespace-Agents]]
- [[ADR-057-Agent-Rooms]]: "A Plugin Provided Rooms Service can use the existing QNTX managed harnesses."
