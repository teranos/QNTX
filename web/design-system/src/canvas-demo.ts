/**
 * Canvas demo — focusable canvas with real composition edges.
 *
 * Edges are data. buildFocusGraph walks them. No hardcoded per-glyph providers.
 * Dogfoods canvas-pan (zoom/pan), focus manifestation, and the edge graph.
 *
 * DAG:
 *   A →right→ B →right→ C →right→ D →right→ E
 *             B →bottom→ R1 →bottom→ R2 →bottom→ R3
 *                        R1 →right→ L1 →right→ L2
 */

import { createGlyphUI } from '../../ts/components/glyph/glyph-ui'
import type { Glyph } from '@qntx/glyphs'
import {
  configureGlyphs,
  buildFocusGraph,
  focusGlyph as canvasFocusGlyph,
  unfocusGlyph,
  isFocused,
  setupCanvasFocus,
} from '@qntx/glyphs'
import type { FocusPanControl, CompositionEdge } from '@qntx/glyphs'
import { setupCanvasPan, getTransform, setPanZoom, resetTransform } from '../../ts/components/glyph/canvas/canvas-pan'

configureGlyphs({
  logger: {
    debug: (_seg, msg) => console.debug(`[⧉]`, msg),
    info: (_seg, msg) => console.info(`[⧉]`, msg),
    warn: (_seg, msg) => console.warn(`[⧉]`, msg),
    error: (_seg, msg) => console.error(`[⧉]`, msg),
  },
})

// ── Composition edges (the single source of truth) ──

const edges: CompositionEdge[] = [
  // Horizontal chain: A → B → C → D → E
  { from: 'A', to: 'B', direction: 'right', position: 0 },
  { from: 'B', to: 'C', direction: 'right', position: 1 },
  { from: 'C', to: 'D', direction: 'right', position: 2 },
  { from: 'D', to: 'E', direction: 'right', position: 3 },
  // Vertical chain: B → R1 → R2 → R3
  { from: 'B', to: 'R1', direction: 'bottom', position: 0 },
  { from: 'R1', to: 'R2', direction: 'bottom', position: 1 },
  { from: 'R2', to: 'R3', direction: 'bottom', position: 2 },
  // Horizontal from R1: R1 → L1 → L2
  { from: 'R1', to: 'L1', direction: 'right', position: 0 },
  { from: 'L1', to: 'L2', direction: 'right', position: 1 },
]

// ── Glyph definitions ──

interface GlyphSpec {
  id: string
  label: string
  color?: string
  labelColor?: string
  x: number
  y: number
  width: number
  height: number
  content: string
}

const glyphSpecs: GlyphSpec[] = [
  { id: 'A', label: 'ix-json', x: 10, y: 10, width: 260, height: 170, content: 'API URL: https://api.example.com/data\n\n[Fetch]' },
  { id: 'B', label: 'py-glyph', color: '#2a5578', labelColor: '#FFD43B', x: 290, y: 10, width: 260, height: 170, content: 'import time\nimport secrets\n\nfoo = [\'teach\', \'meld\']\nprint(secrets.choice(foo))' },
  { id: 'C', label: 'ts-glyph', color: '#5c3d1a', labelColor: '#f0c878', x: 570, y: 10, width: 260, height: 170, content: 'const subjects = ["alice", "bob"]\nconst id = generateASUID()\nconsole.log(id)' },
  { id: 'D', label: 'rs-glyph', color: '#4a2a1a', labelColor: '#f0a060', x: 850, y: 10, width: 260, height: 130, content: 'let mut v = Vec::new();\nv.push(42);' },
  { id: 'E', label: 'go-glyph', color: '#1a3a4a', labelColor: '#7ecbf0', x: 1130, y: 10, width: 260, height: 130, content: 'func main() {\n    fmt.Println("hello")\n}' },
  { id: 'R1', label: 'result-1', x: 290, y: 200, width: 260, height: 100, content: '>>> teach\nExecution time: 0.003s' },
  { id: 'R2', label: 'result-2', x: 290, y: 320, width: 260, height: 100, content: '>>> meld\nExecution time: 0.001s' },
  { id: 'R3', label: 'result-3', x: 290, y: 440, width: 260, height: 100, content: '>>> teach\nExecution time: 0.002s' },
  { id: 'L1', label: 'log-1', x: 570, y: 200, width: 220, height: 80, content: '[log-1] stdout captured' },
  { id: 'L2', label: 'log-2', x: 810, y: 200, width: 220, height: 80, content: '[log-2] stdout captured' },
]

