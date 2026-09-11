/**
 * Doc — an inline document viewer, published rather than built in.
 *
 * The first glyph to leave the shell. It was
 * web/ts/components/glyph/doc-glyph.ts, and what it needed from in there was
 * three things the GlyphUI contract now carries: the frame (ui.glyph), what
 * the canvas persisted for it (ui.content), and a URL a browser will load
 * (ui.nodeUrl). Nothing else about it changed.
 *
 * Content is JSON: { fileId, filename, ext }. The file is served from
 * /api/files/{fileId}{ext}. A doc glyph is made by dropping a file on the
 * canvas, which is the canvas's job and not this module's — this draws what
 * was dropped.
 *
 * Known limitation, carried over: browsers render their own PDF toolbar inside
 * the <embed>, and there is no cross-browser way to suppress it. Chrome's
 * #toolbar=0 does nothing in Firefox.
 *
 * Publish with:
 *   qntx glyph publish doc --file glyphs/doc/glyph.js
 */

export const glyphDef = {
  symbol: '▤',
  title: 'Document',
  label: 'Doc',
  defaultWidth: 400,
  defaultHeight: 500,
}

/** What the canvas persisted for this glyph, when it is a document reference. */
function documentIn(ui) {
  const held = ui.content()
  if (!held) return null

  try {
    const meta = JSON.parse(held)
    if (typeof meta?.fileId === 'string' && typeof meta?.ext === 'string') return meta
    ui.log.error(`content is JSON but names no file: ${held}`)
  } catch (err) {
    // The canvas holds a string this glyph wrote; unreadable means something
    // else wrote it, and saying so beats drawing an empty frame.
    ui.log.error('content is not the JSON this glyph writes:', err)
  }
  return null
}

export const render = (glyph, ui) => {
  const meta = documentIn(ui)

  const { element, content } = ui.glyph({
    defaults: {
      x: glyph.x ?? 200,
      y: glyph.y ?? 200,
      width: glyph.width ?? glyphDef.defaultWidth,
      height: glyph.height ?? glyphDef.defaultHeight,
    },
    titleBar: { label: meta?.filename ?? 'Document' },
    resizable: { minWidth: 200, minHeight: 200 },
  })

  content.style.padding = '0'
  content.style.overflow = 'hidden'

  if (!meta) {
    const empty = document.createElement('div')
    empty.className = 'doc-placeholder'
    empty.textContent = 'No document loaded'
    content.appendChild(empty)
    return element
  }

  const embed = document.createElement('embed')
  embed.className = 'doc-embed'
  embed.src = ui.nodeUrl(`/api/files/${meta.fileId}${meta.ext}`)
  embed.type = 'application/pdf'
  content.appendChild(embed)

  ui.log.debug(`drawing ${meta.filename} from ${meta.fileId}${meta.ext}`)
  return element
}
