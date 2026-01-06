# Contributing to QNTX

## Development Philosophy

**Use Claude Code for development.** See [CLAUDE.md](CLAUDE.md) for project-specific instructions that Claude follows.

## Contribution Workflow

1. **Write code** - Implement your feature or fix
2. **Open a PR** - Create pull request with clear description
3. **Pass CI** - Ensure all tests and checks pass
4. **Claude Review** - Comment `@Claude review` for automated review
5. **Implement feedback** - Fix issues, defer non-critical items, document decisions
6. **Iterate** - Repeat steps 3-5 until review passes
7. **Manual testing** - Verify behavior matches intent
8. **Update PR** - Add comprehensive description and screenshots
9. **Merge** - Once approved and tested

## Code Standards

- Focus on "why" not "what" in comments
- Document reality, not aspiration
- Use error wrapping with context (see [errors/README.md](errors/README.md))
- Follow testing patterns in [CLAUDE.md](CLAUDE.md#testing)

## Documentation

- Update relevant documentation with your changes
- Follow [README template](docs/README_TEMPLATE.md) for new packages
- Add entries to [GLOSSARY.md](docs/GLOSSARY.md) for new concepts
- Future vision goes in GitHub issues, not documentation

## Common Issues

### Tests Failing?
- Use `qntxtest.CreateTestDB(t)` for database tests
- Never create schemas inline
- Check migrations are up to date: `make migrate`

### Type Generation?
- Never edit `types/generated/*` directly
- Fix the generator, then run: `make types`

### Symbol Confusion?
- See [GLOSSARY.md](docs/GLOSSARY.md) for definitive meanings
- Primary SEG operators: ⍟ ≡ ⨳ ⋈ ⌬ ✦ ⟶
- System symbols: ꩜ ✿ ❀ ⊔ ▣
