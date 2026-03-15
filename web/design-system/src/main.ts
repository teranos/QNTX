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

function renderToken(token: Token): HTMLElement {
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
  renderButtonGallery(root)
}

// --- Button gallery ---

interface ButtonSpec {
  label: string
  classes: string
  disabled?: boolean
}

function renderButtonGallery(root: HTMLElement) {
  const section = document.createElement('section')
  section.className = 'token-group'

  const h2 = document.createElement('h2')
  h2.textContent = 'Components'
  section.appendChild(h2)

  // ── SDK Primitives ──
  const sdkHeader = document.createElement('h3')
  sdkHeader.className = 'component-section-header'
  sdkHeader.textContent = 'SDK Primitives'
  const sdkDesc = document.createElement('p')
  sdkDesc.className = 'component-section-desc'
  sdkDesc.textContent = 'What plugins get through ui.* — the canonical components for plugin-authored glyphs'
  section.appendChild(sdkHeader)
  section.appendChild(sdkDesc)

  // glyph-btn (SDK: ui.button())
  const glyphBtnMatrix = buttonMatrix('ui.button()', 'glyph-btn — default and primary variants', ['default', 'primary'], [
    {
      rowLabel: '',
      cells: [
        { label: 'Cancel', classes: 'glyph-btn' },
        { label: 'Execute', classes: 'glyph-btn glyph-btn--primary' },
      ]
    },
  ])
  section.appendChild(glyphBtnMatrix)

  // ui.input() — with and without label
  const inputGroup = document.createElement('div')
  inputGroup.className = 'button-group'

  const inputHeader = document.createElement('div')
  inputHeader.className = 'button-group-header'
  const inputName = document.createElement('span')
  inputName.className = 'button-group-name'
  inputName.textContent = 'ui.input()'
  const inputDesc = document.createElement('span')
  inputDesc.className = 'button-group-desc'
  inputDesc.textContent = 'glyph-input — text input with drag protection'
  inputHeader.appendChild(inputName)
  inputHeader.appendChild(inputDesc)
  inputGroup.appendChild(inputHeader)

  const inputRow = document.createElement('div')
  inputRow.className = 'sdk-specimen-row'

  // Without label
  const input1 = document.createElement('div')
  input1.className = 'glyph-form-group'
  const inp1 = document.createElement('input')
  inp1.className = 'glyph-input'
  inp1.type = 'text'
  inp1.placeholder = 'Enter URL...'
  input1.appendChild(inp1)
  inputRow.appendChild(input1)

  // With label
  const input2 = document.createElement('div')
  input2.className = 'glyph-form-group'
  const lbl = document.createElement('label')
  lbl.className = 'glyph-label'
  lbl.textContent = 'API Endpoint'
  input2.appendChild(lbl)
  const inp2 = document.createElement('input')
  inp2.className = 'glyph-input'
  inp2.type = 'text'
  inp2.placeholder = 'https://...'
  inp2.value = 'https://api.example.com/v1'
  input2.appendChild(inp2)
  inputRow.appendChild(input2)

  inputGroup.appendChild(inputRow)
  section.appendChild(inputGroup)

  // ── Internal Systems ──
  const internalHeader = document.createElement('h3')
  internalHeader.className = 'component-section-header'
  internalHeader.textContent = 'Internal Systems'
  const internalDesc = document.createElement('p')
  internalDesc.className = 'component-section-desc'
  internalDesc.textContent = 'Used by QNTX core — not exposed to plugins'
  section.appendChild(internalHeader)
  section.appendChild(internalDesc)

  // qntx-btn: one matrix — variants + states as rows, sizes as columns
  const variants = ['default', 'primary', 'secondary', 'danger', 'warning', 'ghost']
  const sizes = ['small', 'medium', 'large']

  const variantRows: MatrixRow[] = variants.map(v => ({
    rowLabel: v,
    cells: sizes.map(s => ({
      label: v,
      classes: `qntx-btn qntx-btn-${s} qntx-btn-${v}`,
    }))
  }))

  const stateRows: MatrixRow[] = [
    {
      rowLabel: 'disabled',
      cells: sizes.map(s => ({
        label: 'disabled',
        classes: `qntx-btn qntx-btn-${s} qntx-btn-default`,
        disabled: true,
      }))
    },
    {
      rowLabel: 'confirming',
      cells: sizes.map(s => ({
        label: 'confirming',
        classes: `qntx-btn qntx-btn-${s} qntx-btn-danger qntx-btn-confirming`,
      }))
    },
    {
      rowLabel: 'loading',
      cells: sizes.map(s => ({
        label: 'loading',
        classes: `qntx-btn qntx-btn-${s} qntx-btn-default qntx-btn-loading`,
      }))
    },
    {
      rowLabel: 'error',
      cells: sizes.map(s => ({
        label: 'error',
        classes: `qntx-btn qntx-btn-${s} qntx-btn-error`,
      }))
    },
  ]

  const qntxMatrix = buttonMatrix('qntx-btn', 'Primary button system — variants, states, sizes', sizes, [...variantRows, ...stateRows])
  section.appendChild(qntxMatrix)

  // Interactive two-stage confirmation demo
  const confirmGroup = document.createElement('div')
  confirmGroup.className = 'button-group'

  const confirmHeader = document.createElement('div')
  confirmHeader.className = 'button-group-header'
  const confirmName = document.createElement('span')
  confirmName.className = 'button-group-name'
  confirmName.textContent = 'Two-stage confirmation'
  const confirmDesc = document.createElement('span')
  confirmDesc.className = 'button-group-desc'
  confirmDesc.textContent = 'First click enters confirming state, second click executes. Auto-reverts after 3s.'
  confirmHeader.appendChild(confirmName)
  confirmHeader.appendChild(confirmDesc)
  confirmGroup.appendChild(confirmHeader)

  const confirmRow = document.createElement('div')
  confirmRow.className = 'sdk-specimen-row'

  for (const variant of ['danger', 'warning', 'ghost'] as const) {
    const wrapper = document.createElement('div')
    wrapper.className = 'two-stage-demo'

    const btn = document.createElement('button')
    btn.className = `qntx-btn qntx-btn-medium qntx-btn-${variant}`
    btn.style.minWidth = '120px'

    const label = document.createElement('span')
    label.className = 'qntx-btn-label'
    label.textContent = variant === 'danger' ? 'Delete' : variant === 'warning' ? 'Reset' : 'Clear'
    btn.appendChild(label)

    const originalText = label.textContent
    const confirmText = 'Are you sure?'
    let confirming = false
    let timeout: ReturnType<typeof setTimeout> | null = null

    btn.addEventListener('click', () => {
      if (!confirming) {
        confirming = true
        label.textContent = confirmText
        btn.classList.add('qntx-btn-confirming')
        timeout = setTimeout(() => {
          confirming = false
          label.textContent = originalText
          btn.classList.remove('qntx-btn-confirming')
        }, 3000)
      } else {
        confirming = false
        if (timeout) clearTimeout(timeout)
        btn.classList.remove('qntx-btn-confirming')
        label.textContent = 'Done!'
        btn.classList.add('qntx-btn-loading')
        setTimeout(() => {
          label.textContent = originalText
          btn.classList.remove('qntx-btn-loading')
        }, 1000)
      }
    })

    const variantLabel = document.createElement('span')
    variantLabel.className = 'button-class-label'
    variantLabel.textContent = `qntx-btn-${variant}`
    variantLabel.style.marginTop = '4px'
    variantLabel.style.display = 'block'
    variantLabel.style.fontSize = '9px'
    variantLabel.style.color = 'var(--text-on-dark-tertiary)'

    wrapper.appendChild(btn)
    wrapper.appendChild(variantLabel)
    confirmRow.appendChild(wrapper)
  }

  confirmGroup.appendChild(confirmRow)
  section.appendChild(confirmGroup)

  // titlebar — rendered as live title bar strips
  const titlebarSection = document.createElement('div')
  titlebarSection.className = 'button-group'

  const tbHeader = document.createElement('div')
  tbHeader.className = 'button-group-header'
  const tbName = document.createElement('span')
  tbName.className = 'button-group-name'
  tbName.textContent = 'glyph-title-bar'
  const tbDesc = document.createElement('span')
  tbDesc.className = 'button-group-desc'
  tbDesc.textContent = 'Unified title bar for all glyph manifestations'
  tbHeader.appendChild(tbName)
  tbHeader.appendChild(tbDesc)
  titlebarSection.appendChild(tbHeader)

  const tbRow = document.createElement('div')
  tbRow.className = 'titlebar-row'

  tbRow.appendChild(titleBarStrip('Standard', 'glyph-title-bar', 'ix-prompt', [
    { label: '▶', cls: 'titlebar-btn' },
    { label: '✕', cls: 'titlebar-btn' },
  ]))

  tbRow.appendChild(titleBarStrip('Generic buttons', 'glyph-title-bar', 'result-glyph', [
    { label: '⊞', cls: '' },
    { label: '✕', cls: '' },
  ]))

  const panelWrap = document.createElement('div')
  panelWrap.className = 'glyph-panel titlebar-specimen'
  const panelLabel = document.createElement('span')
  panelLabel.className = 'titlebar-specimen-label'
  panelLabel.textContent = 'Panel (no drag cursor)'
  panelWrap.appendChild(panelLabel)
  const panelBar = document.createElement('div')
  panelBar.className = 'glyph-title-bar'
  const panelTitle = document.createElement('span')
  panelTitle.textContent = 'plugin-config'
  panelTitle.style.flex = '1'
  panelBar.appendChild(panelTitle)
  const panelBtn = document.createElement('button')
  panelBtn.className = 'titlebar-btn'
  panelBtn.textContent = '✕'
  panelBar.appendChild(panelBtn)
  panelWrap.appendChild(panelBar)
  tbRow.appendChild(panelWrap)

  titlebarSection.appendChild(tbRow)

  // Auto-height on its own (wider to show wrapping)
  titlebarSection.appendChild(titleBarStrip('Auto-height (--auto)', 'glyph-title-bar glyph-title-bar--auto', 'attestation with a longer title that wraps to demonstrate auto-height behavior', [
    { label: '⟳', cls: 'titlebar-btn' },
    { label: '✕', cls: 'titlebar-btn' },
  ]))

  section.appendChild(titlebarSection)

  root.appendChild(section)
}

