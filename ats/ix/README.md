# ⨳ IX - Data Ingestion Utilities

IX (⨳) provides reusable components for building data ingesters.

## Why IX?

**Domain-agnostic ingestion.** QNTX needs to ingest from any data source - CSVs, APIs, LLMs, databases - without baking source-specific logic into core.

Generator pattern keeps ingestion infrastructure separate from domain knowledge.

## Why Dry-Run?

**Avoid API costs during testing.** Some ingesters call LLM APIs. Dry-run lets you test ingestion logic without burning API credits every time.

## Components

- **AttestationGenerator** - Domain-agnostic attestation creation
- **ExecutionHelper** - Manages execution with dry-run support
- **Result types** - Structured feedback (evolving)

## Usage Patterns

### Basic Ingester Structure

```
ProcessData(filePath, dryRun):
    1. Parse source data from filePath
    2. Create AttestationGenerator for your data source
    3. For each record, generate an attestation
    4. Create ExecutionHelper with dryRun flag
    5. Execute attestations against the store
```

### Structured Execution Pattern

```
ExecuteDataSource(filePath, dryRun, options):
    result = new Result("data-source")

    data = parseFile(filePath)
    if error: result.AddError("parse", "invalid_file", error)

    generator = new AttestationGenerator("data-source")
    attestations = generateAttestations(generator, data)

    executor = new ExecutionHelper(dryRun)
    executor.ExecuteAttestations(store, attestations, options.IncludeTrace)

    return result
```

## Design Principles

- No assumptions about data source types
- Reusable across different ingestion scenarios
- Minimal dependencies on external packages

### Composability

Components can be mixed and matched:

- Use `AttestationGenerator` without `ExecutionHelper`
- Combine result types with custom logic
- Integrate with existing systems
