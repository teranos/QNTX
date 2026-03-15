/**
 * Design token viewer
 *
 * Fetches tokens.css, parses custom property declarations,
 * groups by comment headings, renders visual swatches.
 *
 * TODO: Non-color tokens need visual demonstrations, not just text values:
 *   - Border radius: render boxes with the actual radius applied
 *   - Spacing: render bars/gaps at the token's size
 *   - Shadows: render cards with each shadow applied
 *   - Transitions: render interactive elements that use the timing/easing
 *   - Font sizes: render sample text at each size
 * TODO: Render a sample QNTX glyph (title bar, body, status colors)
 *   that composes multiple tokens together — shows how they work as a system
 * TODO: Could become a canvas panel / glyph inside QNTX itself
 */

interface Token {
  name: string      // e.g. --bg-canvas
  value: string     // e.g. #2d2e36
  comment?: string  // inline comment
}

interface TokenGroup {
  heading: string
  tokens: Token[]
}

// --- Parse tokens.css into groups ---

function parseTokens(css: string): TokenGroup[] {
  const groups: TokenGroup[] = []
  let current: TokenGroup = { heading: 'Ungrouped', tokens: [] }
  const lines = css.split('\n')

  for (const line of lines) {
    const trimmed = line.trim()

    // Comment headings like /* Font Stacks */ or /* Core Colors */
    if (trimmed.startsWith('/*') && trimmed.endsWith('*/') && !trimmed.includes(':')) {
      const heading = trimmed.substring(2, trimmed.length - 2).trim()
      // Skip the file-level comment
      if (heading.startsWith('Design Tokens')) continue
      // Skip directives like "No selectors..."
      if (heading.startsWith('No ')) continue
      if (current.tokens.length > 0) groups.push(current)
      current = { heading, tokens: [] }
      continue
    }

    // Variable declaration: --name: value;
    const dashIdx = trimmed.indexOf('--')
    if (dashIdx < 0) continue
    const colonIdx = trimmed.indexOf(':', dashIdx)
    if (colonIdx < 0) continue

    const name = trimmed.substring(dashIdx, colonIdx).trim()
    let rest = trimmed.substring(colonIdx + 1).trim()

    // Strip trailing semicolon
    if (rest.endsWith(';')) rest = rest.substring(0, rest.length - 1).trim()

    // Extract inline comment
    let comment: string | undefined
    const commentIdx = rest.indexOf('/*')
    if (commentIdx >= 0) {
      const endIdx = rest.indexOf('*/', commentIdx)
      if (endIdx >= 0) {
        comment = rest.substring(commentIdx + 2, endIdx).trim()
        rest = rest.substring(0, commentIdx).trim()
      }
    }

    current.tokens.push({ name, value: rest, comment })
  }

  if (current.tokens.length > 0) groups.push(current)
  return groups
}

// --- Detect if a value is a color ---

function isColor(value: string): boolean {
  if (value.startsWith('#')) return true
  if (value.startsWith('rgb')) return true
  if (value.startsWith('hsl')) return true
  return false
}

function isVarRef(value: string): boolean {
  return value.startsWith('var(')
}

// --- Resolve computed value of a CSS variable ---

function getComputed(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

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
      const card = document.createElement('div')
      card.className = 'token-card'

      const computed = getComputed(token.name)
      const displayValue = token.value
      const showSwatch = isColor(token.value) || isColor(computed) || isVarRef(token.value)

      if (showSwatch) {
        const swatch = document.createElement('div')
        swatch.className = 'token-swatch'
        swatch.style.background = `var(${token.name})`

        // For light colors, add a border so they're visible
        if (computed && isLightColor(computed)) {
          swatch.style.border = '1px solid var(--border-on-dark)'
        }

        card.appendChild(swatch)
      }

      const info = document.createElement('div')
      info.className = 'token-info'

      const nameEl = document.createElement('span')
      nameEl.className = 'token-name'
      nameEl.textContent = token.name
      nameEl.title = 'Click to copy'
      nameEl.addEventListener('click', () => {
        navigator.clipboard.writeText(`var(${token.name})`)
        nameEl.textContent = 'copied!'
        setTimeout(() => { nameEl.textContent = token.name }, 800)
      })
      info.appendChild(nameEl)

      const valueEl = document.createElement('span')
      valueEl.className = 'token-value'
      valueEl.textContent = displayValue
      info.appendChild(valueEl)

      // Show resolved value if it's a var() reference
      if (isVarRef(token.value) && computed) {
        const resolved = document.createElement('span')
        resolved.className = 'token-resolved'
        resolved.textContent = computed
        info.appendChild(resolved)
      }

      if (token.comment) {
        const commentEl = document.createElement('span')
        commentEl.className = 'token-comment'
        commentEl.textContent = token.comment
        info.appendChild(commentEl)
      }

      card.appendChild(info)
      grid.appendChild(card)
    }

    section.appendChild(grid)
    root.appendChild(section)
  }
}

function isLightColor(hex: string): boolean {
  if (!hex.startsWith('#')) return false
  const clean = hex.length === 4
    ? hex[1] + hex[1] + hex[2] + hex[2] + hex[3] + hex[3]
    : hex.substring(1)
  const r = parseInt(clean.substring(0, 2), 16)
  const g = parseInt(clean.substring(2, 4), 16)
  const b = parseInt(clean.substring(4, 6), 16)
  return (r * 299 + g * 587 + b * 114) / 1000 > 200
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
      font-size: 12px;
      line-height: 1.5;
      -webkit-font-smoothing: antialiased;
    }

    #root {
      max-width: 1200px;
      margin: 0 auto;
      padding: 20px;
    }

    header {
      margin-bottom: 24px;
      padding-bottom: 12px;
      border-bottom: 1px solid var(--border-on-dark);
    }

    header h1 {
      color: var(--accent-on-dark);
      font-size: 16px;
      font-weight: 500;
    }

    header p {
      color: var(--text-on-dark-tertiary);
      font-size: 11px;
      margin-top: 4px;
    }

    .token-group {
      margin-bottom: 28px;
    }

    .token-group h2 {
      font-size: 13px;
      font-weight: 500;
      color: var(--text-on-dark-secondary);
      margin-bottom: 8px;
      padding-bottom: 4px;
      border-bottom: 1px solid var(--border-on-dark);
    }

    .token-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
      gap: 4px;
    }

    .token-card {
      display: flex;
      align-items: stretch;
      background: var(--bg-secondary);
      border: 1px solid var(--border-on-dark);
      overflow: hidden;
    }

    .token-swatch {
      width: 48px;
      min-height: 40px;
      flex-shrink: 0;
    }

    .token-info {
      padding: 4px 8px;
      display: flex;
      flex-direction: column;
      gap: 1px;
      min-width: 0;
    }

    .token-name {
      color: var(--accent-on-dark);
      font-size: 11px;
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
      font-size: 10px;
      overflow-wrap: break-word;
      word-break: break-word;
    }

    .token-resolved {
      color: var(--text-on-dark-tertiary);
      font-size: 10px;
    }

    .token-comment {
      color: var(--text-on-dark-tertiary);
      font-size: 10px;
      font-style: italic;
      overflow-wrap: break-word;
      word-break: break-word;
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