interface MatrixRow {
  rowLabel: string
  cells: ButtonSpec[]
}

function buttonMatrix(name: string, description: string, columnLabels: string[], rows: MatrixRow[]): HTMLElement {
  const container = document.createElement('div')
  container.className = 'button-group'

  const header = document.createElement('div')
  header.className = 'button-group-header'

  const nameEl = document.createElement('span')
  nameEl.className = 'button-group-name'
  nameEl.textContent = name

  const descEl = document.createElement('span')
  descEl.className = 'button-group-desc'
  descEl.textContent = description

  header.appendChild(nameEl)
  header.appendChild(descEl)
  container.appendChild(header)

  const table = document.createElement('div')
  table.className = 'button-matrix'
  const hasRowLabels = rows.some(r => r.rowLabel)
  table.style.gridTemplateColumns = hasRowLabels
    ? `80px repeat(${columnLabels.length}, 1fr)`
    : `repeat(${columnLabels.length}, 1fr)`

  // Column headers
  if (hasRowLabels) {
    const corner = document.createElement('div')
    corner.className = 'button-matrix-header'
    table.appendChild(corner)
  }
  for (const col of columnLabels) {
    const colHeader = document.createElement('div')
    colHeader.className = 'button-matrix-header'
    colHeader.textContent = col
    table.appendChild(colHeader)
  }

  // Rows
  for (const row of rows) {
    if (hasRowLabels) {
      const rowLabel = document.createElement('div')
      rowLabel.className = 'button-matrix-rowlabel'
      rowLabel.textContent = row.rowLabel
      table.appendChild(rowLabel)
    }

    for (const spec of row.cells) {
      const cell = document.createElement('div')
      cell.className = 'button-matrix-cell'

      const btn = document.createElement('button')
      btn.className = spec.classes
      if (spec.disabled) btn.disabled = true

      if (spec.classes.includes('qntx-btn-loading')) {
        const spinner = document.createElement('span')
        spinner.className = 'qntx-btn-spinner'
        btn.appendChild(spinner)
      }

      const label = document.createElement('span')
      label.className = 'qntx-btn-label'
      label.textContent = spec.label
      btn.appendChild(label)

      cell.appendChild(btn)

      // Click cell to copy full class string
      cell.title = spec.classes
      cell.addEventListener('click', (e) => {
        if (e.target === btn || btn.contains(e.target as Node)) return
        navigator.clipboard.writeText(spec.classes)
      })

      table.appendChild(cell)
    }
  }

  container.appendChild(table)
  return container
}