/**
 * Render canvas demo into the given container.
 * Returns the elements to append (section header, canvas).
 */
export function renderCanvasDemo(
  sectionGlyph: (title: string, description: string) => HTMLElement,
  glyphSection: (title: string, description: string, build: (body: HTMLElement) => void) => HTMLElement,
): HTMLElement[] {
  const elements: HTMLElement[] = []

  elements.push(sectionGlyph('Canvas', 'Double-click a glyph to focus. Escape to unfocus. Scroll to navigate.'))

  elements.push(glyphSection('Focus demo', 'Real composition edges fed to buildFocusGraph. No hardcoded providers.', (body) => {
    const canvasId = 'design-system-focus'
    const canvas = document.createElement('div')
    canvas.className = 'canvas-workspace'
    canvas.style.position = 'relative'
    canvas.style.height = '420px'
    canvas.style.overflow = 'hidden'
    canvas.style.border = '2px solid var(--border-on-dark)'
    canvas.tabIndex = 0

    const contentLayer = document.createElement('div')
    contentLayer.className = 'canvas-content-layer'
    contentLayer.style.position = 'absolute'
    contentLayer.style.transformOrigin = '0 0'
    contentLayer.style.width = '100%'
    contentLayer.style.height = '100%'

    // Create glyph elements
    for (const spec of glyphSpecs) {
      const data: Glyph = { id: spec.id, title: spec.label, symbol: spec.label, x: spec.x, y: spec.y, renderContent: () => document.createElement('div') }
      const ui = createGlyphUI(data, spec.label)
      const g = ui.glyph({
        defaults: { x: spec.x, y: spec.y, width: spec.width, height: spec.height },
        titleBar: {
          label: spec.label,
          ...(spec.color ? { color: spec.color } : {}),
          ...(spec.labelColor ? { labelColor: spec.labelColor } : {}),
        },
      })
      const pre = document.createElement('pre')
      pre.style.fontFamily = 'monospace'
      pre.style.fontSize = 'var(--font-size-sm)'
      pre.style.color = 'var(--text-on-dark)'
      pre.style.whiteSpace = 'pre-wrap'
      pre.style.wordBreak = 'break-word'
      pre.textContent = spec.content
      g.content.appendChild(pre)
      contentLayer.appendChild(g.element)
    }

    // ── Event handlers ──

    canvas.addEventListener('dblclick', (e) => {
      const target = (e.target as HTMLElement).closest('[data-glyph-id]') as HTMLElement | null
      if (target && canvas.contains(target)) {
        canvas.focus()
        canvasFocusGlyph(canvas, canvasId, target)
        updateIndicator()
      }
    })

    canvas.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && isFocused(canvasId)) {
        e.preventDefault()
        unfocusGlyph(canvas, canvasId)
        updateIndicator()
      }
    })

    canvas.addEventListener('click', (e) => {
      const target = (e.target as HTMLElement).closest('[data-glyph-id]')
      if (!target && isFocused(canvasId)) {
        unfocusGlyph(canvas, canvasId)
        updateIndicator()
      }
    })

    // ── Controls ──

    const controls = document.createElement('div')
    controls.style.display = 'flex'
    controls.style.gap = '4px'
    controls.style.marginBottom = '6px'

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

    // Indicator
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
      indicator.textContent = `${canvas.clientWidth}px — ${zoomPct}% zoom — pan(${Math.round(t.panX)}, ${Math.round(t.panY)})${focusLabel}`
    }
    canvas.appendChild(indicator)

    canvas.appendChild(contentLayer)

    // Set up pan/zoom and focus
    setupCanvasPan(canvas, canvasId)

    const panControl: FocusPanControl = { getTransform, setPanZoom }
    setupCanvasFocus(canvas, canvasId, {
      panControl,
      graphProvider: (glyphId) => buildFocusGraph(edges, glyphId),
    })

    canvas.addEventListener('wheel', () => setTimeout(updateIndicator, 0), { passive: true })
    const observer = new ResizeObserver(updateIndicator)
    observer.observe(canvas)
    setTimeout(updateIndicator, 0)

    body.appendChild(canvas)
  }))

  return elements
}
