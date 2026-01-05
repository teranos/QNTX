# Documentation Consistency Fixes - Summary

## Changes Made Based on Your Clarifications

### 1. Symbol System Reorganization ✅

**Created clear categories:**
- **Primary SEG Operators** (7 symbols with UI components): ⍟, ≡, ⨳, ⋈, ⌬, ✦, ⟶
- **Attestation Building Blocks** (3 symbols, no UI): +, =, ∈
- **System Infrastructure** (5 symbols): ꩜, ✿, ❀, ⊔, ▣

**Files Updated:**
- `/docs/GLOSSARY.md` - Created definitive glossary
- `/CLAUDE.md` - Fixed symbol definitions and categories
- `/README.md` - Updated segment section
- `/sym/symbols.go` - Reorganized code with clear categories
- `PaletteOrder` now only includes primary SEG operators

### 2. Configuration Consistency ✅

**Clarified file naming:**
- Both `am.toml` (preferred) and `config.toml` (compatibility) accepted
- UI config always writes to `~/.qntx/config_from_ui.toml`
- Future consideration: `qntx.toml` as unified standard

**Files Updated:**
- `/am/README.md` - Clarified precedence and naming

### 3. ATS Definition Clarification ✅

**ATS is now clearly defined as:**
- A type system (data model)
- A storage system (persistence)
- A query language (ax subsystem)

**Files Updated:**
- `/ats/README.md` - Added comprehensive definition
- `/docs/GLOSSARY.md` - Clear ATS entry

### 4. Budget System Documentation ✅

**Documented the relationship:**
- `ai/tracker` records API calls
- `pulse/budget` aggregates and enforces limits

**Files Created:**
- `/docs/architecture/budget-tracking.md` - Complete architecture doc

### 5. Future Vision Separation ✅

**Created issue templates for aspirational features:**
- Multi-provider config support
- Documentation drawer
- Real-time log streaming
- Package manager distribution

**Files Created:**
- `/docs/FUTURE_VISION_ISSUES.md` - Items to move to GitHub issues

### 6. Documentation Standards ✅

**Established clear principles:**
- Philosophy first, then usage
- Document reality, not aspiration
- Focus on "why" not "what"
- Honest about broken features

**Files Created:**
- `/docs/README_TEMPLATE.md` - Standard structure for package READMEs

### 7. Async Job Documentation ✅

**Acknowledged JobMetadata existence:**
- Updated to reflect actual implementation
- Documented two-phase job pattern

**Files Updated:**
- `/docs/architecture/pulse-async-ix.md` - Fixed incorrect claim
- `/docs/architecture/two-phase-jobs.md` - Created pattern documentation

## Key Philosophy Clarifications

Based on your answers:

1. **≡ definitively means "am" (configuration)**, not "by"
2. **Symbols +, =, ∈ are attestation primitives**, not UI components
3. **Database terminology** can use both technical and poetic descriptions
4. **Pulse symbol (꩜)** should always prefix related logs
5. **Keyboard shortcuts** are user-configurable
6. **⌬ (by)** represents all forms of actor (creator, source, user)
7. **Configuration shows source** in UI for debugging
8. **Configuration is pluggable** (unlimited sources possible)
9. **Empty config values are invalid** (not "use default")
10. **Core is minimal**, all domains are plugins using gRPC
11. **Documentation honesty** - mark broken features as broken
12. **Future vision in GitHub issues**, not docs

## Remaining Considerations

### Symbol for "of" (∈)
You mentioned considering a more typeable alternative for ∈. Possible options:
- `@` - "predicate @ context"
- `~` - "predicate ~ context"
- `^` - "predicate ^ context"
- Keep ∈ but add keyboard shortcut

### Configuration File Naming
You liked both option C (either am.toml or config.toml) and option D (qntx.toml). Consider standardizing on `qntx.toml` in a future version for clarity.

### User-Defined Retention Policies
You chose "hard limits with oldest deletion by default" but "user-defined policies later" - this is tracked for future implementation.

## Branch Status

All changes are on branch: `docs/analysis-and-consistency-review`

Ready to commit with message focusing on WHY:
"Fix documentation inconsistencies to match actual implementation and mental model"