function titleBarStrip(label: string, barClasses: string, title: string, buttons: {label: string, cls: string}[]): HTMLElement {
  const row = document.createElement('div')
  row.className = 'titlebar-specimen'

  const desc = document.createElement('span')
  desc.className = 'titlebar-specimen-label'
  desc.textContent = label
  row.appendChild(desc)

  const bar = document.createElement('div')
  bar.className = barClasses

  const titleSpan = document.createElement('span')
  titleSpan.textContent = title
  titleSpan.style.flex = '1'
  bar.appendChild(titleSpan)

  for (const b of buttons) {
    const btn = document.createElement('button')
    if (b.cls) btn.className = b.cls
    btn.textContent = b.label
    bar.appendChild(btn)
  }

  row.appendChild(bar)
  return row
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
      gap: var(--gap);
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

    /* Demo cards (font-size, etc.) */
    .token-card--demo {
      flex-direction: column;
    }

    .token-demo {
      padding: var(--panel-padding-sm);
      min-height: 40px;
      display: flex;
      align-items: center;
    }

    .token-font-sample {
      color: var(--text-on-dark);
      overflow-wrap: break-word;
      word-break: break-word;
    }

    .token-spacing-bar {
      height: 16px;
      min-width: 4px;
      background: var(--accent-on-dark);
      opacity: 0.5;
    }

    .token-padding-box {
      background: var(--bg-tertiary);
      border: 1px dashed var(--border-on-dark);
      color: var(--text-on-dark-tertiary);
      font-size: 10px;
    }

    .token-demo--row {
      gap: var(--gap);
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
      width: 80px;
      height: 48px;
      background: var(--bg-tertiary);
      border: var(--panel-border);
      border-radius: var(--border-radius);
      transition: var(--panel-transition);
    }

    .token-shadow-box:hover {
      background: var(--bg-dark-hover);
    }

    .token-transition-box {
      width: 48px;
      height: 32px;
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
      width: 80px;
      height: 40px;
      background: var(--bg-secondary);
      border-radius: var(--border-radius);
    }

    /* Component section headers */
    .component-section-header {
      font-size: var(--font-size-sm);
      font-weight: 500;
      color: var(--text-on-dark);
      margin-top: 16px;
      margin-bottom: 2px;
    }

    .component-section-desc {
      font-size: 10px;
      color: var(--text-on-dark-tertiary);
      margin-bottom: 12px;
    }

    /* SDK specimen row */
    .sdk-specimen-row {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 12px;
      padding: var(--panel-padding-sm);
      background: var(--bg-secondary);
      border: var(--panel-border);
      border-radius: var(--border-radius);
    }

    /* Button gallery */
    .button-group {
      margin-bottom: 16px;
    }

    .button-group-header {
      display: flex;
      align-items: baseline;
      gap: 8px;
      margin-bottom: 8px;
    }

    .button-group-name {
      color: var(--accent-on-dark);
      font-size: var(--font-size-sm);
      font-weight: 500;
    }

    .button-group-desc {
      color: var(--text-on-dark-tertiary);
      font-size: 10px;
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
      padding: 4px 8px;
      font-size: 10px;
      color: var(--text-on-dark-tertiary);
      text-align: center;
    }

    .button-matrix-rowlabel {
      background: var(--bg-secondary);
      padding: 8px;
      font-size: 10px;
      color: var(--accent-on-dark);
      display: flex;
      align-items: center;
    }

    .button-matrix-cell {
      background: var(--bg-secondary);
      padding: 8px;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    /* Title bar specimens */
    .titlebar-row {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 12px;
      margin-bottom: 8px;
    }

    .titlebar-specimen {
      margin-bottom: 0;
    }

    .titlebar-specimen-label {
      display: block;
      font-size: 10px;
      color: var(--text-on-dark-tertiary);
      margin-bottom: 4px;
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
