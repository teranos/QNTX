/**
 * Component gallery — live specimens of QNTX UI components.
 *
 * Split into SDK Primitives (what plugins get through ui.*)
 * and Internal Systems (used by QNTX core).
 *
 * The gallery itself is built from glyph primitives — title bars
 * as section headers, glyph containers as specimen wrappers.
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
  section.appendChild(sectionGlyph('SDK Primitives', 'What plugins get through ui.* — the canonical components for plugin-authored glyphs'))

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

  // Three mini glyphs side by side — showing colored title bars + content
  const glyphRow = document.createElement('div')
  glyphRow.className = 'glyph-specimen-row'

  // ix-json: default title bar + SDK primitives
  const ixJsonGlyph = miniGlyph('ix-json', undefined, 180, (content) => {
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
    content.appendChild(urlInput)

    const fetchBtn = document.createElement('button')
    fetchBtn.className = 'glyph-btn glyph-btn--primary'
    fetchBtn.textContent = 'Fetch'
    content.appendChild(fetchBtn)

    const statusEl = document.createElement('div')
    statusEl.className = 'glyph-status'
    statusEl.style.fontFamily = 'monospace'
    statusEl.style.fontSize = 'var(--font-size-xs)'
    statusEl.style.minHeight = '16px'
    statusEl.style.lineHeight = '16px'
    content.appendChild(statusEl)

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
  })
  glyphRow.appendChild(ixJsonGlyph)

  // py-glyph: Python blue title bar + code placeholder
  const pyGlyph = miniGlyph('py-glyph', '#2a5578', 180, (content) => {
    const code = document.createElement('pre')
    code.style.fontFamily = 'monospace'
    code.style.fontSize = 'var(--font-size-sm)'
    code.style.color = 'var(--text-on-dark)'
    code.style.whiteSpace = 'pre-wrap'
    code.style.wordBreak = 'break-word'
    code.textContent = 'import time\nimport secrets\n\nfoo = [\'teach\', \'meld\']\nprint(secrets.choice(foo))'
    content.appendChild(code)
  }, [{ label: '\u25B6', cls: 'titlebar-btn' }])
  glyphRow.appendChild(pyGlyph)

  // ts-glyph: TypeScript amber title bar + code placeholder
  const tsGlyph = miniGlyph('ts-glyph', '#5c3d1a', 180, (content) => {
    const code = document.createElement('pre')
    code.style.fontFamily = 'monospace'
    code.style.fontSize = 'var(--font-size-sm)'
    code.style.color = 'var(--text-on-dark)'
    code.style.whiteSpace = 'pre-wrap'
    code.style.wordBreak = 'break-word'
    code.textContent = 'const subjects = ["alice", "bob"]\nconst id = generateASUID()\nconsole.log(id)'
    content.appendChild(code)
  }, [{ label: '\u25B6', cls: 'titlebar-btn' }])
  glyphRow.appendChild(tsGlyph)

  section.appendChild(glyphRow)

  // ── Internal Systems ──
  section.appendChild(sectionGlyph('Internal Systems', 'Used by QNTX core — not exposed to plugins'))

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
  section.appendChild(glyphSection('Two-stage confirmation', 'First click enters confirming state, second click executes. Auto-reverts after 3s.', (body) => {
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
      variantLabel.style.marginTop = '4px'
      variantLabel.style.display = 'block'
      variantLabel.style.fontSize = 'var(--font-size-xs)'
      variantLabel.style.color = 'var(--text-on-dark-tertiary)'
      variantLabel.textContent = `qntx-btn-${variant}`

      wrapper.appendChild(btn)
      wrapper.appendChild(variantLabel)
      confirmRow.appendChild(wrapper)
    }

    body.appendChild(confirmRow)
  }))

  // titlebar specimens
  section.appendChild(glyphSection('glyph-title-bar', 'Unified title bar for all glyph manifestations', (body) => {
    const tbRow = document.createElement('div')
    tbRow.className = 'titlebar-row'

    tbRow.appendChild(titleBarStrip('Standard', 'glyph-title-bar', 'ix-prompt', [
      { label: '\u25B6', cls: 'titlebar-btn' },
      { label: '\u2715', cls: 'titlebar-btn' },
    ]))

    tbRow.appendChild(titleBarStrip('Generic buttons', 'glyph-title-bar', 'result-glyph', [
      { label: '\u229E', cls: '' },
      { label: '\u2715', cls: '' },
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
    panelBtn.textContent = '\u2715'
    panelBar.appendChild(panelBtn)
    panelWrap.appendChild(panelBar)
    tbRow.appendChild(panelWrap)

    body.appendChild(tbRow)

    // Auto-height
    body.appendChild(titleBarStrip('Auto-height (--auto)', 'glyph-title-bar glyph-title-bar--auto', 'attestation with a longer title that wraps to demonstrate auto-height behavior', [
      { label: '\u27F3', cls: 'titlebar-btn' },
      { label: '\u2715', cls: 'titlebar-btn' },
    ]))
  }))

  root.appendChild(section)
}

// ── Helpers ──

/** Section header rendered as a glyph title bar */
function sectionGlyph(title: string, description: string): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.style.marginTop = '10px'
  wrapper.style.marginBottom = '6px'

  const bar = document.createElement('div')
  bar.className = 'glyph-title-bar'

  const titleSpan = document.createElement('span')
  titleSpan.style.flex = '1'
  titleSpan.textContent = title
  bar.appendChild(titleSpan)

  wrapper.appendChild(bar)

  if (description) {
    const desc = document.createElement('div')
    desc.style.fontSize = 'var(--font-size-xs)'
    desc.style.color = 'var(--text-on-dark-tertiary)'
    desc.style.padding = '2px 8px'
    desc.textContent = description
    wrapper.appendChild(desc)
  }

  return wrapper
}

