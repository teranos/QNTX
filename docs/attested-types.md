# Attested Types

Types exist through attestation - 'restaurant' is real because someone attested it, not because a schema declares it. That attestation carries everything: what fields matter, which ones are searchable, how it appears visually, and crucially, who made these decisions and when. Types can contradict, overlap, and evolve because they're attestations - the mess is the message. Multiple actors might attest different meanings for 'restaurant' - a health inspector sees safety fields, a food critic sees ambiance, a delivery app sees logistics. This multiplicity isn't a problem to solve - it's the actual shape of knowledge in contested domains. Types become part of the conversation, not the rules that govern it.

## Attestation format

A type attestation is: `[typeName] is type`. No context — a type exists because it was attested, not because it belongs to a namespace. The `source` field records who attested it. Attributes carry display metadata (color, label, opacity) and semantic information (rich string fields, array fields).

The type name is its own actor (self-certifying in typespace), avoiding bounded storage limits.

Relationship types follow the same pattern: `[predicateName] is relationship_type`.