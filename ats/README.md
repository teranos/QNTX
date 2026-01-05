# ⨳ ⋈ + = ⌬  ✦ ⟶

# ATS - Attestation Type System

The ATS (Attestation Type System) is both:
- **A type system**: Defining the data model and structure of attestations
- **A storage system**: Managing persistence and retrieval of attestations
- **A query language**: The `ax` subsystem for querying attestations

Together, these components provide a domain-agnostic framework for attesting, ix-ing, and ax-ing about entities.

## Why ATS?

Traditional databases ask: "What is the schema?" They assume you know the structure upfront, bake it into code, and treat data as facts.

**The problem**: Real systems are about claims, not facts. You don't know if `hr-system@company` is right that Alice works here - you know that the HR system *said* it. Provenance matters. Attribution matters. Time matters.

Without attestations, you either:
- **Trust blindly** - store data as facts, lose who said what
- **Build attribution yourself** - reinvent metadata tracking in every table, inconsistently

**ATS is the answer**: Treat data as verifiable claims from the start. Every piece of information knows who attested to it and when.

## Why Attestations?

An attestation is a verifiable claim, not a fact.

At its simplest, an attestation is a statement of the form:

```
as [Subject] is [Predicate] of [Context] by [⌬ Actor] on [✦ Temporal]
```

This pattern captures:
- **What** was claimed (subject, predicate, context)
- **Who** claimed it (actor)
- **When** they claimed it (temporal)

The claim might be wrong. The actor might be unreliable. But the attestation itself is verifiable - someone did say this at this time.

## Why Interfaces?

QNTX is **not tailor-made boutique software**. ATS is a new way of thinking about types and data - it must work for domains we haven't imagined yet.

**Why not configuration?** Can't predict what people need. Your domain's synonym logic, actor detection, entity resolution - these are complex, domain-specific behaviors. Configuration files can't capture that flexibility.

**Interfaces keep ATS truly domain-agnostic**: Core infrastructure stays pure. Your domain knowledge plugs in without forking.

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

## Why ASIDs?

**Debugging/readability**: Seeing `as-node_type-contact` in logs beats UUID gibberish.

**Vanity IDs for fundamentals**: Type definitions and other canonical attestations deserve stable, well-known IDs that systems can reference consistently. The alias system then maps duplicates to these canonical IDs.

## Features

- **ASID generation** with vanity ID support and collision detection
- **Attestation existence checking** to prevent duplicates

**Supporting Packages:**

- **`ix/` ⨳** - Framework for building data ingesters ([see ix/README.md](ix/README.md))
- **`ax/` ⋈** - Query and retrieval operations ([see ax/README.md](ax/README.md))
- **`parser/`** - Command parsing ([see parser/README.md](parser/README.md))
- **`alias/`** - Identity resolution system
- **`../sym/`** - Canonical symbol definitions (SEG operators and Pulse)

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
