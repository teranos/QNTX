# QNTX Development Guide

## Working with this Project

**Read, don't infer:**
- Stick to what's explicitly stated in code and documentation
- Don't add features, explanations, or context that weren't requested
- If something is unclear, ask - don't assume or fill in gaps
- State only what you can directly verify

## Segments

QNTX uses segments across the project:

- **꩜** (Pulse) - Async operations, rate limiting, job processing
- **⌬** (Actor/Agent) - of Actors/agents in the attestation system
- **≡** (Configuration) - am Configuration and system settings
- **⨳** (Ingestion) - ix Data ingestion operations
- **⋈** (Join/Merge) - ax Entity merging and relationship operations

**Note:** These symbols are defined in the `sym` package for consistent use across QNTX.

**For Claude**: Use these segments consistently when referencing system components.

## Go Development Standards

### Code Quality

- **Deterministic operations**: Use sorted map keys, consistent error patterns, predictable behavior

### Error Handling

- **Context in errors**: Errors should provide sufficient context for debugging
