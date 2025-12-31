# sym - QNTX Symbol System

Canonical symbols for QNTX SEG operations and system markers.

## Why Symbols?

**Visual grep.** Scan code, UI, or logs and instantly know which domain you're in. Symbols are stable across CLI, web UI, and documentation.

## SEG Operators

| Symbol | Command | Meaning |
|--------|---------|---------|
| `⍟` | i | Self — your vantage point into QNTX |
| `≡` | am | Structure — internal interpretation |
| `⨳` | ix | Ingest — import external data |
| `⋈` | ax | Expand — surface related context |
| `+` | as | Assert — emit an attestation |
| `=` | is | Identity — subject/equivalence |
| `∈` | of | Membership — element-of/belonging |
| `⌬` | by | Actor — catalyst/origin of action |
| `✦` | at | Event — temporal marker |
| `⟶` | so | Therefore — consequent action |

## System Symbols

| Symbol | Name | Purpose |
|--------|------|---------|
| `꩜` | Pulse | Async jobs, rate limiting, budget management |
| `✿` | PulseOpen | Graceful startup |
| `❀` | PulseClose | Graceful shutdown |
| `⊔` | DB | Database/storage (material retention substrate) |

## Usage

```go
import "github.com/teranos/QNTX/sym"

fmt.Printf("%s Starting async job...\n", sym.Pulse)
fmt.Printf("%s Query complete\n", sym.AX)
```

## Dual-Mode Input

Commands accept both symbols and text:
- `⨳ https://example.com` = `ix https://example.com`
- `⋈ contact` = `ax contact`

See `SymbolToCommand` and `CommandToSymbol` maps for bidirectional conversion.
