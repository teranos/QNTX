# Attestation Context Naming Convention

## Overview

QNTX uses domain-specific contexts in attestations to enable precise filtering and querying. Contexts represent the scope or domain of an attested action, allowing queries like `ax over github` to find only GitHub-related attestations.

## Naming Strategy

**Use domain-specific contexts for better granularity and filtering.**

### Core Principles

1. **Specific over broad**: Prefer `"github"` over `"code-domain"` for GitHub operations
2. **Domain-based**: Context names reflect the external system or subsystem (`"github"`, `"gopls"`, `"ixgest-git"`)
3. **Hierarchical when needed**: Use hyphen-separated names for sub-domains (e.g., `"github-pr"`, `"github-issues"`)
4. **Reserved generic**: Use `"code-domain"` only for cross-cutting concerns that don't fit a specific domain

### Rationale

Domain-specific contexts enable:
- **Precise querying**: `ax over github` finds only GitHub attestations
- **Domain isolation**: Separate tracking for different external integrations
- **Scalability**: Easy to add new domains without naming conflicts
- **Analytics**: Domain-specific metrics and reporting

## Defined Contexts

### Code Plugin Contexts

| Context | Usage | Example Operations |
|---------|-------|-------------------|
| `github` | GitHub PR and issue operations | Fetching suggestions, listing PRs |
| `gopls` | Go language server operations | Initialization status, diagnostics |
| `ixgest-git` | Git repository ingestion | Commit processing, attestation generation |
| `code-domain` | Cross-cutting code operations | Generic file access (use sparingly) |

### Future Context Patterns

When adding new contexts, follow these patterns:

**External Services:**
- Use service name: `"gitlab"`, `"bitbucket"`, `"jira"`
- Add sub-domain if needed: `"gitlab-mr"`, `"jira-issue"`

**Ingestion Domains:**
- Use `ixgest-*` prefix: `"ixgest-npm"`, `"ixgest-pypi"`, `"ixgest-cargo"`

**Language Servers:**
- Use tool name: `"rust-analyzer"`, `"typescript-ls"`, `"python-ls"`

**Internal Subsystems:**
- Use subsystem name: `"pulse"`, `"repl"`, `"server"`

## Examples

### GitHub PR Operations
```go
cmd := &types.AsCommand{
    Subjects:   []string{"pr-153"},
    Predicates: []string{"fetched-suggestions"},
    Contexts:   []string{"github"},  // Specific to GitHub
}
```

### Git Ingestion
```go
cmd := &types.AsCommand{
    Subjects:   []string{"/path/to/repo"},
    Predicates: []string{"ingested"},
    Contexts:   []string{"ixgest-git"},  // Specific ingestion domain
}
```

### Gopls Initialization
```go
cmd := &types.AsCommand{
    Subjects:   []string{"gopls"},
    Predicates: []string{"initialized"},
    Contexts:   []string{"gopls"},  // Language server specific
}
```

## Query Examples

```bash
# Find all GitHub-related attestations
ax over github

# Find all git ingestion operations
ax over ixgest-git

# Find gopls status changes
ax over gopls

# Find attestations across all code operations (broader)
ax over code-domain
```

## Migration Notes

When refactoring existing attestations:

1. Identify the specific domain (GitHub, gopls, git ingestion, etc.)
2. Use domain-specific context instead of generic `"code-domain"`
3. Update queries to use new context names
4. Document context changes in commit messages

## Anti-Patterns

**Avoid:**
- ❌ Overly generic contexts: `"code"`, `"system"`, `"app"`
- ❌ Inconsistent naming: `"github"` vs `"GitHub"` vs `"gh"`
- ❌ Action-based contexts: `"read"`, `"write"`, `"fetch"` (use predicates instead)
- ❌ Deep hierarchies: `"code-github-pr-suggestion-fetch"` (too specific)

**Prefer:**
- ✅ Domain names: `"github"`, `"ixgest-git"`, `"gopls"`
- ✅ Simple hierarchies: `"github-pr"`, `"ixgest-npm"`
- ✅ Consistent casing: Always lowercase, hyphen-separated
