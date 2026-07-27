# Contributing to QNTX

Thank you.

## Contribution Workflow

1. **Write code** - Implement your feature or fix
2. **Open a PR** - Create pull request with clear description
3. **Pass CI** - Ensure all tests and checks pass
4. **Implement feedback** - Fix issues, defer non-critical items, document decisions
5. **Iterate** - Repeat steps 3-5 until review passes
6. **Manual testing** - Verify behavior matches intent
7. **Update PR** - Add comprehensive description and screenshots
8. **Merge** - Once approved and tested

## Code Standards

- Focus on "why" not "what" in comments
- Document reality, not aspiration
- Incorporate your words verbatim into comments and commits.
- Use error wrapping with context (see [errors/README.md](errors/README.md))
- Strict TDD

## Documentation

- Update relevant documentation with your changes
- Add entries to [GLOSSARY.md](docs/GLOSSARY.md) for new concepts
- Cross-reference related docs (no doc is an island)

## Common

### Type Generation?

- Never edit `types/generated/*` directly
- Use: `make types`
- See [typegen.md](docs/typegen.md) for struct tags and troubleshooting

