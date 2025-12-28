# Graph Package

The graph package is the **first real implementation** of QNTX's attested types vision.

### Why Self-Describing Types

Types must be attested because QNTX is a **platform, not boutique software**:

```
as alice node_type contact
as rust node_type skill
```

**Portability**: Clipboard-friendly. Share types via text message, any platform.
**Composability**: Core remains agnostic. Works for any domain without modification.
**Transparency**: Type information lives in attestations, not hidden in code or database schema.

### Why Typespace

Type definitions use the reserved `type` predicate in a separate typespace:

```
Subject:    artist
Predicate:  type
Context:    graph
Attributes: {
  "display_color": "#e74c3c",
  "display_label": "Artist",
  "opacity": 0.9
}
```

## Why Deterministic Operations

Graph output must be **scientifically valid** - same inputs always produce same outputs:

- **Provability**: Testability = provability. Deterministic tests prove behavior.
- **Version control**: Diffs show data changes, not ordering artifacts.
- **Reproducibility**: Required for scientific validity.

## Why Domain-Agnostic

QNTX is a **platform**. Graph infrastructure must work for any domain without modification:

- **No hardcoded types**: No `NodeTypeGroups`, no recruitment-specific logic
- **Pure infrastructure**: Attestation expansion, type extraction, graph construction

## See Also

- [ATS Types](../ats/types/) - Attestation data structures
- [AX Package](../ats/ax/) - Attestation query execution
- [QNTX Testing](../internal/testing/) - Migration-based test helpers
