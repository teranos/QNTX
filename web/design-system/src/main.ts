/**
 * Design system viewer — boot file.
 *
 * Injects styles, fetches tokens.css, renders token groups
 * and component gallery.
 *
 * TODO: Render a sample QNTX glyph (title bar, body, status colors)
 *   that composes multiple tokens together — shows how they work as a system
 * TODO: Could become a canvas panel / glyph inside QNTX itself
 */

import { parseTokens, renderToken, type TokenGroup } from './tokens'
import { renderComponentGallery } from './components'

// --- Render ---

function render(groups: TokenGroup[]) {
  const root = document.getElementById('root')!

  // Page header
  const header = document.createElement('header')
  header.innerHTML = `<h1>QNTX Design Tokens</h1><p>${groups.reduce((n, g) => n + g.tokens.length, 0)} tokens across ${groups.length} groups</p>`
  root.appendChild(header)

  for (const group of groups) {
    const section = document.createElement('section')
    section.className = 'token-group'

    const h2 = document.createElement('h2')
    h2.textContent = group.heading
    section.appendChild(h2)

    const grid = document.createElement('div')
    grid.className = 'token-grid'

    for (const token of group.tokens) {
      grid.appendChild(renderToken(token))
    }

    section.appendChild(grid)
    root.appendChild(section)
  }

  // Component galleries — simpler first, then more complex
  renderComponentGallery(root)
}

// --- Styles ---

function injectStyles() {
  const style = document.createElement('style')
  style.textContent = `
    * { box-sizing: border-box; margin: 0; padding: 0; }

    body {
      background: var(--bg-canvas);
      color: var(--text-on-dark);
      font-family: var(--font-mono);
      font-size: var(--font-size-sm);
      line-height: 1.4;
      -webkit-font-smoothing: antialiased;
    }

    #root {
      max-width: 1400px;
      margin: 0 auto;
      padding: 12px 16px;
    }

    header {
      margin-bottom: 12px;
      padding-bottom: 6px;
      border-bottom: 1px solid var(--border-on-dark);
      display: flex;
      align-items: baseline;
      gap: 12px;
    }

    header h1 {
      color: var(--accent-on-dark);
      font-size: var(--font-size-md);
      font-weight: 500;
    }

    header p {
      color: var(--text-on-dark-tertiary);
      font-size: var(--font-size-xs);
    }

    .token-group {
      margin-bottom: 14px;
    }

    .token-group h2 {
      font-size: var(--font-size-sm);
      font-weight: 500;
      color: var(--text-on-dark-secondary);
      margin-bottom: 4px;
      padding-bottom: 2px;
      border-bottom: 1px solid var(--border-on-dark);
    }

    .token-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
      gap: 3px;
    }

    .token-card {
      display: flex;
      align-items: stretch;
      background: var(--bg-secondary);
      border: var(--panel-border);
      border-radius: var(--border-radius);
      overflow: hidden;
    }

    .token-swatch {
      width: 36px;
      min-height: 28px;
      flex-shrink: 0;
    }

    .token-info {
      padding: 2px 6px;
      display: flex;
      flex-direction: column;
      gap: 0;
      min-width: 0;
    }

    .token-name {
      color: var(--accent-on-dark);
      font-size: var(--font-size-xs);
      font-weight: 500;
      cursor: pointer;
      overflow-wrap: break-word;
      word-break: break-word;
    }
    .token-name:hover {
      color: var(--accent-on-dark-hover);
    }

    .token-value {
      color: var(--text-on-dark);
      font-size: var(--font-size-xs);
      overflow-wrap: break-word;
      word-break: break-word;
    }

    .token-resolved {
      color: var(--text-on-dark-tertiary);
      font-size: var(--font-size-xs);
    }

    .token-comment {
      color: var(--text-on-dark-tertiary);
      font-size: var(--font-size-xs);
      font-style: italic;
      overflow-wrap: break-word;
      word-break: break-word;
    }

    /* Demo cards (font-size, etc.) */
    .token-card--demo {
      flex-direction: column;
    }

    .token-demo {
      padding: 4px 8px;
      min-height: 28px;
      display: flex;
      align-items: center;
    }

    .token-font-sample {
      color: var(--text-on-dark);
      overflow-wrap: break-word;
      word-break: break-word;
    }

    .token-spacing-bar {
      height: 12px;
      min-width: 4px;
      background: var(--accent-on-dark);
      opacity: 0.5;
    }

    .token-padding-box {
      background: var(--bg-tertiary);
      border: 1px dashed var(--border-on-dark);
      color: var(--text-on-dark-tertiary);
      font-size: var(--font-size-xs);
    }

    .token-demo--row {
      gap: 4px;
    }

    .token-demo--center {
      justify-content: center;
    }

    .token-radius-box {
      background: var(--accent-on-dark);
      flex-shrink: 0;
      border: var(--panel-border);
    }

    .token-shadow-box {
      width: 60px;
      height: 36px;
      background: var(--bg-tertiary);
      border: var(--panel-border);
      border-radius: var(--border-radius);
      transition: var(--panel-transition);
    }

    .token-shadow-box:hover {
      background: var(--bg-dark-hover);
    }

    .token-transition-box {
      width: 36px;
      height: 24px;
      background: var(--bg-tertiary);
      border: var(--panel-border);
      border-radius: var(--border-radius);
      cursor: pointer;
    }

    .token-transition-box:hover {
      background: var(--accent-on-dark);
      border-color: var(--accent-on-dark);
    }

    .token-border-box {
      width: 60px;
      height: 28px;
      background: var(--bg-secondary);
      border-radius: var(--border-radius);
    }

    /* Glyph specimen row — side-by-side mini glyphs */
    .glyph-specimen-row {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 8px;
      margin-bottom: 10px;
    }

    /* SDK specimen row */
    .sdk-specimen-row {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 8px;
      padding: 6px 8px;
      background: var(--bg-secondary);
      border: var(--panel-border);
      border-radius: var(--border-radius);
    }

    .button-matrix {
      display: grid;
      gap: 1px;
      background: var(--border-on-dark);
      border: var(--panel-border);
      border-radius: var(--border-radius);
      overflow: hidden;
    }

    .button-matrix-header {
      background: var(--bg-tertiary);
      padding: 2px 6px;
      font-size: var(--font-size-xs);
      color: var(--text-on-dark-tertiary);
      text-align: center;
    }

    .button-matrix-rowlabel {
      background: var(--bg-secondary);
      padding: 4px 6px;
      font-size: var(--font-size-xs);
      color: var(--accent-on-dark);
      display: flex;
      align-items: center;
    }

    .button-matrix-cell {
      background: var(--bg-secondary);
      padding: 4px 6px;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    /* Title bar specimens */
    .titlebar-row {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 8px;
      margin-bottom: 4px;
    }

    .titlebar-specimen {
      margin-bottom: 0;
    }

    .titlebar-specimen-label {
      display: block;
      font-size: var(--font-size-xs);
      color: var(--text-on-dark-tertiary);
      margin-bottom: 2px;
    }
  `
  document.head.appendChild(style)
}

// --- Boot ---

async function main() {
  injectStyles()

  try {
    const res = await fetch('/tokens.css')
    const css = await res.text()
    const groups = parseTokens(css)
    render(groups)
  } catch (e: any) {
    const root = document.getElementById('root')!
    root.textContent = 'Failed to load tokens.css: ' + (e.message || e)
  }
}

main()
