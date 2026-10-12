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
last month          → same time, one calendar month back (Mar 31 → Feb 28)
last year           → same time, one calendar year back (Feb 29 → Feb 28)
```

There is one resolver, `crates/ats/src/temporal.rs`. `ax`, `as ... on`, and
the HTTP `since`/`until`/`on` parameters all read time through it, in UTC.

### Relative Expressions

```
3 days ago, 3d ago  → now - 3 days
2 weeks ago, 2w ago → now - 14 days
5 hours ago, 5h ago → now - 5 hours
5m ago              → now - 5 minutes
2mo ago             → two calendar months back, as last month
in 2w               → now + 14 days
```

`3d`, `3 days`, `2d3h`, `3m12s` and `over 5y` are spans, read in one grammar
(`crates/ats/src/span.rs`): whole numbers each followed by `ms`, `s`, `m`, `h`,
`d`, `w`, `mo` or `y`, or their words, largest unit first and each unit once.
`m` is minutes; months are `mo`. Every place that shows
a length of time writes it in this grammar, so what is shown reads back:
`3d ago` on screen is `since 3d ago` in a query.

Words after `since`, `until`, `on`, `between` and `over` run to the next
keyword, so `since last month` and `over 5 years` need no quotes.

### Over: longer than

`over 5y` keeps attestations longer than five years. An attestation's type is
its predicate, and a type names where its attestations start and end with two
attributes, `start_field` and `end_field`, the way it names `rich_string_fields`:

```bash
# the employed type says where an employment starts and ends
POST /api/types {"name": "employed", "start_field": "from", "end_field": "until"}

# employments of ALICE that lasted longer than five years
ax ALICE is employed over 5y
```

Length is counted on the calendar from the start (`crates/ats/src/ax/over.rs`):
from 2020-02-29, five years ends 2025-02-28, and an end must be past it. An
attestation whose type names no ends, or whose ends hold no ISO time, has no
length and is not over. The limit counts what is kept. A watcher measures each
attestation as it arrives, by the definitions of the types it is of.

### ISO Date Formats

```
2025-01-15                    # Date only
2025-01-15T10:30:00Z          # ISO 8601 + timezone
2025-01-15T10:30:00           # ISO 8601 local
2025-01-15 10:30:00           # Space-separated
2025/01/15                    # Slashes, year first
```

Dates must be year first with a four-digit year. `01/15/2025` and
`03/04/2025` are refused: the second is April 3 to some and March 4 to others.

### Time Ranges

```bash
# Single boundary
since yesterday              # From yesterday to now
until tomorrow               # From start to tomorrow

# Specific day (the UTC calendar day, midnight to midnight)
on 2025-01-15
on yesterday

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

- `crates/ats/src/parser/` - AX query parser (`mod.rs`) and AS command parser (`as_command.rs`)
- `crates/ats/src/temporal.rs` - Temporal expression resolver
- `ats/parser/dispatch_qntxwasm.go` - Go's calls into them through the ats WASM module
- `ats/parser/error.go` - Error types and formatting
