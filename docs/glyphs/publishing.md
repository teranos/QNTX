# Publishing a glyph

A glyph's UI is an attestation. You publish one, and every page in that
namespace redraws it where it stands — no node restart, no rebuild of QNTX, no
file on the box.

```bash
qntx glyph publish crier --file dist/glyph-module.js --to https://api.q.abcd.nl
qntx glyph list --to https://api.q.abcd.nl
```

## What a published glyph is

Subject `glyph-<name>`, predicate `glyph:module`, the module's source under the
`source` attribute. Publishing again supersedes: the store answers newest
first, so the last one published is the one served, and the ones before it stay
where they are.

The node serves what is published from `/g/<name>.js`. That is a root of its
own rather than a path under `/api`, because a page imports what it renders and
an import is governed by `script-src`, which names origins and not routes. An
edge can put `/g/` on the node while the rest of the page stays where it is,
and the module is then same-origin with the page that imports it.

`/g/` is `of ANYONE`. A glyph module is UI, and UI is not a boundary — every
call it makes is gated against whoever made it. What is served is what was
published; a glyph written and not yet public is an attestation this route does
not read.

## What a module exports

Two things:

```js
export const glyphDef = {
  symbol: '📯',              // what it is drawn as
  title: 'CRIER',
  label: 'crier',
  manifestation: 'panel',   // omit for the canvas; 'panel' puts it in the tray
  defaultWidth: 960,
  defaultHeight: 640,
}

export const render = (glyph, ui) => {
  const { element, content } = ui.glyph({
    defaults: { x: glyph.x ?? 200, y: glyph.y ?? 200, width: 960, height: 640 },
    titleBar: { label: glyphDef.title },
    resizable: true,
  })

  content.textContent = 'hello'
  return element
}
```

`ui.glyph()` builds the frame: position, drag, resize, the title bar, and the ⬆
that lifts the glyph off the canvas into a window and puts it back. A module
that returns a bare element instead gets that frame wrapped around it.

What else `ui` carries: `log`, `onCleanup`, `attestations(query)`,
`pluginFetch`, `pluginWebSocket`, `onMeld`, `loadConfig`/`saveConfig`,
`input`/`button`/`statusLine`, `spawnResult`. Register every timer, socket and
subscription through `onCleanup` — it runs when the glyph closes and again
before a republished module draws in its place. Without it, publishing twice
leaves the first one still running underneath the second.

## Building one

One ES module, no import map, nothing fetched at runtime from another origin.
Bundle whatever it needs into the file. CRIER's build is an example: Svelte
compiled and bundled by `bun build` into a single `dist/glyph-module.js`, which
is the file handed to `--file`.

Nothing about the build is prescribed. The node stores a string and serves it
with `Content-Type: text/javascript`; what produced that string is yours.

## What happens when you publish

1. The attestation lands.
2. `standing-glyph-published` matches on the predicate. It is a watcher every
   node is born with, held in no store, and no route can delete or disable it.
   It runs nothing — its action type is `tell`, which reaches browsers and no
   code anywhere.
3. The node addresses that wake to the namespace the attestation was written
   in, and to nobody else.
4. Pages in that namespace re-read `/g/`, see an attestation id they are not
   holding, and import the new module.
5. What is already drawn is redrawn: a canvas glyph in place, keeping its
   position and content; an open tray panel from the new module, after the old
   one's cleanups have run.

## Reaching a node

`--to` names the node. Without it, the one this machine runs, on the port
`am.toml` gives.

The token is `--token`, then `$QNTX_TOKEN`, then `~/.qntx/token`. A refusal
names which of those it used, so a 403 tells you which credential was refused
rather than that something was.

A glyph is published into the namespace the caller acts in, and served to that
namespace alone. Namespaces are their own universes and nothing crosses
(ADR-026) — a glyph published in one is not on another's canvas.

## When it does not appear

The glyph draws its own absence. Where it should be, you get a frame that says
what the node answered: published and not yet loaded, published and failed to
load with the reason, or nothing published under that name. Nobody should have
to open a console to find out why a glyph is missing.

`/g/` refuses in ways that survive an edge: `410` when nothing is published
under that name, `422` when the ask is malformed. Neither is in CloudFront's
rewritable set, so a refusal cannot come back as an HTML page that fails as a
parse error naming nothing.

## Related

- [ADR-026](../adr/ADR-026-namespaces.md) — namespaces are their own universes
- `server/glyph_module_handlers.go` — what `/g/` serves
- `ats/watcher/standing.go` — the watcher every node is born with
