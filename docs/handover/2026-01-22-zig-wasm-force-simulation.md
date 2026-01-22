# Handover: Zig/WASM Force Simulation

**Date:** 2026-01-22
**Branch:** `claude/review-state-machines-yA8QG`
**Commit:** `cc03313`

## Context

The user is considering moving parts of the TypeScript frontend to Zig/WASM. This emerged from a discussion about:

1. **State machine patterns in the TS codebase** - Review found the codebase is mostly pragmatic, with some duplication in confirmation logic and error classification, but nothing severely over-engineered.

2. **Accepting stateful complexity** - The user wants to "accept that the frontend is going to evolve into a stateful machine" rather than fight it.

3. **Vision alignment** - The vision documents describe:
   - Tile-based semantic UI with 1000+ tiles
   - Temporal layer with time-travel and z-axis depth
   - Performance as a non-negotiable constraint

4. **Outgrowing D3** - The immediate pain points are:
   - Physics performance (force simulation)
   - DOM overhead (1000 DOM nodes with events, style recalcs)

## What Was Built

### Files Created

```
web/wasm/force/
├── force.zig      # Core simulation (~280 LOC)
├── build.zig      # WASM build configuration
└── README.md      # Build instructions

web/ts/graph/force-wasm.ts   # TypeScript bridge (D3-compatible API)
```

### Files Modified

- `flake.nix` - Added `pkgs.zig` to devShell
- `Makefile` - Added `wasm-force` target

### Force Implementation

| Force | Complexity | Config |
|-------|------------|--------|
| Link | O(links) | distance=100, strength=0.1 |
| Many-body | O(n²) | strength=-2000, maxDist=400 |
| Center | O(n) | strength=0.05 |
| Collision | O(n²) | radius=60 (per node) |

The O(n²) implementations are intentionally naive. Zig's tight loops without GC will outperform JS even at O(n²). Quadtree optimization is a clear next step if needed.

### TypeScript Bridge API

```typescript
const sim = await ForceSimulation.create(nodes, links);
sim.center(width / 2, height / 2);
sim.charge(-2000);
sim.on('tick', () => renderPositions(sim.nodes()));
sim.alpha(0.3).restart();

// Drag support
sim.fix(nodeId, x, y);
sim.unfix(nodeId);
```

## Architecture Direction

```
┌─────────────────────────────────────────────┐
│  Zig/WASM Core (owns truth)                 │
├─────────────────────────────────────────────┤
│  • Tile state (position, size, fields)      │
│  • Force simulation physics                 │
│  • Spatial index (quadtree - what's visible)│
│  • Temporal index (state at time T)         │
│  • Zoom state machine                       │
└──────────────────┬──────────────────────────┘
                   │
┌──────────────────▼──────────────────────────┐
│  TypeScript Shell (renders + IO)            │
├─────────────────────────────────────────────┤
│  • Canvas/WebGL rendering                   │
│  • User input → WASM state transitions      │
│  • WebSocket → WASM updates                 │
│  • UI chrome (panels, palettes)             │
└─────────────────────────────────────────────┘
```

This is the long-term direction. The force simulation is step 1.

## Building

```bash
# Option 1: Nix (recommended)
nix develop
make wasm-force

# Option 2: Direct Zig install
brew install zig  # macOS
# or curl from ziglang.org for Linux
make wasm-force

# Output: web/wasm/dist/force.wasm
```

## Not Done (Intentional)

- **Typegen for Zig** - User explicitly said "do not worry about typegen yet"
- **Quadtree optimization** - Start simple, optimize when needed
- **Integration with existing graph code** - User should test WASM first
- **Canvas/WebGL rendering** - Current D3 DOM rendering still works, WASM just provides positions

## Next Steps

1. **Build and test** - Run `make wasm-force`, load in browser, verify positions
2. **Benchmark** - Compare tick times: D3 vs WASM at 100, 500, 1000 nodes
3. **Integrate** - Replace D3 simulation in `renderer.ts` with WASM version
4. **Spatial index** - If semantic zoom needs "what's visible at this zoom", add quadtree
5. **Temporal index** - For time-travel, add state snapshots in WASM
6. **Canvas rendering** - Move from DOM to Canvas when DOM overhead becomes the bottleneck

## Key Decisions Made

1. **Start with physics** - D3 force simulation is a common first WASM migration, well-scoped
2. **O(n²) is fine initially** - No premature optimization; Zig's raw speed compensates
3. **Fixed buffer allocator** - 1MB static buffer, no heap allocation, deterministic
4. **D3-compatible API** - Bridge maintains familiar `alpha()`, `restart()`, `on('tick')` interface
5. **Float32 throughout** - Cache-friendly, sufficient precision for screen coordinates

## Questions for Continuation

- What's an acceptable tick budget? (Current D3 likely ~16ms for 60fps)
- Should focus mode physics (weaker charge, tighter collision) be configurable from JS or hardcoded modes in Zig?
- When does Canvas rendering become the priority vs. more WASM state?
