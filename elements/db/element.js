/**
 * DB — what the record cost, drawn.
 *
 * It was web/ts/db-element.ts, which drew distillation sigmas, eviction
 * breakdowns, WAL bytes and a write-lock holder. ADR-024 took all of those
 * away: "No distillation, no bounded-storage enforcement", and a parquet node
 * holds no WAL. The old panel kept asking for them and a parquet node kept
 * answering with a payload that had none, which is what made it look dead.
 *
 * What it draws now is what the record actually has, and the chart is the old
 * one: a line per series, a legend carrying totals, the top few and no more.
 *
 * The numbers are not on this box. internal/measure sends them to Sentry and
 * a gauge is a number in flight, not a table, so /api/db/series asks Sentry
 * for them back. That route holds the credential; this module never sees one.
 *
 * Publish with:
 *   qntx element publish db --file elements/db/element.js
 */

export const elementDef = {
  symbol: '⊔',
  title: 'Database',
  label: 'DB',
  defaultWidth: 640,
  defaultHeight: 420,
}

/**
 * What can be drawn. Each is one of the numbers internal/measure names, and
 * the label says what a reading of it means rather than repeating its name.
 *
 * `by` is the attribute the lines split on. `counts` is what sampleCount means
 * for this number — for a merge it is how many ran, where the value is how
 * many files they took, and that second reading is free and was never on the
 * old panel.
 */
const DRAWABLE = [
  {
    metric: 'qntx.store.requests',
    label: 'Requests to the location',
    by: 'request',
    counts: null,
  },
  {
    metric: 'qntx.store.compacted.files',
    label: 'Files a merge replaced',
    by: 'store',
    counts: 'merges',
  },
  {
    metric: 'qntx.store.files',
    label: 'Files the record holds',
    by: 'store',
    counts: null,
  },
  {
    metric: 'qntx.store.unsent',
    label: 'Rows the record does not have yet',
    by: 'store',
    counts: null,
  },
  {
    metric: 'qntx.store.sent.rows',
    label: 'Rows a send carried',
    by: 'store',
    counts: 'sends',
  },
]

const WINDOWS = [
  { since: '24h', every: '1h' },
  { since: '7d', every: '1d' },
  { since: '14d', every: '1d' },
]

// The palette the old panel used, kept so a predicate that was green stays
// green for anyone who has been looking at this screen for a year.
const COLOURS = [
  '#4ade80',
  '#60a5fa',
  '#f59e0b',
  '#ef4444',
  '#a78bfa',
  '#f472b6',
  '#2dd4bf',
  '#fb923c',
  '#818cf8',
  '#34d399',
]

/** What one line is of, in a word. Ungrouped is the whole number. */
function nameOf(line) {
  const by = Object.values(line.by ?? {})
  return by.length > 0 ? by.join(' · ') : 'all'
}

function totalOf(line) {
  let sum = 0
  for (const reading of line.readings) sum += reading.value
  return sum
}

function samplesOf(line) {
  let sum = 0
  for (const reading of line.readings) sum += reading.samples
  return sum
}

/**
 * The error as the node sent it: what failed, where, every detail and hint the
 * chain carried, and the id it is logged under. Carried over from the old
 * panel unchanged — a 503 here names the config key that is unset, and
 * summarising it would send the reader to the wrong place.
 */
function drawRefusal(into, envelope) {
  const rows = [
    row('element-error', `${envelope.surface ?? 'database series'}: ${envelope.error ?? 'failed'}`),
  ]
  for (const detail of envelope.details ?? []) rows.push(row('element-error-detail', detail))
  for (const hint of envelope.hints ?? []) rows.push(row('element-error-hint', hint))
  if (envelope.id) rows.push(row('element-error-id', envelope.id))

  into.replaceChildren(...rows)
}

function row(className, text) {
  const line = document.createElement('div')
  line.className = className
  line.textContent = text
  return line
}

/**
 * A line per series, over one shared time axis.
 *
 * Built as SVG through the DOM rather than as markup, because a series name is
 * an attribute value the node was given and the old panel put those straight
 * into innerHTML.
 */
