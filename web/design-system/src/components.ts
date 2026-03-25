/**
 * Component gallery — live specimens of QNTX UI components.
 *
 * Split into SDK Primitives (what plugins get through ui.*)
 * and Internal Systems (used by QNTX core).
 *
 * The gallery itself is built from glyph primitives — title bars
 * as section headers, glyph containers as specimen wrappers.
 * Mini glyphs use the real GlyphUI SDK — actual ui.glyph(), ui.input(),
 * ui.button(), ui.statusLine() calls on a mini canvas.
 */

import { createGlyphUI } from '../../ts/components/glyph/glyph-ui'
import type { Glyph } from '../../ts/components/glyph/glyph'

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

  // Three mini glyphs on a mini canvas — real SDK calls
  const canvas = document.createElement('div')
  canvas.className = 'canvas-workspace'
  canvas.style.position = 'relative'
  canvas.style.height = '210px'
  canvas.style.marginBottom = '10px'
  canvas.style.border = '1px solid var(--border-on-dark)'
  canvas.style.borderRadius = 'var(--border-radius)'
  canvas.style.overflow = 'hidden'

  // ix-json: default title bar + SDK primitives
  const ixJsonGlyph: Glyph = { id: 'demo-ix-json', title: 'ix-json', symbol: 'ix-json', x: 10, y: 10, renderContent: () => document.createElement('div') }
  const ixJsonUI = createGlyphUI(ixJsonGlyph, 'ix-json')
  const ixJson = ixJsonUI.glyph({
    defaults: { x: 10, y: 10, width: 280, height: 190 },
    titleBar: { label: 'ix-json' },
  })

  const urlInput = ixJsonUI.input({ label: 'API URL', placeholder: 'https://api.example.com/data' })
  ixJson.content.appendChild(urlInput)

  const status = ixJsonUI.statusLine()
  let demoState = 0
  const fetchBtn = ixJsonUI.button({ label: 'Fetch', primary: true, onClick: () => {
    if (demoState === 0) {
      status.show('Fetching...')
      demoState = 1
      setTimeout(() => fetchBtn.click(), 800)
    } else if (demoState === 1) {
      const inp = urlInput.querySelector('input')
      if (inp?.value) {
        status.show('OK — 200, 1.4kb')
        demoState = 2
        setTimeout(() => { status.clear(); demoState = 0 }, 4000)
      } else {
        status.show('No URL provided', true)
        demoState = 0
      }
    }
  }})
  ixJson.content.appendChild(fetchBtn)
  ixJson.content.appendChild(status.element)
  canvas.appendChild(ixJson.element)

  // py-glyph: Python blue title bar
  const pyGlyphData: Glyph = { id: 'demo-py', title: 'py-glyph', symbol: 'py', x: 300, y: 10, renderContent: () => document.createElement('div') }
  const pyUI = createGlyphUI(pyGlyphData, 'py')
  const py = pyUI.glyph({
    defaults: { x: 300, y: 10, width: 280, height: 190 },
    titleBar: { label: 'py-glyph', color: '#2a5578', labelColor: '#FFD43B', actions: [runBtn()] },
  })
  const pyCode = document.createElement('pre')
  pyCode.style.fontFamily = 'monospace'
  pyCode.style.fontSize = 'var(--font-size-sm)'
  pyCode.style.color = 'var(--text-on-dark)'
  pyCode.style.whiteSpace = 'pre-wrap'
  pyCode.style.wordBreak = 'break-word'
  pyCode.textContent = 'import time\nimport secrets\n\nfoo = [\'teach\', \'meld\']\nprint(secrets.choice(foo))'
  py.content.appendChild(pyCode)
  canvas.appendChild(py.element)

  // ts-glyph: TypeScript amber title bar
  const tsGlyphData: Glyph = { id: 'demo-ts', title: 'ts-glyph', symbol: 'ts', x: 590, y: 10, renderContent: () => document.createElement('div') }
  const tsUI = createGlyphUI(tsGlyphData, 'ts')
  const ts = tsUI.glyph({
    defaults: { x: 590, y: 10, width: 280, height: 190 },
    titleBar: { label: 'ts-glyph', color: '#5c3d1a', labelColor: '#f0c878', actions: [runBtn()] },
  })
  const tsCode = document.createElement('pre')
  tsCode.style.fontFamily = 'monospace'
  tsCode.style.fontSize = 'var(--font-size-sm)'
  tsCode.style.color = 'var(--text-on-dark)'
  tsCode.style.whiteSpace = 'pre-wrap'
  tsCode.style.wordBreak = 'break-word'
  tsCode.textContent = 'const subjects = ["alice", "bob"]\nconst id = generateASUID()\nconsole.log(id)'
  ts.content.appendChild(tsCode)
  canvas.appendChild(ts.element)

  section.appendChild(canvas)

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

    // Auto-height — constrained width to force wrapping
    const autoHeightWrapper = titleBarStrip('Auto-height (--auto)', 'glyph-title-bar glyph-title-bar--auto', 'attestation with a longer title that wraps to demonstrate auto-height behavior', [
      { label: '\u27F3', cls: 'titlebar-btn' },
      { label: '\u2715', cls: 'titlebar-btn' },
    ])
    autoHeightWrapper.style.maxWidth = '320px'
    body.appendChild(autoHeightWrapper)
  }))

  // ── Focus ──
  section.appendChild(sectionGlyph('Focus', 'Double-click a glyph to focus it. Escape to unfocus. Viewport width determines the split.'))

  section.appendChild(glyphSection('Focus demo', 'Double-click any glyph below. Resize browser to see split change.', (body) => {
    const canvas = document.createElement('div')
    canvas.className = 'canvas-workspace'
    canvas.style.position = 'relative'
    canvas.style.height = '320px'
    canvas.style.overflow = 'hidden'
    canvas.tabIndex = 0

    // Track focus state
    let focusedElement: HTMLElement | null = null

    // Compute split column count from viewport width
    function getColumnCount(): number {
      const w = canvas.clientWidth
      if (w >= 960) return 4
      if (w >= 720) return 3
      if (w >= 480) return 2
      return 1
    }

    function focusGlyph(el: HTMLElement) {
      // Unfocus previous
      if (focusedElement && focusedElement !== el) {
        focusedElement.style.transition = 'transform 0.35s ease-out, width 0.35s ease-out, height 0.35s ease-out, left 0.35s ease-out, top 0.35s ease-out'
        focusedElement.style.transform = ''
        focusedElement.style.width = ''
        focusedElement.style.height = ''
        focusedElement.style.left = focusedElement.dataset.origLeft || ''
        focusedElement.style.top = focusedElement.dataset.origTop || ''
        focusedElement.style.zIndex = ''
        const prev = focusedElement
        setTimeout(() => { prev.style.transition = '' }, 350)
      }

      // Save original position on first focus of this element
      if (!el.dataset.origLeft) {
        el.dataset.origLeft = el.style.left
        el.dataset.origTop = el.style.top
      }

      const cols = getColumnCount()
      const colWidth = canvas.clientWidth / cols
      const colIndex = Math.floor(cols / 2) // center column

      el.style.transition = 'transform 0.35s ease-out, width 0.35s ease-out, height 0.35s ease-out, left 0.35s ease-out, top 0.35s ease-out'
      el.style.left = `${colIndex * colWidth}px`
      el.style.top = '0px'
      el.style.width = `${colWidth}px`
      el.style.height = `${canvas.clientHeight}px`
      el.style.zIndex = '10'
      setTimeout(() => { el.style.transition = '' }, 350)

      focusedElement = el
    }

    function unfocus() {
      if (!focusedElement) return
      focusedElement.style.transition = 'transform 0.35s ease-out, width 0.35s ease-out, height 0.35s ease-out, left 0.35s ease-out, top 0.35s ease-out'
      focusedElement.style.width = ''
      focusedElement.style.height = ''
      focusedElement.style.left = focusedElement.dataset.origLeft || ''
      focusedElement.style.top = focusedElement.dataset.origTop || ''
      focusedElement.style.zIndex = ''
      const prev = focusedElement
      setTimeout(() => { prev.style.transition = '' }, 350)
      focusedElement = null
    }

    // Create demo glyphs using SDK
    const glyphConfigs = [
      { id: 'focus-a', label: 'Glyph A', x: 10, y: 10, color: '#2a5578', labelColor: '#88ccff' },
      { id: 'focus-b', label: 'Glyph B', x: 300, y: 10, color: '#5c3d1a', labelColor: '#f0c878' },
      { id: 'focus-c', label: 'Glyph C', x: 590, y: 10, color: '#2a4a3a', labelColor: '#88ddaa' },
    ]

    for (const cfg of glyphConfigs) {
      const glyphData: Glyph = { id: cfg.id, title: cfg.label, x: cfg.x, y: cfg.y, renderContent: () => document.createElement('div') }
      const ui = createGlyphUI(glyphData, cfg.id)
      const g = ui.glyph({
        defaults: { x: cfg.x, y: cfg.y, width: 280, height: 190 },
        titleBar: { label: cfg.label, color: cfg.color, labelColor: cfg.labelColor },
      })

      const content = document.createElement('pre')
      content.style.fontFamily = 'monospace'
      content.style.fontSize = 'var(--font-size-sm)'
      content.style.color = 'var(--text-on-dark)'
      content.style.padding = '8px'
      content.textContent = `Double-click to focus ${cfg.label}\nEscape to unfocus\n\nViewport columns: resize to see`

      g.content.appendChild(content)
      canvas.appendChild(g.element)
    }

    // Double-click to focus
    canvas.addEventListener('dblclick', (e) => {
      const target = (e.target as HTMLElement).closest('[data-glyph-id]') as HTMLElement | null
      if (target && canvas.contains(target)) {
        canvas.focus()
        focusGlyph(target)
      }
    })

    // Escape to unfocus
    canvas.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && focusedElement) {
        e.preventDefault()
        unfocus()
      }
    })

    // Click background to unfocus
    canvas.addEventListener('click', (e) => {
      const target = (e.target as HTMLElement).closest('[data-glyph-id]')
      if (!target && focusedElement) unfocus()
    })

    // Viewport presets
    const presets: [string, number, number][] = [
      ['Phone', 375, 667],
      ['Phone landscape', 667, 375],
      ['Tablet', 768, 1024],
      ['Desktop', 1100, 320],
    ]

    const controls = document.createElement('div')
    controls.style.display = 'flex'
    controls.style.gap = '4px'
    controls.style.marginBottom = '6px'

    let activePreset: string | null = null

    for (const [label, w, h] of presets) {
      const btn = document.createElement('button')
      btn.className = 'qntx-btn qntx-btn-small qntx-btn-ghost'
      const btnLabel = document.createElement('span')
      btnLabel.className = 'qntx-btn-label'
      btnLabel.textContent = label
      btn.appendChild(btnLabel)

      btn.addEventListener('click', () => {
        if (activePreset === label) {
          // Toggle off — restore full width
          canvas.style.width = ''
          canvas.style.height = '320px'
          canvas.style.margin = ''
          activePreset = null
          controls.querySelectorAll('.qntx-btn').forEach(b => b.classList.remove('qntx-btn-primary'))
        } else {
          canvas.style.width = `${w}px`
          canvas.style.height = `${h}px`
          canvas.style.margin = '0 auto'
          activePreset = label
          controls.querySelectorAll('.qntx-btn').forEach(b => b.classList.remove('qntx-btn-primary'))
          btn.classList.add('qntx-btn-primary')
        }
        if (focusedElement) unfocus()
        updateIndicator()
      })
      controls.appendChild(btn)
    }
    body.appendChild(controls)

    // Column indicator
    const indicator = document.createElement('div')
    indicator.style.position = 'absolute'
    indicator.style.bottom = '4px'
    indicator.style.right = '8px'
    indicator.style.fontSize = 'var(--font-size-xs)'
    indicator.style.color = 'var(--text-on-dark-tertiary)'
    function updateIndicator() {
      indicator.textContent = `${getColumnCount()} columns @ ${canvas.clientWidth}px`
    }
    canvas.appendChild(indicator)

    const observer = new ResizeObserver(updateIndicator)
    observer.observe(canvas)
    setTimeout(updateIndicator, 0)

    body.appendChild(canvas)
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

/** Title bar action button — play/run */
function runBtn(): HTMLElement {
  const btn = document.createElement('button')
  btn.className = 'titlebar-btn'
  btn.textContent = '\u25B6'
  return btn
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
