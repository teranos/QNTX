# sym - QNTX Symbol System

- TODO: Review, I havent touched this for more than 3 months, that means its very stable? i think this stuff is still permeating throughout QNTX, so very important indeed. but the UI has been going ham on creating many more symbols, in a way the sym are becoming the glyph you could say? Or more like a subset.

Canonical symbols for QNTX SEG operations and system markers.

## Why Symbols?

**Visual grep.** Scan code, UI, or logs and instantly know which domain you're in. Symbols are stable across CLI, web UI, and documentation.

**Cognitive compression.** Your brain pattern-matches `꩜` faster than parsing "pulse". One symbol = instant context.

## SEG Operators

| Symbol | Command | Meaning |
|--------|---------|---------|
| `⍟` | i | Self — your vantage point into QNTX |
| `≡` | am | Structure — internal interpretation |
| `⨳` | ix | Ingest — import external data |
| `⋈` | ax | Expand — surface related context |
| `+` | as | Assert — emit an attestation |
| `=` | is | |
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
