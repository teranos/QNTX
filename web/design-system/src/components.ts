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
