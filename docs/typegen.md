# TypeGen - Type Generator

Generate TypeScript (and other language) type definitions from Go structs.

## Quick Start

```bash
# Generate types to stdout
qntx typegen

# Generate to files
qntx typegen --output types/generated/

# Generate specific packages
qntx typegen --packages pulse/async,server
```

## Makefile Integration

```bash
make types        # Generate types
make types-check  # Verify types are up to date
```

## Struct Tags

### `json` Tag
Controls field naming and optionality:

```go
type Job struct {
    ID       string `json:"id"`              // Required field: "id"
    Status   string `json:"status,omitempty"` // Optional field: "status?"
    Internal string `json:"-"`                // Skipped
}
```

### `tstype` Tag
Override TypeScript type:

```go
type Event struct {
    Timestamp time.Time `json:"timestamp" tstype:"Date"`
    Metadata  any       `json:"metadata" tstype:"Record<string, unknown>"`
}
```

Force optional:
```go
Count int `json:"count" tstype:",optional"`  // count?: number
```

### `readonly` Tag
Generate readonly fields:

```go
type Config struct {
    Version string `json:"version" readonly:""`  // readonly version: string
}
```

## Adding New Types

1. Add struct to appropriate package (e.g., `server/types.go`)
2. Add JSON tags: `json:"field_name"`
3. Export the struct (capitalize first letter)
4. Run `make types`
5. Import in TypeScript: `import { YourType } from '@/types/generated/typescript'`

## Troubleshooting

**Types out of date in CI?**
- Run `make types` locally and commit the changes

**Import not found?**
- Check the struct is exported (starts with capital letter)
- Verify JSON tags are present
- Check `types/generated/typescript/index.ts` includes your type

**Wrong TypeScript type?**
- Use `tstype` tag to override: `` `tstype:"YourCustomType"` ``

## Future Languages

Python, Rust, and Dart generators planned for v1.0.0.
