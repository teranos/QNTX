# Mobile UX Vision

**Status:** Active — core touch interactions implemented, canvas navigation in progress

Mobile is not a compromise. It is the primary exploratory interface. Desktop adds power-user features.

## The Story

*Source: [tube-journey.test.ts](../../web/ts/state/tube-journey.test.ts) — every detail below is encoded as a passing test.*

### Jenny's Commute

Jenny is a biology researcher. Every morning she rides the Northern Line from Morden to Old Street — 35 minutes, 17 stations, 18 tunnel segments where connectivity drops to zero.

Last night, while Jenny slept, two things happened:

1. Her metagenomic pipeline finished processing a soil microbiome dataset. Novel gene clusters appeared in the results.
2. Parbattie, a field researcher in Guyana (UTC-4), worked through the evening documenting a rare flora inventory near Kaieteur Falls. Guyana's 11pm is London's 3am — by the time Jenny wakes, Parbattie's field notes (*Heliamphora chimantensis* pitcher plants, a possibly undiscovered orchid species, GPS coordinates) are already synced to the backend.

### 08:29 — Morden station

Jenny gets off her bike. She opens QNTX on her phone before boarding. Her local IndexedDB has only the AX glyph she left there yesterday. The backend has everything Parbattie documented overnight.

`mergeCanvasState` runs. Local wins on ID conflicts (Jenny's glyph positions preserved), backend-only items append. She sees her glyph plus Parbattie's two field note glyphs and their composition, all delivered in one merge. Two new glyphs, one composition. Jenny's screen now has the full picture.

### 08:31 — Board the train

WiFi works at Morden. Jenny spots a novel gene cluster in the pipeline results, taps it, adds an annotation. The sync completes before the train moves. The glyph gets a subtle 110% saturation boost — *synced, safe*.

### 08:31–08:34 — Tunnel: Morden → South Wimbledon

Connectivity drops. After 300ms debounce, the UI shifts to offline mode.

Jenny doesn't stop working. She identifies a candidate protein-coding gene and drags it onto the canvas. The glyph appears immediately but is marked `unsynced`. It goes ghostly — grayscale, border opacity 0.15. *This data exists only on your phone.*

Meanwhile, the cluster glyph she synced at Morden keeps its azure tint — saturate(65%), hue-rotate(10°), border opacity 0.35. *Synced and safe, just dormant while offline.*

Two visual states on the same screen, no labels needed. Ghostly = unreachable. Azure = safe.

### 08:34 — South Wimbledon

Connectivity returns. Auto-sync fires in the background. The candidate gene transitions: ghostly → syncing → synced. The grayscale filter dissolves into a 110% saturation boost over 1.5 seconds. CSS transitions do the work.

### 08:34–08:41 — Colliers Wood, Tooting Broadway, Tooting Bec, Balham

The pattern repeats. Tunnel → station → tunnel → station. Each station is a sync window. Jenny adds a homolog relationship in a tunnel, it syncs at Colliers Wood. She reviews the growing network in the next tunnel (everything azure). By Balham, all three discovery glyphs — novel cluster, candidate gene, homolog — are synced.

### 08:42–08:49 — Hypothesis formation

Between Balham and Stockwell, Jenny forms a hypothesis about the candidate protein's function. She creates a hypothesis glyph in a tunnel (ghostly), syncs it at Clapham South. Refines it across three more tunnel-station cycles.

At 08:48, between Clapham North and Stockwell, she wants to cross-reference her hypothesis against existing attestations. She spawns an AX glyph and types a query. The AX glyph has locally-cached attestation data in IndexedDB — it works offline.

Three distinct visual states on Jenny's screen:

| State | Appearance | Meaning |
|---|---|---|
| **Orange** | Full opacity, warm tint | AX glyph — actively querying local data |
| **Azure** | 65% saturation, 10° hue shift | Synced glyphs — safe, dormant |
| **Ghostly** | Grayscale, border 0.15 | Unsynced — only on this device |

Same connectivity, same moment, different visual treatments. The orange glyph is locally functional. The azure glyphs are safe. The ghostly ones need a station.

### 08:51–08:54 — Sync failure and recovery

At Oval, Jenny creates a validation note and sync begins. The tunnel to Kennington is long (3 minutes). Sync fails mid-transfer.

At Kennington, auto-retry fires. First retry fails (unstable connection). Exponential backoff. Second retry succeeds. The validation note transitions: `syncing → failed → syncing → failed → syncing → synced`. Seven state transitions, two failures, resilient recovery.

### 09:00–09:06 — City of London to arrival

London Bridge, Bank Station, Moorgate. Each station confirms everything is synced. At 09:06, Jenny exits at Old Street. All six glyphs — novel cluster, candidate gene, homolog, hypothesis, AX query results, validation note — are synced.

### 09:10 — Desktop continuation

Jenny sits down at her workstation. Same canvas URL. All mobile work is already there — no re-sync needed. She immediately begins deep analysis on a larger screen. The 35-minute commute produced a novel protein function hypothesis backed by local attestation cross-referencing, all built in tunnel segments averaging 2 minutes each.

---

## Visual State System

The color system encodes connectivity × sync × capability without labels or icons.

| Connectivity | Sync State | Local-Active? | Filter | Border | Name |
|---|---|---|---|---|---|
| Online | Synced | — | saturate(110%) | 1.0 | Enhanced color |
| Online | Syncing/Unsynced/Failed | — | saturate(100%) | 1.0 | Normal |
| Offline | Synced | No | saturate(65%) hue-rotate(10°) | 0.35 | Azure tint |
| Offline | Unsynced/Failed | No | grayscale(100%) | 0.15 | Ghostly |
| Offline | Any | Yes | none | 1.0 | Orange (local-active) |

CSS transitions animate between states over 1.5s. The researcher's mental model of data state is maintained without disrupting flow.

## Gestural Exploration

*Source: [tile-based-typed-ui.md](./tile-based-typed-ui.md) — semantic zoom modes*

Three modes serve distinct cognitive tasks:

| Mode | Gesture | What you see | When |
|---|---|---|---|
| **Focus** | Pinch in | Single glyph fills screen, full detail | Deep inspection |
| **Relational** | Pinch out slightly | Focused glyph + half of connected glyphs | Navigate relationships |
| **Overview** | Pinch out fully | Full graph | Orientation |

**Relational mode** is the key mobile innovation — shows relationship context without losing focus. Drag a connected glyph to center to navigate. Swipe back to return.

Jenny's 6-step sequence on the tube:

1. **Overview:** Pinch out — see full gene network, spot the novel cluster
2. **Focus:** Tap target gene glyph — full sequence annotations, expression data
3. **Relational:** Pinch out slightly — connected genes (homologs, co-expressed partners)
4. **Navigate:** Drag candidate protein-coding gene to center — function predictions
5. **Discovery:** Pinch to relational — see this gene's regulatory network
6. **Backtrack:** Swipe back — return to original cluster for comparison

All under 2 minutes per tunnel segment. Gestural exploration enables hypothesis formation despite intermittent connectivity.

## Related

- [Tube Journey Test](../../web/ts/state/tube-journey.test.ts) — the full scenario as passing tests
- [Tile-Based Typed UI](./tile-based-typed-ui.md) — semantic zoom, pane-based computing
- [Mobile Canvas UX Analysis](../analysis/mobile-canvas-ux.md) — implementation status and gap tracking