function drawChart(into, answer) {
  const lines = (answer.lines ?? []).filter((line) => line.readings.length > 0)
  if (lines.length === 0) {
    into.replaceChildren(row('element-loading', `Nothing recorded for ${answer.metric} in the last ${answer.since}`))
    return
  }

  const when = [...new Set(lines.flatMap((line) => line.readings.map((r) => r.at)))].sort((a, b) => a - b)
  const place = new Map(when.map((at, i) => [at, i]))

  let highest = 0
  for (const line of lines) {
    for (const reading of line.readings) {
      if (reading.value > highest) highest = reading.value
    }
  }
  if (highest === 0) highest = 1

  const width = 640
  const height = 200
  const left = 52
  const right = 12
  const top = 8
  const bottom = 22
  const plotWidth = width - left - right
  const plotHeight = height - top - bottom

  const x = (at) => left + (place.get(at) / Math.max(1, when.length - 1)) * plotWidth
  const y = (value) => top + plotHeight - (value / highest) * plotHeight

  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`)
  svg.style.width = '100%'
  svg.style.height = `${height}px`

  for (let i = 0; i <= 4; i++) {
    const value = (highest / 4) * i
    const at = y(value)
    svg.appendChild(svgNode('line', { x1: left, y1: at, x2: left + plotWidth, y2: at, stroke: '#1e293b', 'stroke-width': 0.5 }))
    const label = svgNode('text', { x: left - 4, y: at, 'text-anchor': 'end', 'dominant-baseline': 'middle', fill: '#64748b', 'font-size': 9 })
    label.textContent = value >= 1000 ? `${(value / 1000).toFixed(1)}k` : Math.round(value).toString()
    svg.appendChild(label)
  }

  const step = Math.max(1, Math.floor(when.length / 8))
  for (let i = 0; i < when.length; i += step) {
    const label = svgNode('text', { x: x(when[i]), y: height - 4, 'text-anchor': 'middle', fill: '#64748b', 'font-size': 9 })
    label.textContent = clockOf(when[i], answer.every)
    svg.appendChild(label)
  }

  lines.forEach((line, i) => {
    const drawn = line.readings
      .slice()
      .sort((a, b) => a.at - b.at)
      .map((reading, n) => `${n === 0 ? 'M' : 'L'}${x(reading.at)},${y(reading.value)}`)
      .join(' ')
    svg.appendChild(svgNode('path', { d: drawn, fill: 'none', stroke: COLOURS[i % COLOURS.length], 'stroke-width': 1.5, opacity: 0.85 }))
  })

  const legend = document.createElement('div')
  legend.style.cssText = 'display: flex; flex-wrap: wrap; gap: 12px; padding: 6px 0; font-size: var(--font-size-sm);'
  lines.forEach((line, i) => {
    const entry = document.createElement('span')
    entry.style.cssText = 'display: inline-flex; align-items: center; gap: 4px;'

    const dot = document.createElement('span')
    dot.style.cssText = `width: 8px; height: 8px; border-radius: 50%; background: ${COLOURS[i % COLOURS.length]};`

    const name = document.createElement('span')
    name.style.color = '#e2e8f0'
    name.textContent = nameOf(line)

    const total = document.createElement('span')
    total.style.color = '#64748b'
    const counted = answer.counts ? ` over ${samplesOf(line).toLocaleString()} ${answer.counts}` : ''
    total.textContent = `${Math.round(totalOf(line)).toLocaleString()}${counted}`

    entry.append(dot, name, total)
    legend.appendChild(entry)
  })

  into.replaceChildren(svg, legend)
}

function svgNode(kind, attributes) {
  const node = document.createElementNS('http://www.w3.org/2000/svg', kind)
  for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, String(value))
  return node
}

/** An hourly bucket is a clock; a daily one is a date. */
function clockOf(at, every) {
  const when = new Date(at)
  const two = (n) => String(n).padStart(2, '0')
  if (every.endsWith('d')) return `${two(when.getMonth() + 1)}-${two(when.getDate())}`
  return `${two(when.getHours())}:${two(when.getMinutes())}`
}

export const render = (item, ui) => {
  const { element, content } = ui.element({
    defaults: {
      x: item.x ?? 200,
      y: item.y ?? 200,
      width: item.width ?? elementDef.defaultWidth,
      height: item.height ?? elementDef.defaultHeight,
    },
    titleBar: { label: 'Database' },
    resizable: { minWidth: 380, minHeight: 260 },
  })

  let drawing = DRAWABLE[0]
  let window_ = WINDOWS[0]

  const controls = document.createElement('div')
  controls.style.cssText = 'display: flex; gap: 8px; align-items: center; padding: 4px 0; flex-wrap: wrap;'

  const chart = document.createElement('div')
  const note = document.createElement('div')
  note.style.cssText = 'font-size: var(--font-size-xs); color: #475569; padding: 2px 0;'

  const whichNumber = document.createElement('select')
  for (const one of DRAWABLE) {
    const option = document.createElement('option')
    option.value = one.metric
    option.textContent = one.label
    whichNumber.appendChild(option)
  }

  const whichWindow = document.createElement('select')
  for (const one of WINDOWS) {
    const option = document.createElement('option')
    option.value = one.since
    option.textContent = one.since
    whichWindow.appendChild(option)
  }

  ui.preventDrag(whichNumber, whichWindow)
  controls.append(whichNumber, whichWindow)

  let asking = 0
  const draw = async () => {
    // Every ask is numbered, so an answer to a question the reader has already
    // moved on from is dropped rather than drawn over the one they asked for.
    const mine = ++asking
    chart.replaceChildren(row('element-loading', `Asking for ${drawing.label.toLowerCase()}…`))

    const path =
      `/series?metric=${encodeURIComponent(drawing.metric)}` +
      `&by=${encodeURIComponent(drawing.by)}` +
      `&since=${encodeURIComponent(window_.since)}` +
      `&every=${encodeURIComponent(window_.every)}`

    let answered
    try {
      answered = await ui.pluginFetch(path)
    } catch (err) {
      if (mine !== asking) return
      ui.log.error(`${drawing.metric} could not be asked for:`, err)
      drawRefusal(chart, { surface: 'database series', error: String(err) })
      return
    }
    if (mine !== asking) return

    const body = await answered.json().catch(() => null)
    if (mine !== asking) return

    if (!answered.ok) {
      // The route answers 503 with the config key that is unset, which is the
      // difference between "no traffic" and "nothing is measuring". Drawn as
      // the refusal it is, never as an empty axis.
      drawRefusal(chart, body ?? { surface: 'database series', error: `the node answered ${answered.status}` })
      return
    }

    drawChart(chart, { ...body, counts: drawing.counts })
    note.textContent = `${body.metric} · ${body.since} in ${body.every} buckets · split by ${drawing.by}`
    ui.log.debug(`drew ${body.lines?.length ?? 0} lines of ${body.metric}`)
  }

  whichNumber.addEventListener('change', () => {
    drawing = DRAWABLE.find((one) => one.metric === whichNumber.value) ?? DRAWABLE[0]
    void draw()
  })
  whichWindow.addEventListener('change', () => {
    window_ = WINDOWS.find((one) => one.since === whichWindow.value) ?? WINDOWS[0]
    void draw()
  })

  content.append(controls, chart, note)
  void draw()

  return element
}
