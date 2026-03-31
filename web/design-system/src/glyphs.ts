/**
 * Glyph specimens — live demos of @qntx/glyphs capabilities.
 *
 * Each specimen imports directly from the package and demonstrates
 * a specific subsystem: proximity engine, morph transactions,
 * window drag, title bar controls, stash/restore.
 */

import {
    GlyphProximity,
    beginMaximizeMorph,
    beginMinimizeMorph,
    getMaximizeDuration,
    getMinimizeDuration,
    addWindowControls,
    removeWindowControls,
    stashContent,
    restoreContent,
    hasStash,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    setGlyphId,
    setGlyphSymbol,
} from '@qntx/glyphs'

// ── Entry point ──────────────────────────────────────────────────────

export function renderGlyphSpecimens(root: HTMLElement) {
    const section = document.createElement('section')
    section.className = 'token-group'

    const h2 = document.createElement('h2')
    h2.textContent = '@qntx/glyphs'
    section.appendChild(h2)

    const intro = document.createElement('div')
    intro.style.fontSize = 'var(--font-size-xs)'
    intro.style.color = 'var(--text-on-dark-tertiary)'
    intro.style.marginBottom = '8px'
    intro.textContent = 'Live specimens imported directly from the @qntx/glyphs package. Each demo exercises real package code.'
    section.appendChild(intro)

    // Proximity and morph side by side
    const topRow = document.createElement('div')
    topRow.style.display = 'grid'
    topRow.style.gridTemplateColumns = '1fr 1fr'
    topRow.style.gap = '8px'
    proximitySpecimen(topRow)
    morphSpecimen(topRow)
    section.appendChild(topRow)

    // Title bar controls and stash side by side
    const bottomRow = document.createElement('div')
    bottomRow.style.display = 'grid'
    bottomRow.style.gridTemplateColumns = '1fr 1fr'
    bottomRow.style.gap = '8px'
    titleBarSpecimen(bottomRow)
    stashSpecimen(bottomRow)
    section.appendChild(bottomRow)

    root.appendChild(section)
}

// ── Helpers ──────────────────────────────────────────────────────────

function specimenCard(title: string, description: string, build: (body: HTMLElement) => void): HTMLElement {
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
    body.style.padding = '12px'
    build(body)
    container.appendChild(body)

    return container
}

function demoButton(label: string, onClick: () => void): HTMLButtonElement {
    const btn = document.createElement('button')
    btn.className = 'glyph-btn'
    btn.textContent = label
    btn.addEventListener('click', onClick)
    btn.style.marginRight = '6px'
    return btn
}

function statusText(): { element: HTMLElement; set: (msg: string) => void } {
    const el = document.createElement('span')
    el.style.fontSize = 'var(--font-size-xs)'
    el.style.color = 'var(--text-on-dark-tertiary)'
    el.style.marginLeft = '8px'
    return {
        element: el,
        set(msg: string) { el.textContent = msg }
    }
}

// ── 1. Proximity Engine ─────────────────────────────────────────────

function proximitySpecimen(section: HTMLElement) {
    section.appendChild(specimenCard(
        'GlyphProximity',
        'Pointer-distance morphing — move cursor toward the dots',
        (body) => {
            // Container simulating a tray
            const tray = document.createElement('div')
            tray.style.position = 'relative'
            tray.style.height = '200px'
            tray.style.border = '1px dashed var(--border-on-dark)'
            tray.style.borderRadius = 'var(--border-radius)'
            tray.style.overflow = 'hidden'

            // Indicator column on the right (like the real tray)
            const indicators = document.createElement('div')
            indicators.style.position = 'absolute'
            indicators.style.right = '4px'
            indicators.style.top = '50%'
            indicators.style.transform = 'translateY(-50%)'
            indicators.style.display = 'flex'
            indicators.style.flexDirection = 'column'
            indicators.style.gap = '2px'
            indicators.style.alignItems = 'flex-end'

            const proximity = new GlyphProximity()
            const items = new Map<string, { title: string; symbol: string }>()

            const glyphNames = [
                { id: 'demo-ax', title: 'AX Query', symbol: '⋈' },
                { id: 'demo-py', title: 'Python', symbol: 'Py' },
                { id: 'demo-ts', title: 'TypeScript', symbol: 'TS' },
                { id: 'demo-note', title: 'Note', symbol: '▣' },
                { id: 'demo-pulse', title: 'Pulse', symbol: '꩜' },
            ]

            for (const g of glyphNames) {
                const dot = document.createElement('div')
                dot.className = 'glyph-run-glyph'
                setGlyphId(dot, g.id)
                setGlyphSymbol(dot, g.symbol)
                indicators.appendChild(dot)
                items.set(g.id, { title: g.title, symbol: g.symbol })
            }

            tray.appendChild(indicators)

            // Track pointer within the tray area
            let rafId = 0
            const update = () => {
                proximity.updateProximity(indicators, items, false)
                rafId = requestAnimationFrame(update)
            }

            tray.addEventListener('mousemove', (e) => {
                const rect = tray.getBoundingClientRect()
                // Map to absolute position relative to the indicator column
                const absX = e.clientX
                const absY = e.clientY
                proximity.setPointerPosition(absX, absY)
            })

            tray.addEventListener('mouseenter', () => {
                rafId = requestAnimationFrame(update)
            })

            tray.addEventListener('mouseleave', () => {
                cancelAnimationFrame(rafId)
                // Reset to far-away position so dots collapse
                proximity.setPointerPosition(-1000, -1000)
                proximity.updateProximity(indicators, items, false)
            })

            // Instructions
            const hint = document.createElement('div')
            hint.style.position = 'absolute'
            hint.style.left = '12px'
            hint.style.top = '50%'
            hint.style.transform = 'translateY(-50%)'
            hint.style.fontSize = 'var(--font-size-xs)'
            hint.style.color = 'var(--text-on-dark-tertiary)'
            hint.textContent = '← Move cursor toward the dots'
            tray.appendChild(hint)

            body.appendChild(tray)
        }
    ))
}

