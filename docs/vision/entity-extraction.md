# Entity Extraction

## LLM over SpaCy

The industry is moving from rule-based and trained NER (SpaCy, Stanford NER) toward LLM-based entity extraction. The advantages for QNTX:

- **No model training**: SpaCy NER requires training data specific to your domain. LLMs generalize across domains with prompting alone.
- **Richer extraction**: beyond named entities (person, org, location), an LLM can extract relationships, intents, and domain-specific concepts from attestation text.
- **Consistency with labeling**: cluster labeling already uses the LLM pipeline. Entity extraction reuses the same infrastructure — provider selection, budget tracking, attestation of results.

## Entities as Attestations

Extracted entities are attested by `qntx@entities` (or similar system actor). Each extraction is an attestation: "this text mentions entity X with role Y." The entity itself becomes a node in the knowledge graph, linked to the attestation it was extracted from.

This means entity extraction is queryable, syncable, and auditable — the same properties as every other attestation. You can ask "what entities were extracted from this cluster?" or "which attestations mention this entity?"

## Integration with Embeddings

Extracted entities can be embedded alongside attestation text. A cluster might contain attestations about "Kubernetes deployment" — entity extraction identifies the specific services, namespaces, and error codes mentioned. These entities, once embedded, might form their own sub-clusters or strengthen existing ones.

## When

Not now. The cluster labeling pipeline proves the LLM-over-attestations pattern works. Entity extraction is the same pattern applied more broadly. Worth building when there's a concrete use case that cluster labels alone don't serve.
