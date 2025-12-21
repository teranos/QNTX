# ATS Parser

Natural language parser for AX (query) and AS (attestation) commands.

## Overview

This package provides parsers for:
- **AX queries** - Natural language attestation queries
- **AS commands** - Attestation creation statements
- **Temporal expressions** - Time range parsing

## AX Query Grammar

### Production Rules

```
query      ::= subjects? predicates? contexts? actors? temporal? flags?
subjects   ::= TOKEN+
predicates ::= ("is" | "are") TOKEN+
contexts   ::= ("of" | "from") TOKEN+
actors     ::= ("by" | "via") TOKEN+
temporal   ::= timeExpr | timeRange
timeExpr   ::= ("since" | "until" | "on") EXPR
timeRange  ::= "between" EXPR "and" EXPR
flags      ::= ("--limit" INT | "--format" STRING)*
```

### Semantic Symbols

- `=` (is/are) — Identity/equivalence
- `∈` (of/from) — Membership/element-of
- `⌬` (by/via) — Actor/catalyst
- `✦` (temporal) — Time marker

### Query Examples

```bash
# Simple subject query
ax PATIENT-123

# Relationship query (healthcare)
ax PATIENT-456 has diagnosis of TYPE_2_DIABETES

# Corporate hierarchy query
ax SUBSIDIARY-XYZ has parent_org of HOLDINGS_INC

# Actor filtering (legal domain)
ax by court-system since yesterday

# Multiple actors
ax by court-clerk filing-system

# Complex query with time range (education)
ax STUDENT-789 enrolled_in COURSE-CS101 by registrar on 2025-01-15
```

## Temporal Expression Parsing

### Natural Language Shortcuts

```
today, now          → current timestamp
yesterday           → now - 1 day
tomorrow            → now + 1 day
last week           → now - 7 days
last month          → now - 1 month
```

### Relative Expressions

```
3 days ago          → now - 3 days
2 weeks ago         → now - 14 days
5 hours ago         → now - 5 hours
```

### ISO Date Formats

```
2025-01-15                    # Date only
2025-01-15T10:30:00Z          # ISO 8601 + timezone
2025-01-15T10:30:00           # ISO 8601 local
2025-01-15 10:30:00           # Space-separated
```

### Time Ranges

```bash
# Single boundary
since yesterday              # From yesterday to now
until tomorrow               # From start to tomorrow

# Specific day (spans full day 00:00:00-23:59:59)
on 2025-01-15

# Range
between 2025-01-01 and 2025-02-01
```

## AS Command Grammar

### Production Rules

```
command    ::= subjects predicates contexts? actors? temporal? attributes?
subjects   ::= TOKEN+
predicates ::= ("is" | "are" | "has") TOKEN+
contexts   ::= ("of" | "from" | "in") TOKEN+
actors     ::= ("by" | "via") TOKEN+
temporal   ::= ("on" | "since" | "until") EXPR
attributes ::= ("--" KEY VALUE)*
```

### Command Examples

```bash
# Healthcare: patient diagnosis
as PATIENT-123 has diagnosis of HYPERTENSION

# Legal: case filing
as CASE-456 filed_in DISTRICT_COURT

# Education: student enrollment
as STUDENT-789 enrolled_in COURSE-BIO201 on 2025-01-15

# Corporate: subsidiary relationship with temporal marker
as COMPANY-XYZ has parent_org of HOLDINGS_INC since 2024-01-01

# With attributes (any domain)
as ENTITY-999 has property of VALUE-ABC --verified true
```

## Parsing Architecture

### State Machine Flow

```
Input → Keyword Detection → Segmentation → State Machine → Filter/Command
```

The parser uses a hybrid approach:
1. **Keyword detection** - Identify semantic transitions (is, of, by, since)
2. **Segmentation** - Group tokens by role (subjects, predicates, contexts)
3. **State machine** - Parse each segment according to grammar rules

### State Transitions (AX)

```
SUBJECTS → (is/are) → PREDICATES → (of/from) → CONTEXTS
                                                    ↓
                                                 (by/via)
                                                    ↓
                                                 ACTORS
                                                    ↓
                                              (since/until/on)
                                                    ↓
                                                 TEMPORAL
```

All states are valid terminating states (query can end at any stage).

## Error Handling

The parser provides detailed error messages with suggestions:

```go
type ParseError struct {
    Message         string
    Suggestions     []string
    PartiallyParsed *Filter  // What was successfully parsed
}
```

Partial parsing allows valid segments to be processed even when later segments fail.

## Implementation Files

- `ax.go` / `ax_test.go` - AX query parser
- `as.go` / `as_test.go` - AS command parser
- `temporal.go` / `temporal_test.go` - Time expression parser
- `semantic.go` / `semantic_test.go` - Semantic analysis
- `error.go` - Error types and formatting
- `position.go` / `position_test.go` - Position tracking for errors