// ── 2. Morph Transactions ───────────────────────────────────────────

function morphSpecimen(section: HTMLElement) {
    section.appendChild(specimenCard(
        'Morph Transactions',
        'Web Animations API with commit/rollback — dot ↔ window',
        (body) => {
            const status = statusText()

            // The ONE element that morphs between states
            const glyphEl = document.createElement('div')
            glyphEl.className = 'glyph-run-glyph'
            glyphEl.style.position = 'relative'
            glyphEl.style.marginBottom = '10px'

            // Demo stage — positioned container for the morph
            const stage = document.createElement('div')
            stage.style.position = 'relative'
            stage.style.height = '250px'
            stage.style.border = '1px dashed var(--border-on-dark)'
            stage.style.borderRadius = 'var(--border-radius)'
            stage.style.overflow = 'visible'

            // Place dot at bottom-right of stage
            glyphEl.style.position = 'absolute'
            glyphEl.style.right = '10px'
            glyphEl.style.bottom = '10px'
            stage.appendChild(glyphEl)

            let isWindow = false
            let morphing = false

            const maximize = async () => {
                if (isWindow || morphing) return
                morphing = true
                status.set('Morphing to window...')

                const fromRect = glyphEl.getBoundingClientRect()

                // Detach from stage, go fixed
                glyphEl.remove()
                glyphEl.style.position = 'fixed'
                glyphEl.style.left = `${fromRect.left}px`
                glyphEl.style.top = `${fromRect.top}px`
                glyphEl.style.width = `${fromRect.width}px`
                glyphEl.style.height = `${fromRect.height}px`
                document.body.appendChild(glyphEl)

                const stageRect = stage.getBoundingClientRect()
                const targetW = 300
                const targetH = 180
                const targetX = stageRect.left + (stageRect.width - targetW) / 2
                const targetY = stageRect.top + (stageRect.height - targetH) / 2

                try {
                    await beginMaximizeMorph(
                        glyphEl, fromRect,
                        { x: targetX, y: targetY, width: targetW, height: targetH },
                        getMaximizeDuration()
                    )

                    // Commit: apply window styles
                    glyphEl.className = ''
                    glyphEl.style.left = `${targetX}px`
                    glyphEl.style.top = `${targetY}px`
                    glyphEl.style.width = `${targetW}px`
                    glyphEl.style.height = `${targetH}px`
                    glyphEl.style.borderRadius = WINDOW_BORDER_RADIUS
                    glyphEl.style.boxShadow = WINDOW_BOX_SHADOW
                    glyphEl.style.backgroundColor = 'var(--bg-almost-black)'
                    glyphEl.style.border = '1px solid var(--border-on-dark)'
                    glyphEl.style.display = 'flex'
                    glyphEl.style.flexDirection = 'column'

                    // Add content
                    const tb = document.createElement('div')
                    tb.className = 'glyph-title-bar'
                    const tbText = document.createElement('span')
                    tbText.style.flex = '1'
                    tbText.textContent = 'Morphed Window'
                    tb.appendChild(tbText)
                    glyphEl.appendChild(tb)

                    const content = document.createElement('div')
                    content.style.padding = '12px'
                    content.style.fontSize = 'var(--font-size-sm)'
                    content.style.color = 'var(--text-on-dark)'
                    content.textContent = 'This element was a 10×10 dot. Same DOM node — no cloning, no recreation.'
                    glyphEl.appendChild(content)

                    isWindow = true
                    status.set('Committed. Click "Minimize" to morph back.')
                } catch {
                    status.set('Morph cancelled or failed')
                }
                morphing = false
            }

            const minimize = async () => {
                if (!isWindow || morphing) return
                morphing = true
                status.set('Morphing back to dot...')

                const fromRect = glyphEl.getBoundingClientRect()

                // Strip content
                glyphEl.innerHTML = ''

                // Target: bottom-right of stage
                const stageRect = stage.getBoundingClientRect()
                const targetX = stageRect.right - 20
                const targetY = stageRect.bottom - 20

                try {
                    await beginMinimizeMorph(
                        glyphEl, fromRect,
                        { x: targetX, y: targetY },
                        getMinimizeDuration()
                    )

                    // Commit: restore dot state
                    glyphEl.remove()
                    glyphEl.style.cssText = ''
                    glyphEl.className = 'glyph-run-glyph'
                    glyphEl.style.position = 'absolute'
                    glyphEl.style.right = '10px'
                    glyphEl.style.bottom = '10px'
                    stage.appendChild(glyphEl)

                    isWindow = false
                    status.set('Rolled back to dot. Same element.')
                } catch {
                    status.set('Minimize failed')
                }
                morphing = false
            }

            const controls = document.createElement('div')
            controls.style.marginTop = '8px'
            controls.style.display = 'flex'
            controls.style.alignItems = 'center'
            controls.appendChild(demoButton('Maximize', maximize))
            controls.appendChild(demoButton('Minimize', minimize))
            controls.appendChild(status.element)

            body.appendChild(stage)
            body.appendChild(controls)
        }
    ))
}

