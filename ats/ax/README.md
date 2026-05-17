# Ax - Attestation Query

The `ax` package provides natural language querying capabilities for attestations, with alias resolution and advanced sameness analysis.

## Overview

This package implements:

- **Query execution** for natural language queries with literal matching
- **Sameness analysis** and conflict resolution
- **Alias resolution** for identity equivalence
- **Advanced classification** with temporal logic

## Data Models

```go
// AxFilter - Query specification
type AxFilter struct {
    Subjects   []string     // Entities to query
    Predicates []string     // What to match (literal)
    Contexts   []string     // Context filtering
    Actors     []string     // Actor filtering
    TimeStart  *time.Time   // Temporal range start
    TimeEnd    *time.Time   // Temporal range end
    Format     string       // Output format (table/json)
    Limit      int          // Result limit
}

// AxResult - Query execution results
type AxResult struct {
    Attestations []models.As   // Matching attestations
    Conflicts    []Conflict    // Sameness analysis results
    Summary      AxSummary     // Statistical summary
    Format       string        // Display format
}
```

## Features

**Query Execution:**

- **Natural language parsing** with flexible grammar
- **Temporal expressions** (yesterday, last week, ISO dates)
- **Literal predicate matching**
- **Alias resolution** for identity equivalence
- **Cartesian expansion** for multi-dimensional attestations

**Sameness Analysis (Advanced Classification):**

- **Evolution detection** (temporal updates with same actor)
- **Verification detection** (multiple sources confirming)
- **Coexistence detection** (different contexts)
- **Supersession detection** (authority overrides)
- **Resolution strategies** that filter duplicate results
- **Actor credibility hierarchy** (Human > LLM > System > External)
