# OpenClaw Integration — PR Handover

## What this is

OpenClaw is a workspace of plain markdown files (`~/.openclaw/workspace/`) that an AI agent maintains as its persistent identity and memory. This PR makes those files observable from the QNTX canvas in real time.

## Two commits

### 1. `qntx-claw` Rust plugin (`52eb6db`)

A new Rust crate following the `qntx-python` plugin pattern. Discovers the workspace, takes SHA-256-addressed snapshots, and watches for file changes with `notify` (300ms debounce, content-hash dedup).

**HTTP endpoints** (routed via Go backend at `/api/claw/*`):
- `GET /snapshot` — full workspace snapshot (bootstrap files + daily memories)
- `GET /changes` — recent change events from the watcher
- `GET /bootstrap`, `GET /memory`, `GET /file/:name` — individual access

**Key design choice**: `parking_lot::RwLock` for sync state, `tokio::RwLock` for the snapshot Arc. The parking_lot guard is extracted and dropped before any `.await` to avoid the `!Send` across await boundary problem.

10 tests, all passing.

### 2. OpenClaw canvas frontend (`e964989`)

A fullscreen tray glyph (`manifestationType: 'fullscreen'`) that renders each workspace file as a result-glyph-style card on a spatial grid. Dark terminal background, monospace, header bar with filename and content SHA.

- Polls `/api/claw/snapshot` every 3s
- Only re-renders cards whose SHA changed
- MutationObserver cleanup stops polling when glyph is removed
- All inline styles, no custom CSS — matches result-glyph patterns exactly

Registered in `default-glyphs.ts` alongside the main canvas.

## What's not in this PR

- **WebSocket push** instead of polling — polling is simple and sufficient for now, but the watcher already emits change events that could drive a WS channel
- **ATSStore attestation** on workspace changes — the infrastructure is there (change events, SHAs) but no attestations are created yet
- **Card drag/reposition** — cards are statically grid-placed; could use `canvasPlaced` for free-form arrangement later
- **Markdown rendering** — content is shown as raw text (`pre-wrap`); a lightweight markdown pass would improve readability