// ── 3. Title Bar Controls ───────────────────────────────────────────

function titleBarSpecimen(section: HTMLElement) {
    section.appendChild(specimenCard(
        'Title Bar Controls',
        'addWindowControls / removeWindowControls',
        (body) => {
            const status = statusText()

            // A title bar to add/remove controls on
            const bar = document.createElement('div')
            bar.className = 'glyph-title-bar'
            bar.style.marginBottom = '8px'

            const titleText = document.createElement('span')
            titleText.style.flex = '1'
            titleText.textContent = 'Sample Glyph'
            bar.appendChild(titleText)

            let hasControls = false

            const controls = document.createElement('div')
            controls.style.display = 'flex'
            controls.style.alignItems = 'center'

            controls.appendChild(demoButton('Add Controls', () => {
                if (hasControls) return
                addWindowControls(bar, {
                    onMinimize: () => status.set('Minimize clicked'),
                    onClose: () => status.set('Close clicked'),
                })
                hasControls = true
                status.set('Controls added (−  ×)')
            }))

            controls.appendChild(demoButton('Remove Controls', () => {
                if (!hasControls) return
                removeWindowControls(bar)
                hasControls = false
                status.set('Controls removed')
            }))

            controls.appendChild(status.element)

            body.appendChild(bar)
            body.appendChild(controls)
        }
    ))
}

// ── 4. Stash / Restore ──────────────────────────────────────────────

function stashSpecimen(section: HTMLElement) {
    section.appendChild(specimenCard(
        'Stash / Restore',
        'DOM content preserved across morph cycles via WeakMap',
        (body) => {
            const status = statusText()

            // A mock window element with real interactive content
            const win = document.createElement('div')
            win.style.border = '1px solid var(--border-on-dark)'
            win.style.borderRadius = 'var(--border-radius)'
            win.style.overflow = 'hidden'
            win.style.marginBottom = '8px'

            const tb = document.createElement('div')
            tb.className = 'glyph-title-bar'
            const tbText = document.createElement('span')
            tbText.style.flex = '1'
            tbText.textContent = 'Stash Demo'
            tb.appendChild(tbText)

            // Add window controls so stash can strip them
            addWindowControls(tb, {
                onMinimize: () => {},
                onClose: () => {},
            })

            win.appendChild(tb)

            const content = document.createElement('div')
            content.style.padding = '12px'

            const input = document.createElement('input')
            input.type = 'text'
            input.placeholder = 'Type something, then stash...'
            input.style.width = '100%'
            input.style.padding = '4px 8px'
            input.style.marginBottom = '8px'
            input.style.backgroundColor = 'var(--bg-tertiary)'
            input.style.border = '1px solid var(--border-on-dark)'
            input.style.borderRadius = 'var(--border-radius)'
            input.style.color = 'var(--text-on-dark)'
            input.style.fontFamily = 'var(--font-mono)'
            input.style.fontSize = 'var(--font-size-sm)'
            content.appendChild(input)

            const note = document.createElement('div')
            note.style.fontSize = 'var(--font-size-xs)'
            note.style.color = 'var(--text-on-dark-tertiary)'
            note.textContent = 'Input value, scroll position, and DOM identity survive the stash/restore cycle. Window controls are stripped on stash and must be re-added.'
            content.appendChild(note)

            win.appendChild(content)

            const controls = document.createElement('div')
            controls.style.display = 'flex'
            controls.style.alignItems = 'center'

            controls.appendChild(demoButton('Stash', () => {
                if (hasStash(win)) {
                    status.set('Already stashed')
                    return
                }
                stashContent(win)
                status.set(`Stashed — element has ${win.childNodes.length} children (should be 0), hasStash=${hasStash(win)}`)
            }))

            controls.appendChild(demoButton('Restore', () => {
                if (!hasStash(win)) {
                    status.set('Nothing to restore')
                    return
                }
                restoreContent(win)
                status.set(`Restored — element has ${win.childNodes.length} children, input value preserved`)
            }))

            controls.appendChild(status.element)

            body.appendChild(win)
            body.appendChild(controls)
        }
    ))
}
