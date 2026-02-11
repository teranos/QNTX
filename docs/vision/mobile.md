# Mobile UX Vision

**Status:** Active — core touch interactions implemented, canvas navigation in progress

Mobile is not a compromise. It is a primary exploratory interface. Desktop adds power-user features.

## The Story

*Encoded as a test*: [tube-journey.test.ts](../../web/ts/state/tube-journey.test.ts)

### Jenny's Commute

Jenny is a biology researcher in London. Every morning she rides the Northern Line from Morden to Old Street — 35 minutes, 17 stations, 18 tunnel segments where connectivity drops to zero.

Last night, while Jenny slept, two things happened:

1. Her metagenomic pipeline finished processing a soil microbiome dataset. Novel clusters appeared in the results.
2. Parbattie, a field researcher in Guyana (UTC-4), worked through the evening documenting a rare flora inventory near Kaieteur Falls. Guyana's 11pm is London's 3am — by the time Jenny wakes, Parbattie's field notes (*Heliamphora chimantensis* a pitcher plant, and a possibly undiscovered orchid species, GPS coordinates) are already synced to the shared QNTX instance.

### 08:29 — Morden station

Jenny gets off her bike. She opens QNTX on her phone before boarding. Her phone only has the AX glyph she left there yesterday. The server has everything Parbattie documented overnight.

The phone and server merge their states. Jenny's glyph positions stay where she left them, and Parbattie's new work appears alongside. She sees her glyph plus Parbattie's two field note glyphs and their composition, all delivered in one merge. Two new glyphs, one composition. Jenny's screen now has the full picture.

### 08:31 — Board the train

WiFi works at Morden. Jenny spots a novel gene cluster in the pipeline results, taps it, adds an annotation. The upload completes before the train moves. The glyph brightens slightly — *saved*.

### 08:31–08:34 — Tunnel: Morden → South Wimbledon

Connectivity drops. After a brief delay, the UI shifts to offline mode.

Jenny doesn't stop working. She identifies a candidate protein-coding gene and drags it onto the canvas. The glyph appears immediately but is marked as not yet saved. It goes ghostly — grayscale, faint border. *This data exists only on your phone.*

Meanwhile, the cluster glyph she saved at Morden keeps its azure tint — cooler color, visible but dormant. *Saved and safe, just dormant while offline.*

Two visual states on the same screen, no labels needed. Ghostly = unreachable. Azure = safe.

### 08:34 — South Wimbledon

Connectivity returns. Automatic upload begins in the background. The candidate gene transitions: ghostly → saving → saved. The grayscale filter dissolves into a subtle brightening over about a second and a half. Smooth, automatic.

### 08:34–08:41 — Colliers Wood, Tooting Broadway, Tooting Bec, Balham

The pattern repeats. Tunnel → station → tunnel → station. Each station is an upload window. Jenny adds a homolog relationship in a tunnel, it saves at Colliers Wood. She reviews the growing network in the next tunnel (everything azure). By Balham, all three discovery glyphs — novel cluster, candidate gene, homolog — are saved automatically.

### 08:42–08:49 — Hypothesis formation

Between Balham and Stockwell, Jenny forms a hypothesis about the candidate protein's function. She creates a hypothesis glyph in a tunnel (ghostly), saves it at Clapham South. Refines it across three more tunnel-station cycles.

At 08:48, between Clapham North and Stockwell, she wants to cross-reference her hypothesis against existing attestations. She spawns an AX glyph and types a query. The AX glyph works with data already stored on her phone — it works offline.

Three distinct visual states on Jenny's screen:

| State | Appearance | Meaning |
|---|---|---|
| **Orange** | Full opacity, warm tint | AX glyph — actively querying local data |
| **Azure** | Cooler tint, softer | Saved glyphs — safe, dormant |
| **Ghostly** | Grayscale, faint | Not yet saved — only on this device |

Same connectivity, same moment, different visual treatments. The orange glyph is locally functional. The azure glyphs are safe. The ghostly ones need a station.

### 08:51–08:54 — Upload failure and recovery

At Oval, Jenny creates a validation note and the upload begins. The tunnel to Kennington is long (3 minutes). Upload fails mid-transfer.

At Kennington, automatic retry. First retry fails (unstable connection). Waits a bit longer. Second retry succeeds. The validation note transitions through multiple attempts: trying → failed → trying → failed → trying → saved. Seven state transitions, two failures, resilient recovery.

### 09:00–09:06 — City of London to arrival

London Bridge, Bank Station, Moorgate. Each station confirms everything is saved. At 09:06, Jenny exits at Old Street. All six glyphs — novel cluster, candidate gene, homolog, hypothesis, AX query results, validation note — are saved.

### 09:10 — Desktop continuation

Jenny sits down at her workstation. Same canvas URL. All mobile work is already there — no re-upload needed. She immediately begins deep analysis on a larger screen. The 35-minute commute produced a novel protein function hypothesis backed by attestation cross-referencing, all built in tunnel segments averaging 2 minutes each.

---

## Visual State System

*Implementation note: Visual state is also influenced by work in [#466](https://github.com/teranos/QNTX/pull/466).*

The color system shows what's safe and what needs saving, without labels or icons.

| Connection | Status | Local-only work? | Appearance | Name |
|---|---|---|---|---|
| Online | Saved | — | Brightened | Enhanced |
| Online | Saving/Not saved/Failed | — | Normal color | Normal |
| Offline | Saved | No | Cooler tint, softer | Azure |
| Offline | Not saved/Failed | No | Grayscale, faint | Ghostly |
| Offline | Any | Yes | Full color, active | Orange |

Transitions between states animate smoothly over about a second and a half. Your mental model of what's safe stays intact without disrupting your flow.

## Gestural Exploration

*Related concept: [tile-based-typed-ui.md](./tile-based-typed-ui.md)*

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
- [Tile-Based Typed UI](./tile-based-typed-ui.md) — pane-based computing
- [Mobile Canvas UX Analysis](../analysis/mobile-canvas-ux.md) — implementation status and gap tracking
