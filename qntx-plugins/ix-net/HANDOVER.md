# ix-net Image Serving & Browser Integration — Handover

## Vision

Claude Code runs through ix-net (HTTPS MITM proxy). ix-net captures images from the API traffic — screenshots, diagrams, anything Claude sees or produces. These images are valuable context for PR descriptions but can't be inserted programmatically (GitHub API doesn't support images in PR bodies via API or `gh`).

Xffexlex (Firefox extension) solves this by showing image thumbnails when editing a PR description on GitHub. Left-click inserts into the editor (via synthetic paste), right-click dismisses. The extension talks to ix-net through QNTX's HTTP handler infrastructure to fetch the images.

Longer term, Xffexlex was envisioned as a general "window into QNTX" — the browser becomes a QNTX node. Firefox extensions can run WASM, so the extension could carry `qntx-wasm` directly and only need the localhost connection for server-side operations (ATS queries, plugin endpoints).

## What was built

### ix-net HTTP endpoints (D, plugin.d)

Three routes added to `handleHTTP`:

- `GET /images?branch=<name>` — resolves branch to capture sessions via ATS, returns `{ branch, sessions: [{ session, images: [...] }] }`
- `GET /images?session=<id>` — lists image files in a session directory, returns `{ session, images: [...] }`
- `GET /images/<session>/<filename>` — serves raw image bytes with correct MIME type and cache header

**Branch → session resolution logic** (in `handleImagesByBranch`):
1. Query ATS for attestations with predicate `"captured"` (ix-net capture attestations)
2. Extract `session_id` and `image_dir` from each attestation's attributes
3. For each session, query ATS for `"PreToolUse"` attestations with context `"session:<id>"`
4. Search raw attribute bytes for `"checkout -b <branch>"` or `"checkout <branch>"`
5. If found, that session belongs to this branch — list its images

### ix-net glyph (D inline JS, web/glyph-module.ts)

Status dashboard showing:
- Proxy status (listening / not reachable)
- Capture count, image count
- Input/output token totals
- Last model and status code

Uses GlyphUI SDK: `ui.container()`, `ui.pluginFetch('/captures')`, `ui.statusLine()`, `ui.onCleanup()`. Refreshes every 5 seconds.

The glyph module is served inline as a JS string from plugin.d at `/glyph-module.js`. The typed source lives at `web/glyph-module.ts` for editing with proper tooling — these must be kept in sync manually. The glyph is registered via Phase 1 (gRPC `registerGlyphs`) but also exports `glyphDef` so Phase 2 auto-discovery works.

### Configurable proxy port (D, plugin.d + am.toml)

The proxy reads `proxy_port` from the `[ix-net]` section in `am.toml` (passed via `InitializeRequest.config`). Defaults to 9100. This prevents port collisions when multiple QNTX instances run ix-net simultaneously.

`claude.fish` parses the same `am.toml` to read the port, so the wrapper and plugin stay in sync automatically.

### Xffexlex Firefox extension (TypeScript, separate repo)

Location: `/Users/s.b.vanhouten/SBVH/teranos/Xffexlex`

