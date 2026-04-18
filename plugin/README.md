# QNTX Plugin System

- PULSE: According to ADR-004 we do have Plugin-Pulse integration, i think we should look into making LLM calls using the LLM gRPC service a Pulse managed abstraction. A new QNTX plugin called Voor is likely to create a lot of requests, right now it's looking like it's getting written to be managing it's own queue, and for sure the priority. So you have Werf nominating a lot of matches, and then Voor who decides what should be something we spend an LLM call on, and then there is Pulse who will also manage a queue of work and will decide how to allocate resources to each. I think what pissed me off about Pulse in the beginning was how deeply tied it was to domain logic. now a lot of that seems to be decoupled. but now the problem seems to be returning. Resource allocation has domain implications, so how do you manage this? 

| ADR | Decision |
|-----|----------|
| [ADR-001](../docs/adr/ADR-001-domain-plugin-architecture.md) | Domain plugin architecture |
| [ADR-002](../docs/adr/ADR-002-plugin-configuration.md) | Plugin configuration management |
| [ADR-003](../docs/adr/ADR-003-plugin-communication.md) | Plugin communication patterns |
| [ADR-004](../docs/adr/ADR-004-plugin-pulse-integration.md) | Plugin-Pulse integration for dynamic async handlers |
| [ADR-006](../docs/adr/ADR-006-proto-as-source-of-truth.md) | Protocol Buffers as single source of truth for types |
