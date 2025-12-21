# ⨳ ⋈ + = ⌬  ✦ ⟶

# ATS - Attestation Type System

The ATS (Attestation Type System) provides a domain-agnostic framework for attesting, ix-ing, and ax-ing about entities.

An attestation is a verifiable claim, not a fact.

At its simplest, an attestation is a statement of the form:

```
as [Subject] is [Predicate] of [Context] by [⌬ Actor] on [✦ Temporal]
```

## Extensibility

Customize ATS behavior through interfaces: `QueryExpander` (semantic search), `ActorDetector` (actor identification), `EntityResolver` (entity aliases).

### Data Models

```go
// AttestationFields - Marshaled JSON fields for database operations
type AttestationFields struct {
    SubjectsJSON   string
    PredicatesJSON string
    ContextsJSON   string
    ActorsJSON     string
    AttributesJSON string
}

```

## Features

- **ASID generation** with vanity ID support and collision detection
- **Attestation existence checking** to prevent duplicates

**Supporting Packages:**

- **`ix/` ⨳** - Framework for building data ingesters ([see ix/README.md](ix/README.md))
- **`ax/` ⋈** - Query and retrieval operations ([see ax/README.md](ax/README.md))
- **`parser/`** - Command parsing ([see parser/README.md](parser/README.md))
- **`alias/`** - Identity resolution system

```go
// Check if attestation already exists
exists, err := ats.AttestationExists(db, attestation)
if err != nil {
    return err
}

if !exists {
    // Create new attestation
    err = ats.CreateAttestation(db, attestation)
    if err != nil {
        return err
    }
}
```

### Alias System Integration

```go
import "github.com/teranos/QNTX/ats/alias"

aliasResolver := alias.NewResolver(db)
// Alias creation is handled by ats/alias
// Attestation storage supports aliased entities seamlessly
```

## Testing

```bash
# Run ats package tests
go test ./ats/...

# Run with verbose output
go test ./ats/... -v

```
