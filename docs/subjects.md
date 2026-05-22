# Subjects

A subject is the entity being attested about. In `[Subject] is [Predicate] of [Context] by [Actor]`, the subject is the noun.

**Subjects are claim-bearing names, not identifiers.** Use `alice`, `vacancies`, `pulse`, `model:qwen-2.5-7b` — names a reader can recognize. Don't use UUIDs, content hashes, database IDs, or numeric identifiers; those carry no claim, they only point.

Dates and bare years are also poor subjects: time already has its own slot via `✦`. Putting `2026-05-20` in the subject duplicates what the temporal field exists for.

## Write-time warning

Storage logs a `WARN` when a subject matches an id-like shape. Heuristics (string-method-based, no regex):

| Shape | Examples |
|---|---|
| UUID (8-4-4-4-12 hex) | `550e8400-e29b-41d4-a716-446655440000` |
| ≥16-char contiguous hex run | content hashes, `deadbeefdeadbeef…` |
| Trailing `_<digits>` or `-<digits>` | `user_123`, `item-42` |
| All-numeric | `12345` |

Dates (`2026-05-20`) and bare years (`2026`) also trigger the warning. These aren't identifiers, but they aren't good subjects either — time belongs in the `✦` slot.

Implemented in `ats/storage/subject_warn.go`.
