# Package README Template

All package READMEs should follow this structure (philosophy first, then usage):

---

# [Package Name]

[Optional: Symbol if applicable, e.g., ꩜ for Pulse]

[One-line description of what this package does]

## Why [Package Name]?

[2-3 paragraphs explaining the philosophy and problem this solves]

**The problem**: [What problem exists without this package]

**[Package] is the answer**: [How this package solves it]

## Philosophy

[Optional: Additional philosophical points or design principles]

- **Principle 1**: Explanation
- **Principle 2**: Explanation
- **Principle 3**: Explanation

## Core Concepts

[Define key terms and concepts specific to this package]

### Concept 1
[Definition and explanation]

### Concept 2
[Definition and explanation]

## Usage

### Basic Example

```go
// Simple, most common use case
import "github.com/teranos/QNTX/[package]"

// Example code
```

### Advanced Usage

```go
// More complex example showing additional features
```

## API

[Key functions/types - link to godoc for complete reference]

### Main Functions

- `FunctionName()` - Brief description
- `AnotherFunction()` - Brief description

### Key Types

- `TypeName` - Brief description
- `AnotherType` - Brief description

## Configuration

[If applicable: How to configure via am.toml]

```toml
[package]
setting = "value"
```

## Integration

[How this package interacts with other QNTX components]

- **Uses**: What other packages this depends on
- **Used by**: What packages depend on this
- **Events**: Any events emitted/consumed

## Testing

```bash
# Run tests
go test ./[package]/...

# With coverage
go test -cover ./[package]/...
```

## Implementation Notes

[Optional: Important implementation details, not aspirational]

- Note about specific design decision
- Performance consideration
- Known limitation with explanation

## Related Documentation

- [Link to architecture doc if exists]
- [Link to related packages]
- [Link to glossary for terms]

---

## Template Usage Notes

When using this template:

1. **Philosophy first**: Start with WHY, then WHAT, then HOW
2. **Focus on "why" not "what"** in descriptions
3. **Use concrete examples** not abstract descriptions
4. **Document reality** not aspiration
5. **Keep examples simple** and directly runnable
6. **Link to other docs** rather than duplicating
7. **Be honest** about limitations or incomplete features

Remove any sections that don't apply to your package.