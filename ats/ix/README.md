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

```go
type DataIngester struct {
    db     *sql.DB
    dryRun bool
    actor  string
}

func (i *DataIngester) ProcessData(filePath string) error {
    // 1. Parse source data
    data, err := parseSourceFile(filePath)
    if err != nil {
        return err
    }

    // 2. Generate attestations
    generator := ix.NewAttestationGenerator("data-source")
    attestations := []types.As{}

    for _, record := range data {
        att := generator.GenerateAttestation(record)
        attestations = append(attestations, att)
    }

    // 3. Execute attestations
    executor := ix.NewExecutionHelper(i.dryRun, i.actor)
    for _, att := range attestations {
        if err := executor.ExecuteAttestations(i.db, []types.As{att}, false); err != nil {
            return err
        }
    }

    return nil
}
```

### Structured Execution Pattern

```go
func ExecuteDataSource(ctx context.Context, filePath string, dryRun bool, opts ix.StructuredOptions) (ix.Result, error) {
    result := ix.NewResult("data-source")

    // Parse and validate input
    data, err := parseFile(filePath)
    if err != nil {
        result.AddError("parse", "invalid_file", err.Error())
        return result, err
    }

    // Generate attestations
    generator := ix.NewAttestationGenerator("data-source")
    attestations := generateAttestations(generator, data)

    // Execute with dry-run support
    executor := ix.NewExecutionHelper(dryRun, opts.Actor)
    if err := executor.ExecuteAttestations(db, attestations, opts.IncludeTrace); err != nil {
        result.AddError("execute", "attestation_failed", err.Error())
        return result, err
    }

    // Populate result
    for _, att := range attestations {
        result.AddAttestation(att)
    }

    return result, nil
}
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
