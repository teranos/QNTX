/**
 * Token parsing and rendering.
 *
 * Parses tokens.css into groups, renders type-specific visual
 * demonstrations (colors, fonts, spacing, borders, shadows, transitions).
 */

export interface Token {
  name: string      // e.g. --bg-canvas
  value: string     // e.g. #2d2e36
  comment?: string  // inline comment
}

export interface TokenGroup {
  heading: string
  tokens: Token[]
}

// --- Parse tokens.css into groups ---

export function parseTokens(css: string): TokenGroup[] {
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

// --- Shared label for all token cards ---

function tokenLabel(token: Token): HTMLElement {
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
  valueEl.textContent = token.value
  info.appendChild(valueEl)

  if (isVarRef(token.value)) {
    const computed = getComputed(token.name)
    if (computed) {
      const resolved = document.createElement('span')
      resolved.className = 'token-resolved'
      resolved.textContent = computed
      info.appendChild(resolved)
    }
  }

  if (token.comment) {
    const commentEl = document.createElement('span')
    commentEl.className = 'token-comment'
    commentEl.textContent = token.comment
    info.appendChild(commentEl)
  }

  return info
}

// --- Type-specific renderers ---

export function renderToken(token: Token): HTMLElement {
  // Shadow tokens: show a card with the shadow applied
  if (token.name.includes('shadow')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo token-demo--center'
    const box = document.createElement('div')
    box.className = 'token-shadow-box'
    box.style.boxShadow = `var(${token.name})`
    demo.appendChild(box)

    card.appendChild(demo)
    card.appendChild(tokenLabel(token))
    return card
  }

  // Border radius tokens: show boxes with the radius applied
  if (token.name.includes('border-radius')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo token-demo--row'
    for (const size of [24, 40, 64]) {
      const box = document.createElement('div')
      box.className = 'token-radius-box'
      box.style.width = size + 'px'
      box.style.height = size + 'px'
      box.style.borderRadius = `var(${token.name})`
      demo.appendChild(box)
    }

    card.appendChild(demo)
    card.appendChild(tokenLabel(token))
    return card
  }

  // Border line tokens: show a box with the border applied
  if (token.name.includes('border') && !token.name.includes('radius') && token.value.includes('solid')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo token-demo--center'
    const box = document.createElement('div')
    box.className = 'token-border-box'
    box.style.border = `var(${token.name})`
    demo.appendChild(box)

    card.appendChild(demo)
    card.appendChild(tokenLabel(token))
    return card
  }

  // Spacing tokens: show as visual bars or padded boxes
  if (token.name.includes('gap') || token.name.includes('padding')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo'

    if (token.name.includes('padding')) {
      // Padding: show a box with the padding applied
      const box = document.createElement('div')
      box.className = 'token-padding-box'
      box.style.padding = `var(${token.name})`
      box.textContent = 'content'
      demo.appendChild(box)
    } else {
      // Gap: show a filled bar at the token's width
      const bar = document.createElement('div')
      bar.className = 'token-spacing-bar'
      bar.style.width = `var(${token.name})`
      demo.appendChild(bar)
    }

    card.appendChild(demo)
    card.appendChild(tokenLabel(token))
    return card
  }

  // Transition tokens: interactive box that demonstrates the timing
  if (token.name.includes('transition')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo token-demo--center'
    const box = document.createElement('div')
    box.className = 'token-transition-box'
    box.style.transition = `var(${token.name})`
    box.title = 'Hover to see transition'
    demo.appendChild(box)

    card.appendChild(demo)
    card.appendChild(tokenLabel(token))
    return card
  }

  // Font stack tokens: show character specimen at the font family
  if (token.name.includes('font-') && !token.name.includes('font-size')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo'
    const sample = document.createElement('span')
    sample.className = 'token-font-sample'
    sample.style.fontFamily = `var(${token.name})`
    sample.style.fontSize = '13px'
    sample.textContent = 'ABCDEFGHIJKLM 0123456789 {}[]()<>'
    demo.appendChild(sample)
    card.appendChild(demo)

    card.appendChild(tokenLabel(token))
    return card
  }

  // Font size tokens: show sample text at the actual size
  if (token.name.includes('font-size')) {
    const card = document.createElement('div')
    card.className = 'token-card token-card--demo'

    const demo = document.createElement('div')
    demo.className = 'token-demo'
    const sample = document.createElement('span')
    sample.className = 'token-font-sample'
    sample.style.fontSize = `var(${token.name})`
    sample.textContent = 'The quick brown fox jumps over the lazy dog'
    demo.appendChild(sample)
    card.appendChild(demo)

    card.appendChild(tokenLabel(token))
    return card
  }

  // Default: color swatch + label
  const card = document.createElement('div')
  card.className = 'token-card'

  const computed = getComputed(token.name)
  const showSwatch = isColor(token.value) || isColor(computed) || isVarRef(token.value)

  if (showSwatch) {
    const swatch = document.createElement('div')
    swatch.className = 'token-swatch'
    swatch.style.background = `var(${token.name})`
    if (computed && isLightColor(computed)) {
      swatch.style.border = '1px solid var(--border-on-dark)'
    }
    card.appendChild(swatch)
  }

  card.appendChild(tokenLabel(token))
  return card
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