Architecture:
- **Background script** (`src/background.ts`) — sole QNTX communication layer. Content scripts never talk to localhost directly. Fetches from ix-net endpoints, converts image blobs to data URLs (blobs can't cross the message boundary). Port configurable via `browser.storage.local`.
- **Content script** (`src/content-github-pr.ts`) — injected on `https://github.com/*/pull/*`. Detects edit mode via MutationObserver watching for `textarea[name='pull_request[body]']`. Sends messages to background script to fetch images. Renders thumbnail panel above textarea. Click inserts via synthetic ClipboardEvent paste. Right-click dismisses thumbnail.
- **Manifest** (`manifest.json`) — manifest v2, permissions: `activeTab`, `storage`, `http://localhost/*`. Extension ID: `xffexlex@teranos`.

Builds with `bun run build` → `dist/background.js`, `dist/content-github-pr.js`.

### Supporting changes

- `ats.d` — added `getAttestations()` method on ATSClient for querying ATS via gRPC
- `proto.d` — added `AttestationFilter`, `GetAttestationsRequest`, `GetAttestationsResponse` structs
- `proxy.d` — changed `jsonEscape` and `getImageDir` from `private` to `package` visibility

## What has NOT been tested

- **Image-serving endpoints** — never curled. The branch→session ATS query logic is entirely untested.
- **The glyph** — never spawned in the QNTX UI. Port collision issue was hit, then the glyph was rewritten, never retried.
- **Xffexlex end-to-end** — extension loads in Firefox (confirmed), but never tested: fetch images → show thumbnails → click to insert. The background script architecture (replacing direct fetch with message passing) was never tested in browser.
- **Configurable port** — v0.9.6 with port config was built but QNTX was not restarted to verify it binds to the configured port.
- **claude.fish am.toml parsing** — the fish script's TOML parser is simple (line-by-line, looks for `[ix-net]` section, extracts `proxy_port`). Not tested.
- **Image deduplication** — ix-net captures accumulate duplicate images across API calls (each call includes all previous images in context). The endpoints return all files. No deduplication logic exists on server or client side.

## Known limitations and fragility

1. **Branch→session search is fragile** — searches raw protobuf attribute bytes for string `"checkout -b <branch>"`. Could false-match if a branch name appears in unrelated attribute data. Could also miss branches checked out with `git switch -c` or other variations.

2. **Glyph source duplication** — the JS module is inlined as a D string in `plugin.d` and separately exists as `web/glyph-module.ts`. These must be manually kept in sync. A better approach would be to have the Makefile compile the TS and embed it, or have the plugin read it from disk at runtime.

3. **Image paths are hardcoded to `$HOME`** — `getImageDir()` resolves to `~/.qntx/files/ix-net/`. This is shared across all QNTX instances. Multiple ix-net instances write to the same directory tree, differentiated only by session ID.

4. **`make install` overwrites certs** — the Makefile copies certs from the source tree to `~/.qntx/certs/`. Running `make install` from one QNTX instance replaces another instance's certs. This caused TLS failures during development when tmp and tmp3 had different CA certificates.

5. **No CORS headers** — removed because the extension uses background script fetch (not subject to CORS). If anything else needs to call these endpoints from a browser page context, CORS headers would need to be re-added.

## Files changed (PR #669)

```
qntx-plugins/ix-net/source/ixnet/ats.d       — getAttestations() method
qntx-plugins/ix-net/source/ixnet/plugin.d    — image endpoints, glyph, port config
qntx-plugins/ix-net/source/ixnet/proto.d     — AttestationFilter, request/response types
qntx-plugins/ix-net/source/ixnet/proxy.d     — private → package visibility
qntx-plugins/ix-net/source/ixnet/version_.d  — 0.8.0 → 0.9.6
qntx-plugins/ix-net/web/glyph-module.ts      — typed glyph source (new file)
qntx-plugins/ix-net/claude.fish              — reads proxy_port from am.toml
am.toml                                       — [ix-net] proxy_port = 9176
```

## Commits

1. `ix-net: serve captured images over HTTP for browser integration` — endpoints + ATS queries
2. `ix-net: remove CORS headers from image endpoints` — background script approach
3. `ix-net: configurable proxy port, GlyphUI glyph, claude.fish reads am.toml`

## If continuing

The critical path to a working demo:
1. Restart QNTX, verify ix-net binds to configured port (check logs for `proxy auto-started on port 9176`)
2. Run Claude Code through the proxy, capture some images
3. `curl http://localhost:8776/api/ix-net/images?session=<session-id>` — verify endpoint returns image list
4. `curl http://localhost:8776/api/ix-net/images?branch=<branch-name>` — verify branch resolution works (this is the most likely failure point — depends on attestation format)
5. Load Xffexlex in Firefox (`about:debugging` → Load Temporary Add-on → `manifest.json`)
6. Open a PR for the branch, click edit, verify thumbnail panel appears
7. Click a thumbnail, verify it pastes into the textarea

The branch→session ATS query is the riskiest piece. If the attestation format has changed or the raw byte search doesn't find matches, that's where to debug first.