/** Component section wrapped in a glyph-like container with title bar */
function glyphSection(title: string, description: string, buildContent: (body: HTMLElement) => void): HTMLElement {
  const container = document.createElement('div')
  container.style.border = '1px solid var(--border-on-dark)'
  container.style.borderRadius = 'var(--border-radius)'
  container.style.overflow = 'hidden'
  container.style.marginBottom = '10px'

  const bar = document.createElement('div')
  bar.className = 'glyph-title-bar'

  const titleSpan = document.createElement('span')
  titleSpan.style.flex = '1'
  titleSpan.textContent = title
  bar.appendChild(titleSpan)

  const descSpan = document.createElement('span')
  descSpan.style.fontSize = 'var(--font-size-xs)'
  descSpan.style.color = 'var(--text-on-dark-tertiary)'
  descSpan.textContent = description
  bar.appendChild(descSpan)

  container.appendChild(bar)

  const body = document.createElement('div')
  body.className = 'glyph-content-area'
  buildContent(body)
  container.appendChild(body)

  return container
}

/** Mini glyph specimen — title bar + content area */
function miniGlyph(
  title: string,
  titleBarColor: string | undefined,
  height: number,
  buildContent: (content: HTMLElement) => void,
  actions?: { label: string, cls: string }[],
): HTMLElement {
  const glyph = document.createElement('div')
  glyph.style.display = 'flex'
  glyph.style.flexDirection = 'column'
  glyph.style.border = '1px solid var(--border-on-dark)'
  glyph.style.borderRadius = 'var(--border-radius)'
  glyph.style.height = `${height}px`
  glyph.style.overflow = 'hidden'
  glyph.style.background = 'var(--bg-secondary)'

  const bar = document.createElement('div')
  bar.className = 'glyph-title-bar'
  if (titleBarColor) bar.style.backgroundColor = titleBarColor

  const label = document.createElement('span')
  label.textContent = title
  label.style.flex = '1'
  bar.appendChild(label)

  if (actions) {
    for (const a of actions) {
      const btn = document.createElement('button')
      if (a.cls) btn.className = a.cls
      btn.textContent = a.label
      bar.appendChild(btn)
    }
  }

  const closeBtn = document.createElement('button')
  closeBtn.className = 'titlebar-btn'
  closeBtn.textContent = '\u2715'
  bar.appendChild(closeBtn)

  glyph.appendChild(bar)

  const content = document.createElement('div')
  content.className = 'glyph-content-area'
  content.style.display = 'flex'
  content.style.flexDirection = 'column'
  content.style.gap = '8px'
  buildContent(content)
  glyph.appendChild(content)

  return glyph
}

function buttonMatrix(name: string, description: string, columnLabels: string[], rows: MatrixRow[]): HTMLElement {
  const container = glyphSection(name, description, (body) => {
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

    body.appendChild(table)
  })

  return container
}

function titleBarStrip(label: string, barClasses: string, title: string, buttons: {label: string, cls: string}[], bgColor?: string): HTMLElement {
  const row = document.createElement('div')
  row.className = 'titlebar-specimen'

  const desc = document.createElement('span')
  desc.className = 'titlebar-specimen-label'
  desc.textContent = label
  row.appendChild(desc)

  const bar = document.createElement('div')
  bar.className = barClasses
  if (bgColor) bar.style.backgroundColor = bgColor

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
