/**
 * Canvas demo — real SDK glyphs on a focusable canvas with zoom/pan.
 *
 * Dogfoods canvas-pan (real zoom/pan), GlyphUI SDK, and focus behavior.
 * DAG structure: ix-json →right py →right ts, py →bottom result-1 →bottom result-2 →bottom result-3
 * Breakpoints table, viewport presets, column overrides, zoom controls.
 */

import { createGlyphUI } from '../../ts/components/glyph/glyph-ui'
import type { Glyph } from '../../ts/components/glyph/glyph'
import { setupCanvasPan, getTransform, setZoom, resetTransform } from '../../ts/components/glyph/canvas/canvas-pan'
import { focusGlyph as canvasFocusGlyph, unfocusGlyph, isFocused, setupCanvasFocus } from '../../ts/components/glyph/canvas/canvas-focus'
import type { FocusGraphProvider } from '../../ts/components/glyph/canvas/canvas-focus'

/**
 * Render canvas demo into the given container.
 * Returns the elements to append (section header, breakpoints, canvas).
 */
export function renderCanvasDemo(
  sectionGlyph: (title: string, description: string) => HTMLElement,
  glyphSection: (title: string, description: string, build: (body: HTMLElement) => void) => HTMLElement,
): HTMLElement[] {
  const elements: HTMLElement[] = []

  elements.push(sectionGlyph('Canvas', 'Double-click a glyph to focus it. Escape to unfocus. Viewport width determines the split.'))

  // Breakpoints reference
  elements.push(glyphSection('Breakpoints', 'Focus column splits — always odd, center column is the thread', (body) => {
    const table = document.createElement('div')
    table.className = 'button-matrix'
    table.style.gridTemplateColumns = '100px 100px 1fr'

    const headers = ['Width', 'Columns', 'System breakpoint']
    for (const h of headers) {
      const cell = document.createElement('div')
      cell.className = 'button-matrix-header'
      cell.textContent = h
      table.appendChild(cell)
    }

    const rows: [string, string, string][] = [
      ['< 480px', '1', 'phone portrait'],
      ['480 \u2013 719px', '1', ''],
      ['720 \u2013 767px', '3', ''],
      ['768 \u2013 899px', '3', 'mobile (--breakpoint-mobile)'],
      ['900 \u2013 959px', '3', 'tablet (--breakpoint-tablet)'],
      ['960 \u2013 1199px', '5', ''],
      ['1200px+', '5', 'desktop (--breakpoint-desktop)'],
    ]

    for (const [width, cols, system] of rows) {
      const wCell = document.createElement('div')
      wCell.className = 'button-matrix-rowlabel'
      wCell.textContent = width
      table.appendChild(wCell)

      const cCell = document.createElement('div')
      cCell.className = 'button-matrix-cell'
      cCell.textContent = cols
      table.appendChild(cCell)

      const sCell = document.createElement('div')
      sCell.className = 'button-matrix-cell'
      sCell.style.color = system ? 'var(--text-on-dark)' : 'var(--text-on-dark-tertiary)'
      sCell.textContent = system || '\u2014'
      sCell.style.justifyContent = 'flex-start'
      table.appendChild(sCell)
    }

    body.appendChild(table)
  }))

  // Canvas with real SDK glyphs, zoom/pan, focus
  elements.push(glyphSection('Canvas demo', 'Double-click any glyph to enter its DAG. Escape to unfocus. Click a sibling to pivot.', (body) => {
    const canvasId = 'design-system-focus'
    const canvas = document.createElement('div')
    canvas.className = 'canvas-workspace'
    canvas.style.position = 'relative'
    canvas.style.height = '420px'
    canvas.style.overflow = 'hidden'
    canvas.tabIndex = 0

    // Content layer — required by canvas-pan for transform application
    const contentLayer = document.createElement('div')
    contentLayer.className = 'canvas-content-layer'
    contentLayer.style.position = 'absolute'
    contentLayer.style.transformOrigin = '0 0'
    contentLayer.style.width = '100%'
    contentLayer.style.height = '100%'

    let columnOverride: number | null = null

    // Compute split column count from viewport width (or use override)
    // Always odd: 1, 3, or 5
    function getColumnCount(_viewportWidth?: number): number {
      if (columnOverride) return columnOverride
      const w = canvas.clientWidth
      if (w >= 960) return 5
      if (w >= 720) return 3
      return 1
    }

    // ── Demo glyphs ──

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
    contentLayer.appendChild(ixJson.element)

    // py-glyph: Python blue title bar (prompt at top of vertical chain)
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
    contentLayer.appendChild(py.element)

    // ts-glyph: TypeScript amber title bar (horizontal sibling of py)
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
    contentLayer.appendChild(ts.element)

    // result-1: first result below py (vertical chain)
    const r1Data: Glyph = { id: 'demo-result-1', title: 'result-1', symbol: 'result', x: 300, y: 220, renderContent: () => document.createElement('div') }
    const r1UI = createGlyphUI(r1Data, 'result')
    const r1 = r1UI.glyph({
      defaults: { x: 300, y: 220, width: 280, height: 120 },
      titleBar: { label: 'result-1' },
    })
    const r1Content = document.createElement('pre')
    r1Content.style.fontFamily = 'monospace'
    r1Content.style.fontSize = 'var(--font-size-sm)'
    r1Content.style.color = 'var(--text-on-dark)'
    r1Content.style.whiteSpace = 'pre-wrap'
    r1Content.style.wordBreak = 'break-word'
    r1Content.textContent = '>>> teach\nExecution time: 0.003s'
    r1.content.appendChild(r1Content)
    contentLayer.appendChild(r1.element)

    // result-2: second result (deeper in chain)
    const r2Data: Glyph = { id: 'demo-result-2', title: 'result-2', symbol: 'result', x: 300, y: 360, renderContent: () => document.createElement('div') }
    const r2UI = createGlyphUI(r2Data, 'result')
    const r2 = r2UI.glyph({
      defaults: { x: 300, y: 360, width: 280, height: 120 },
      titleBar: { label: 'result-2' },
    })
    const r2Content = document.createElement('pre')
    r2Content.style.fontFamily = 'monospace'
    r2Content.style.fontSize = 'var(--font-size-sm)'
    r2Content.style.color = 'var(--text-on-dark)'
    r2Content.style.whiteSpace = 'pre-wrap'
    r2Content.style.wordBreak = 'break-word'
    r2Content.textContent = '>>> meld\nExecution time: 0.001s'
    r2.content.appendChild(r2Content)
    contentLayer.appendChild(r2.element)

    // result-3: third result (deepest)
    const r3Data: Glyph = { id: 'demo-result-3', title: 'result-3', symbol: 'result', x: 300, y: 500, renderContent: () => document.createElement('div') }
    const r3UI = createGlyphUI(r3Data, 'result')
    const r3 = r3UI.glyph({
      defaults: { x: 300, y: 500, width: 280, height: 120 },
      titleBar: { label: 'result-3' },
    })
    const r3Content = document.createElement('pre')
    r3Content.style.fontFamily = 'monospace'
    r3Content.style.fontSize = 'var(--font-size-sm)'
    r3Content.style.color = 'var(--text-on-dark)'
    r3Content.style.whiteSpace = 'pre-wrap'
    r3Content.style.wordBreak = 'break-word'
    r3Content.textContent = '>>> teach\nExecution time: 0.002s'
    r3.content.appendChild(r3Content)
    contentLayer.appendChild(r3.element)

    // ── DAG structure ──
    // ix-json →right py →right ts
    //                 py →bottom result-1 →bottom result-2 →bottom result-3

    // DAG: ix-json →right py →right ts
    //                      py →bottom result-1 →bottom result-2 →bottom result-3
    const pyThread = ['demo-py', 'demo-result-1', 'demo-result-2', 'demo-result-3']

    const focusGraphProvider: FocusGraphProvider = (glyphId) => {
      if (pyThread.indexOf(glyphId) >= 0) {
        // Focusing any member of py's vertical chain
        // ix-json is LEFT of py, ts is RIGHT of py
        return {
          thread: pyThread,
          focusIndex: pyThread.indexOf(glyphId),
          leftSiblings: new Map([['demo-py', ['demo-ix-json']]]),
          rightSiblings: new Map([['demo-py', ['demo-ts']]]),
          siblingThreads: new Map(),
        }
      }

      if (glyphId === 'demo-ix-json') {
        // Pivot: ix-json becomes center, py is to the RIGHT
        return {
          thread: ['demo-ix-json'],
          focusIndex: 0,
          leftSiblings: new Map(),
          rightSiblings: new Map([['demo-ix-json', ['demo-py', 'demo-ts']]]),
          siblingThreads: new Map([['demo-py', pyThread]]),
        }
      }

      if (glyphId === 'demo-ts') {
        // Pivot: ts becomes center, py is to the LEFT
        return {
          thread: ['demo-ts'],
          focusIndex: 0,
          leftSiblings: new Map([['demo-ts', ['demo-py', 'demo-ix-json']]]),
          rightSiblings: new Map(),
          siblingThreads: new Map([['demo-py', pyThread]]),
        }
      }

      // Standalone glyph
      return { thread: [glyphId], focusIndex: 0, leftSiblings: new Map(), rightSiblings: new Map(), siblingThreads: new Map() }
    }

    // ── Event handlers ──

    // Double-click to focus
    canvas.addEventListener('dblclick', (e) => {
      const target = (e.target as HTMLElement).closest('[data-glyph-id]') as HTMLElement | null
      if (target && canvas.contains(target)) {
        canvas.focus()
        canvasFocusGlyph(canvas, canvasId, target)
        updateIndicator()
      }
    })

    // Escape to unfocus
    canvas.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && isFocused(canvasId)) {
        e.preventDefault()
        unfocusGlyph(canvas, canvasId)
        updateIndicator()
      }
    })

    // Click background to unfocus
    canvas.addEventListener('click', (e) => {
      const target = (e.target as HTMLElement).closest('[data-glyph-id]')
      if (!target && isFocused(canvasId)) {
        unfocusGlyph(canvas, canvasId)
        updateIndicator()
      }
    })

    // ── Controls ──

    // Viewport presets
    const presets: [string, number, number][] = [
      ['Phone', 375, 667],
      ['Phone landscape', 667, 375],
      ['Tablet', 768, 1024],
      ['Desktop', 1100, 420],
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
          canvas.style.width = ''
          canvas.style.height = '420px'
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
        if (isFocused(canvasId)) unfocusGlyph(canvas, canvasId)
        updateIndicator()
      })
      controls.appendChild(btn)
    }

    // Separator
    const sep = document.createElement('span')
    sep.style.borderLeft = '1px solid var(--border-on-dark)'
    sep.style.margin = '0 2px'
    controls.appendChild(sep)

    // Column override buttons — always odd
    for (const n of [3, 5]) {
      const btn = document.createElement('button')
      btn.className = 'qntx-btn qntx-btn-small qntx-btn-ghost'
      const btnLabel = document.createElement('span')
      btnLabel.className = 'qntx-btn-label'
      btnLabel.textContent = `${n} col`
      btn.appendChild(btnLabel)

      btn.addEventListener('click', () => {
        if (columnOverride === n) {
          columnOverride = null
          btn.classList.remove('qntx-btn-primary')
        } else {
          columnOverride = n
          controls.querySelectorAll('.col-override').forEach(b => b.classList.remove('qntx-btn-primary'))
          btn.classList.add('qntx-btn-primary')
        }
        if (isFocused(canvasId)) unfocusGlyph(canvas, canvasId)
        updateIndicator()
      })
      btn.classList.add('col-override')
      controls.appendChild(btn)
    }

    // Separator before zoom controls
    const sep2 = document.createElement('span')
    sep2.style.borderLeft = '1px solid var(--border-on-dark)'
    sep2.style.margin = '0 2px'
    controls.appendChild(sep2)

    // Random zoom/pan button
    const randomBtn = document.createElement('button')
    randomBtn.className = 'qntx-btn qntx-btn-small qntx-btn-ghost'
    const randomLabel = document.createElement('span')
    randomLabel.className = 'qntx-btn-label'
    randomLabel.textContent = 'Random'
    randomBtn.appendChild(randomLabel)
    randomBtn.addEventListener('click', () => {
      const scale = 0.4 + Math.random() * 2.1
      const panX = -200 + Math.random() * 400
      const panY = -100 + Math.random() * 200
      setZoom(canvas, canvasId, scale)
      const t = getTransform(canvasId)
      const cl = canvas.querySelector('.canvas-content-layer') as HTMLElement
      if (cl) {
        cl.style.transform = `translate(${panX}px, ${panY}px) scale(${t.scale})`
      }
      updateIndicator()
    })
    controls.appendChild(randomBtn)

    // Reset zoom/pan button
    const resetBtn = document.createElement('button')
    resetBtn.className = 'qntx-btn qntx-btn-small qntx-btn-ghost'
    const resetLabel = document.createElement('span')
    resetLabel.className = 'qntx-btn-label'
    resetLabel.textContent = 'Reset'
    resetBtn.appendChild(resetLabel)
    resetBtn.addEventListener('click', () => {
      resetTransform(canvas, canvasId)
      updateIndicator()
    })
    controls.appendChild(resetBtn)

    body.appendChild(controls)

    // Zoom/pan indicator
    const indicator = document.createElement('div')
    indicator.style.position = 'absolute'
    indicator.style.bottom = '4px'
    indicator.style.right = '8px'
    indicator.style.fontSize = 'var(--font-size-xs)'
    indicator.style.color = 'var(--text-on-dark-tertiary)'
    indicator.style.zIndex = '1'
    function updateIndicator() {
      const t = getTransform(canvasId)
      const zoomPct = Math.round(t.scale * 100)
      const focusLabel = isFocused(canvasId) ? ' — focused' : ''
      indicator.textContent = `${getColumnCount()} col @ ${canvas.clientWidth}px — ${zoomPct}% zoom — pan(${Math.round(t.panX)}, ${Math.round(t.panY)})${focusLabel}`
    }
    canvas.appendChild(indicator)

    // Add content layer to canvas, set up real pan/zoom and focus
    canvas.appendChild(contentLayer)

    setupCanvasPan(canvas, canvasId, () => isFocused(canvasId))
    setupCanvasFocus(canvasId, focusGraphProvider, getColumnCount)

    // Update indicator on zoom/pan changes
    canvas.addEventListener('wheel', () => setTimeout(updateIndicator, 0), { passive: true })
    canvas.addEventListener('touchend', () => setTimeout(updateIndicator, 0))

    const observer = new ResizeObserver(updateIndicator)
    observer.observe(canvas)
    setTimeout(updateIndicator, 0)

    body.appendChild(canvas)
  }))

  return elements
}

/** Title bar action button — play/run */
function runBtn(): HTMLElement {
  const btn = document.createElement('button')
  btn.className = 'titlebar-btn'
  btn.textContent = '\u25B6'
  return btn
}
