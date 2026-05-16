# Ax - Attestation Query

The `ax` package provides natural language querying capabilities for attestations, with alias resolution and advanced sameness analysis.

## Overview

This package implements:

- **Query execution** for natural language queries with literal matching
- **Sameness analysis** and conflict resolution
- **Alias resolution** for identity equivalence
- **Advanced classification** with temporal logic

## Commands

### `qntx ax` - Query Attestations

Query the knowledge graph with natural language:

```bash
# Basic queries
qntx ax ENTITY-123                   # What do we know about ENTITY-123?
qntx ax is member of ACME            # Who are ACME members?
qntx ax has certification            # Find entities with certifications
qntx ax by registry since yesterday  # Recent registry attestations

# Temporal comparison queries (over)
qntx ax over 5y                      # Find entities with duration over 5 years
qntx ax has certification over 3y    # Certifications held over 3 years
qntx ax is participant over 2y       # Participation duration over 2 years
qntx ax over 6m                      # Over 6 months duration (m = months, y = years)

# Output options
qntx ax ENTITY-456                   # Clean mode (default): just attestations
qntx ax ENTITY-456 --verbose         # Verbose mode: sameness analysis + summary
qntx ax ENTITY-456 --format=json     # JSON output for scripts
qntx ax ENTITY-456 --limit=50        # Limit results

# Display modes
qntx ax ENTITY-789                   # Clean table (no ASID, no sameness analysis)
qntx ax ENTITY-789 --verbose         # Full table with sameness analysis and statistics
qntx ax ENTITY-789 --summary         # Summary statistics only
```

### Alias Resolution

Aliases work automatically in ax queries:

```bash
# Aliases are resolved transparently
qntx ax ENTITY-A                     # Finds data for both ENTITY-A and ALT-ID-123
qntx ax J271Z                        # Returns data for both primary and alternative IDs
qntx ax 'FULL-NAME'                  # Also finds data for abbreviated forms
```

## Architecture

### Data Models

```go
// AxFilter - Query specification for ax commands
type AxFilter struct {
    Subjects       []string     // Entities to query
    Predicates     []string     // What to match (literal)
    Contexts       []string     // Context filtering
    Actors         []string     // Actor filtering
    TimeStart      *time.Time   // Temporal range start
    TimeEnd        *time.Time   // Temporal range end
    OverComparison *OverFilter  // Numeric comparison (e.g., "over 5y")
    Format         string       // Output format (table/json)
    Limit          int          // Result limit
}

// OverFilter - Temporal/numeric comparison for duration queries
type OverFilter struct {
    Value    float64 // Numeric value (e.g., 5 for "5y")
    Unit     string  // Unit: "y" for years, "m" for months
    Operator string  // Comparison: "over" means >=
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
- **Temporal comparisons** ("over 5y" for duration filtering)
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
