/**
 * Doc — an inline document viewer, published rather than built in.
 *
 * The first element to leave the shell. It was
 * web/ts/components/element/doc-element.ts, and what it needed from in there was
 * three things the ElementUI contract now carries: the frame (ui.element), what
 * the canvas persisted for it (ui.content), and a URL a browser will load
 * (ui.nodeUrl). Nothing else about it changed.
 *
 * Content is JSON: { fileId, filename, ext }. The file is served from
 * /api/files/{fileId}{ext}. A doc element is made by dropping a file on the
 * canvas, which is the canvas's job and not this module's — this draws what
 * was dropped.
 *
 * Known limitation, carried over: browsers render their own PDF toolbar inside
 * the <embed>, and there is no cross-browser way to suppress it. Chrome's
 * #toolbar=0 does nothing in Firefox.
 *
 * Publish with:
 *   qntx element publish doc --file elements/doc/element.js
 */

export const elementDef = {
  symbol: '▤',
  title: 'Document',
  label: 'Doc',
  defaultWidth: 400,
  defaultHeight: 500,
}

/** What the canvas persisted for this element, when it is a document reference. */
function documentIn(ui) {
  const held = ui.content()
  if (!held) return null

  try {
    const meta = JSON.parse(held)
    if (typeof meta?.fileId === 'string' && typeof meta?.ext === 'string') return meta
    ui.log.error(`content is JSON but names no file: ${held}`)
  } catch (err) {
    // The canvas holds a string this element wrote; unreadable means something
    // else wrote it, and saying so beats drawing an empty frame.
    ui.log.error('content is not the JSON this element writes:', err)
  }
  return null
}

export const render = (item, ui) => {
  const meta = documentIn(ui)

  const { element, content } = ui.element({
    // The class the stylesheet already knows: a flex column, which is what
    // lets the content area fill and the embed fill that. Named here because
    // the frame would otherwise call this element something new and the rules
    // written for it would not apply.
    className: 'canvas-doc-element',
    defaults: {
      x: item.x ?? 200,
      y: item.y ?? 200,
      width: item.width ?? elementDef.defaultWidth,
      height: item.height ?? elementDef.defaultHeight,
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
