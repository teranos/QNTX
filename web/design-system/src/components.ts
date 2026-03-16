/**
 * Component gallery — live specimens of QNTX UI components.
 *
 * Split into SDK Primitives (what plugins get through ui.*)
 * and Internal Systems (used by QNTX core).
 */

interface ButtonSpec {
  label: string
  classes: string
  disabled?: boolean
}

interface MatrixRow {
  rowLabel: string
  cells: ButtonSpec[]
}

export function renderComponentGallery(root: HTMLElement) {
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

  // SDK glyph specimen — container + input + button + statusLine together
  const sdkGroup = document.createElement('div')
  sdkGroup.className = 'button-group'

  const glyphHeader = document.createElement('div')
  glyphHeader.className = 'button-group-header'
  const glyphName = document.createElement('span')
  glyphName.className = 'button-group-name'
  glyphName.textContent = 'Plugin glyph'
  const glyphDescEl = document.createElement('span')
  glyphDescEl.className = 'button-group-desc'
  glyphDescEl.textContent = 'ui.container() + ui.input() + ui.button() + ui.statusLine() — as used by ix-json'
  glyphHeader.appendChild(glyphName)
  glyphHeader.appendChild(glyphDescEl)
  sdkGroup.appendChild(glyphHeader)

  // Mini glyph: title bar + content area with input, button, status
  const glyphDemo = document.createElement('div')
  glyphDemo.style.display = 'flex'
  glyphDemo.style.flexDirection = 'column'
  glyphDemo.style.border = '1px solid var(--border-on-dark)'
  glyphDemo.style.borderRadius = 'var(--border-radius)'
  glyphDemo.style.height = '200px'
  glyphDemo.style.overflow = 'hidden'
  glyphDemo.style.background = 'var(--bg-secondary)'

  const demoTitleBar = document.createElement('div')
  demoTitleBar.className = 'glyph-title-bar'
  const demoLabel = document.createElement('span')
  demoLabel.textContent = 'ix-json'
  demoTitleBar.appendChild(demoLabel)
  const closeBtn = document.createElement('button')
  closeBtn.className = 'titlebar-btn'
  closeBtn.textContent = '\u2715'
  demoTitleBar.appendChild(closeBtn)
  glyphDemo.appendChild(demoTitleBar)

  const demoContent = document.createElement('div')
  demoContent.className = 'glyph-content-area'
  demoContent.style.display = 'flex'
  demoContent.style.flexDirection = 'column'
  demoContent.style.gap = '8px'

  const urlInput = document.createElement('div')
  urlInput.className = 'glyph-form-group'
  const urlLbl = document.createElement('label')
  urlLbl.className = 'glyph-label'
  urlLbl.textContent = 'API URL'
  urlInput.appendChild(urlLbl)
  const urlInp = document.createElement('input')
  urlInp.className = 'glyph-input'
  urlInp.type = 'text'
  urlInp.placeholder = 'https://api.example.com/data'
  urlInput.appendChild(urlInp)
  demoContent.appendChild(urlInput)

  const fetchBtn = document.createElement('button')
  fetchBtn.className = 'glyph-btn glyph-btn--primary'
  fetchBtn.textContent = 'Fetch'
  demoContent.appendChild(fetchBtn)

  const statusEl = document.createElement('div')
  statusEl.className = 'glyph-status'
  statusEl.style.fontSize = 'var(--font-size-sm)'
  statusEl.style.minHeight = '16px'
  demoContent.appendChild(statusEl)

  // Interactive: click Fetch to cycle through status states
  let demoState = 0
  fetchBtn.addEventListener('click', () => {
    if (demoState === 0) {
      statusEl.textContent = 'Fetching...'
      statusEl.style.color = 'var(--text-on-dark-tertiary)'
      demoState = 1
      setTimeout(() => fetchBtn.click(), 800)
    } else if (demoState === 1) {
      if (urlInp.value) {
        statusEl.textContent = 'OK — 200, 1.4kb'
        statusEl.style.color = 'var(--color-success, #22c55e)'
        demoState = 2
        setTimeout(() => { statusEl.textContent = ''; demoState = 0 }, 4000)
      } else {
        statusEl.textContent = 'No URL provided'
        statusEl.style.color = 'var(--color-error, #ef4444)'
        demoState = 0
      }
    }
  })

  glyphDemo.appendChild(demoContent)
  sdkGroup.appendChild(glyphDemo)
  section.appendChild(sdkGroup)

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
    variantLabel.style.fontSize = 'var(--font-size-xs)'
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
