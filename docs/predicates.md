# Predicates

A predicate is what is being asserted. In the [attestation](attestation.md) `[Subject] is [Predicate] of [Context] by [Actor]`, the predicate is the verb.

## System predicates

| Predicate | Meaning |
|---|---|
| `_` | Existence. Default when none specified. |
| [`type`](attested-types.md) | Type definition. |
| [`distill:*`](adr/ADR-020-attestation-distillation.md) | Distillation namespace. Prefixed on summarized attestations. |
