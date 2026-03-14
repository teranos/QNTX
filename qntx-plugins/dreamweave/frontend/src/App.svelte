<script lang="ts">
  import { onMount } from 'svelte'

  // --- Types ---

  interface Weave {
    id: string
    branch: string
    context: string
    timestamp: number
    text: string | null
    word_count: number | null
    turn_count: number | null
  }

  interface Turn {
    speaker: string
    text: string
    weaveId: string
    index: number
    timestamp: number
    branch: string
  }

  interface Session {
    context: string
    weaves: Weave[]
    turns: Turn[]
    branches: string[]
    earliest: number
    latest: number
    totalWords: number
  }

  // --- State ---

  let sessions: Session[] = $state([])
  let loading = $state(true)
  let error = $state('')
  let selectedTurns: Set<string> = $state(new Set())
  let totalWeaves = $state(0)
  let mobileIdx = $state(0)
  let showClusters = $state(true)
  let clusterMap: Map<string, { cluster_id: number, label: string | null }> = $state(new Map())

  // --- Branch colors (deterministic, functional) ---

  const BRANCH_COLORS = [
    '#7dba8a', '#6b9bd1', '#d4b8ff', '#ffab00',
    '#ef4544', '#22c65e', '#3b83f6', '#7b20a2',
    '#e08050', '#50b0b0', '#c0a040', '#d06090',
  ]

  function branchColor(branch: string): string {
    let h = 0
    for (let i = 0; i < branch.length; i++) h = ((h << 5) - h + branch.charCodeAt(i)) | 0
    return BRANCH_COLORS[Math.abs(h) % BRANCH_COLORS.length]
  }

  // --- Cluster colors (low-opacity backgrounds, deterministic by cluster_id) ---

  const CLUSTER_COLORS = [
    'rgba(125,186,138,0.12)', 'rgba(107,155,209,0.12)', 'rgba(212,184,255,0.12)',
    'rgba(255,171,0,0.12)', 'rgba(239,69,68,0.12)', 'rgba(34,198,94,0.12)',
    'rgba(59,131,246,0.12)', 'rgba(123,32,162,0.12)', 'rgba(224,128,80,0.12)',
    'rgba(80,176,176,0.12)', 'rgba(192,160,64,0.12)', 'rgba(208,96,144,0.12)',
  ]

  function clusterBg(weaveId: string): string {
    const entry = clusterMap.get(weaveId)
    if (!entry) return 'transparent'
    return CLUSTER_COLORS[entry.cluster_id % CLUSTER_COLORS.length]
  }

  function clusterLabel(weaveId: string): string | null {
    const entry = clusterMap.get(weaveId)
    return entry?.label || null
  }

  async function loadClusters(weaveIds: string[]) {
    if (weaveIds.length === 0) return
    try {
      const next = new Map(clusterMap)
      // Batch into chunks of 50 to avoid URL length limits
      const chunkSize = 50
      for (let i = 0; i < weaveIds.length; i += chunkSize) {
        const chunk = weaveIds.slice(i, i + chunkSize)
        const res = await fetch('/qntx/embeddings/clusters/memberships?ids=' + chunk.join(','))
        const data = await res.json()
        for (const [id, m] of Object.entries(data.memberships as Record<string, { cluster_id: number, label: string | null }>)) {
          next.set(id, m)
        }
      }
      clusterMap = next
    } catch {
      // Clusters unavailable — silently degrade
    }
  }

  // --- Minimal markdown rendering (string methods only, no regex) ---

  function escapeHtml(s: string): string {
    return s.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  }

  function renderText(text: string): string {
    const lines = text.split('\n')
    let out = ''
    let inCode = false
    for (const line of lines) {
      if (line.startsWith('```')) {
        if (inCode) {
          out += '</code></pre>'
          inCode = false
        } else {
          inCode = true
          out += '<pre class="dw-code"><code>'
        }
        continue
      }
      if (inCode) {
        out += escapeHtml(line) + '\n'
        continue
      }
      // Bold: **text**
      let rendered = escapeHtml(line)
      let result = ''
      let pos = 0
      while (pos < rendered.length) {
        const start = rendered.indexOf('**', pos)
        if (start < 0) { result += rendered.substring(pos); break }
        const end = rendered.indexOf('**', start + 2)
        if (end < 0) { result += rendered.substring(pos); break }
        result += rendered.substring(pos, start) + '<b>' + rendered.substring(start + 2, end) + '</b>'
        pos = end + 2
      }
      // Inline code: `code`
      let final = ''
      pos = 0
      while (pos < result.length) {
        const start = result.indexOf('`', pos)
        if (start < 0) { final += result.substring(pos); break }
        const end = result.indexOf('`', start + 1)
        if (end < 0) { final += result.substring(pos); break }
        final += result.substring(pos, start) + '<code class="dw-inline-code">' + result.substring(start + 1, end) + '</code>'
        pos = end + 1
      }
      out += final + '\n'
    }
    if (inCode) out += '</code></pre>'
    return out
  }

  // --- Parse weave text into turns ---

  const SPEAKERS = ['human', 'assistant', 'tool', 'session', 'compaction', 'agent', 'task']

  function isSpeakerLine(s: string): boolean {
    if (s[0] !== '[') return false
    const end = s.indexOf('] ')
    if (end < 1) return false
    return SPEAKERS.includes(s.substring(1, end))
  }

  function parseTurns(weave: Weave): Turn[] {
    if (!weave.text) return []
    const parts = weave.text.split('\n\n')
    // Re-join parts that don't start with a speaker label onto the previous chunk
    const chunks: string[] = []
    for (const part of parts) {
      if (isSpeakerLine(part) || chunks.length === 0) {
        chunks.push(part)
      } else {
        chunks[chunks.length - 1] += '\n\n' + part
      }
    }
    const turns: Turn[] = []
    for (let i = 0; i < chunks.length; i++) {
      const chunk = chunks[i].trim()
      if (!chunk) continue
      const end = chunk.indexOf('] ')
      if (chunk[0] === '[' && end > 0) {
        turns.push({
          speaker: chunk.substring(1, end),
          text: chunk.substring(end + 2),
          weaveId: weave.id,
          index: i,
          timestamp: weave.timestamp,
          branch: weave.branch,
        })
      } else {
        turns.push({
          speaker: 'unknown',
          text: chunk,
          weaveId: weave.id,
          index: i,
          timestamp: weave.timestamp,
          branch: weave.branch,
        })
      }
    }
    return turns
  }

  // --- Selection ---

  function turnKey(t: Turn): string {
    return `${t.weaveId}:${t.index}`
  }

  function toggle(turn: Turn) {
    const k = turnKey(turn)
    const next = new Set(selectedTurns)
    if (next.has(k)) next.delete(k)
    else next.add(k)
    selectedTurns = next
  }

  function clearSelection() {
    selectedTurns = new Set()
  }

  function copySelected() {
    if (selectedTurns.size === 0) return
    const all: Turn[] = []
    for (const s of sessions) {
      for (const t of s.turns) {
        if (selectedTurns.has(turnKey(t))) all.push(t)
      }
    }
    all.sort((a, b) => a.timestamp - b.timestamp || a.index - b.index)
    const text = all.map(t => `[${t.speaker}] ${t.text}`).join('\n\n')
    navigator.clipboard.writeText(text)
  }

  // --- Time formatting ---

  function fmtTime(ts: number): string {
    const d = new Date(ts)
    const mon = d.toLocaleString('en', { month: 'short' })
    const day = d.getDate()
    const hh = String(d.getHours()).padStart(2, '0')
    const mm = String(d.getMinutes()).padStart(2, '0')
    return `${mon} ${day} ${hh}:${mm}`
  }

  // --- Fetch data ---

  async function load() {
    try {
      const res = await fetch('/api/weaves')
      const data = await res.json()
      totalWeaves = data.total_weaves

      // Flatten all weaves, filter out pre-standard (no context or bare branch)
      const all: Weave[] = []
      for (const branch of Object.keys(data.branches)) {
        for (const w of data.branches[branch]) {
          if ((!w.context || w.context === '_') || !w.branch.includes(':')) continue
          all.push(w)
        }
      }

      // Group by project (part before ':' in branch name)
      const map = new Map<string, Weave[]>()
      for (const w of all) {
        const sep = w.branch.indexOf(':')
        const project = sep > 0 ? w.branch.substring(0, sep) : w.branch
        if (!map.has(project)) map.set(project, [])
        map.get(project)!.push(w)
      }

      // Build sessions (one per project)
      const built: Session[] = []
      for (const [project, weaves] of map) {
        weaves.sort((a, b) => a.timestamp - b.timestamp)
        const turns: Turn[] = []
        for (const w of weaves) turns.push(...parseTurns(w))
        const branchSet = new Set(weaves.map(w => w.branch))
        built.push({
          context: project,
          weaves,
          turns,
          branches: [...branchSet].sort(),
          earliest: weaves[0].timestamp,
          latest: weaves[weaves.length - 1].timestamp,
          totalWords: weaves.reduce((s, w) => s + (w.word_count || 0), 0),
        })
      }

      built.sort((a, b) => a.earliest - b.earliest)
      sessions = built
      loading = false

      // Fetch cluster memberships for all weave IDs
      const allIds = all.map(w => w.id)
      loadClusters(allIds)

      // Init minimaps after DOM renders
      requestAnimationFrame(initMinimaps)
    } catch (e: any) {
      error = e.message || 'fetch failed'
      loading = false
    }
  }

  // --- Time-synchronized scrolling ---

  let columnEls: HTMLElement[] = []
  let hoverIdx: number = -1
  let scrollTimer: ReturnType<typeof setTimeout> | null = null

  function onColumnEnter(e: Event) {
    const el = e.currentTarget as HTMLElement
    hoverIdx = columnEls.indexOf(el)
  }

  function onColumnLeave() {
    hoverIdx = -1
  }

  function onColumnScroll(e: Event) {
    const source = e.target as HTMLElement
    const sourceIdx = columnEls.indexOf(source)
    if (sourceIdx < 0) return

    // Only the column under the pointer can drive
    if (sourceIdx !== hoverIdx) return

    if (scrollTimer) clearTimeout(scrollTimer)
    scrollTimer = setTimeout(() => syncColumns(sourceIdx), 50)
  }

  function syncColumns(sourceIdx: number) {
    const source = columnEls[sourceIdx]
    if (!source || sourceIdx !== hoverIdx) return

    // Find the weave element at the vertical center of the source column
    const centerY = source.getBoundingClientRect().top + source.clientHeight / 2
    const weaves = source.querySelectorAll('[data-ts]')
    let centerTs = 0
    let minDist = Infinity
    for (let i = 0; i < weaves.length; i++) {
      const rect = weaves[i].getBoundingClientRect()
      const dist = Math.abs(rect.top + rect.height / 2 - centerY)
      if (dist < minDist) {
        minDist = dist
        centerTs = Number((weaves[i] as HTMLElement).dataset.ts)
      }
    }
    if (!centerTs) return

    // Scroll all other columns to their closest-timestamp weave
    for (let i = 0; i < columnEls.length; i++) {
      if (i === sourceIdx || !columnEls[i]) continue
      const col = columnEls[i]
      const others = col.querySelectorAll('[data-ts]')
      let best: Element | null = null
      let bestDist = Infinity
      for (let j = 0; j < others.length; j++) {
        const ts = Number((others[j] as HTMLElement).dataset.ts)
        const d = Math.abs(ts - centerTs)
        if (d < bestDist) {
          bestDist = d
          best = others[j]
        }
      }
      if (best) {
        const colRect = col.getBoundingClientRect()
        const bestRect = best.getBoundingClientRect()
        const offset = bestRect.top - colRect.top - col.clientHeight / 2 + bestRect.height / 2
        col.scrollTo({ top: col.scrollTop + offset, behavior: 'smooth' })
      }
    }
  }

  function bindColumn(el: HTMLElement, idx: number) {
    columnEls[idx] = el
    return {
      destroy() { columnEls[idx] = null as any }
    }
  }

  // --- Custom minimap scrollbar ---

  let minimapStates: { viewTop: number, viewHeight: number }[] = $state([])

  function updateMinimap(colIdx: number) {
    const col = columnEls[colIdx]
    if (!col) return
    const ratio = col.clientHeight / col.scrollHeight
    const viewTop = (col.scrollTop / col.scrollHeight) * 100
    const viewHeight = ratio * 100
    if (!minimapStates[colIdx]) minimapStates[colIdx] = { viewTop: 0, viewHeight: 100 }
    minimapStates[colIdx] = { viewTop, viewHeight }
  }

  function onMinimapClick(e: MouseEvent, colIdx: number) {
    const col = columnEls[colIdx]
    const minimap = (e.currentTarget as HTMLElement)
    if (!col || !minimap) return
    const rect = minimap.getBoundingClientRect()
    const fraction = (e.clientY - rect.top) / rect.height
    col.scrollTop = fraction * col.scrollHeight - col.clientHeight / 2
  }

  let dragCol: number = -1

  function onMinimapPointerDown(e: PointerEvent, colIdx: number) {
    dragCol = colIdx
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    onMinimapDrag(e, colIdx)
  }

  function onMinimapDrag(e: PointerEvent, colIdx: number) {
    if (dragCol !== colIdx) return
    const col = columnEls[colIdx]
    const minimap = (e.currentTarget as HTMLElement)
    if (!col || !minimap) return
    const rect = minimap.getBoundingClientRect()
    const fraction = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height))
    col.scrollTop = fraction * col.scrollHeight - col.clientHeight / 2
  }

  function onMinimapPointerUp() {
    dragCol = -1
  }

  function weaveMinimapColor(weave: Weave): string {
    if (showClusters) {
      const entry = clusterMap.get(weave.id)
      if (entry) return BRANCH_COLORS[entry.cluster_id % BRANCH_COLORS.length]
    }
    return branchColor(weave.branch)
  }

  // Update minimaps on column scroll
  function onColumnScrollWithMinimap(e: Event) {
    onColumnScroll(e)
    const source = e.target as HTMLElement
    const idx = columnEls.indexOf(source)
    if (idx >= 0) updateMinimap(idx)
  }

  // Initialize minimaps after load
  function initMinimaps() {
    for (let i = 0; i < sessions.length; i++) {
      requestAnimationFrame(() => updateMinimap(i))
    }
  }

  // --- Touch swipe for mobile ---

  let touchX = 0

  function onTouchStart(e: TouchEvent) {
    touchX = e.touches[0].clientX
  }

  function onTouchEnd(e: TouchEvent) {
    const dx = e.changedTouches[0].clientX - touchX
    if (Math.abs(dx) > 60) {
      if (dx < 0 && mobileIdx < sessions.length - 1) mobileIdx++
      else if (dx > 0 && mobileIdx > 0) mobileIdx--
    }
  }

  // --- Keyboard ---

  function onKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'c' && selectedTurns.size > 0) {
      e.preventDefault()
      copySelected()
    }
    if (e.key === 'Escape') clearSelection()
  }

  onMount(() => {
    load()
    document.addEventListener('keydown', onKeydown)
    return () => document.removeEventListener('keydown', onKeydown)
  })
</script>

<div class="dw">
  <header>
    <span class="dw-title">dreamweave</span>
    {#if !loading}
      <span class="dw-stat">{sessions.length} sessions</span>
      <span class="dw-stat">{totalWeaves} weaves</span>
    {/if}
    <button class="dw-toggle" class:active={showClusters} onclick={() => showClusters = !showClusters}>clusters</button>
    {#if selectedTurns.size > 0}
      <span class="dw-sel">
        {selectedTurns.size} selected
        <button onclick={copySelected}>copy</button>
        <button onclick={clearSelection}>clear</button>
      </span>
    {/if}
  </header>

  {#if loading}
    <div class="dw-msg">loading...</div>
  {:else if error}
    <div class="dw-msg dw-err">{error}</div>
  {:else}
    <nav class="dw-dots mobile-only">
      {#each sessions as _, i}
        <button
          class="dw-dot"
          class:active={i === mobileIdx}
          onclick={() => mobileIdx = i}
          aria-label="Session {i + 1}"
        ></button>
      {/each}
    </nav>

    <div
      class="dw-timeline"
      ontouchstart={onTouchStart}
      ontouchend={onTouchEnd}
    >
      {#each sessions as session, si}
        <div class="dw-col-wrap" class:dw-hidden={si !== mobileIdx}>
        <div
          class="dw-col"
          use:bindColumn={si}
          onscroll={onColumnScrollWithMinimap}
          onpointerenter={onColumnEnter}
          onpointerleave={onColumnLeave}
        >
          <div class="dw-session-hd">
            <div class="dw-session-top">
              <span class="dw-sid">{session.context === '_' ? 'untracked' : session.context.substring(0, 16)}</span>
              <span class="dw-smeta">{fmtTime(session.earliest)}</span>
              <span class="dw-smeta">{session.weaves.length}w {session.totalWords} words</span>
            </div>
            <div class="dw-branches">
              {#each session.branches as branch}
                <span class="dw-branch" style="border-left-color: {branchColor(branch)}">{branch}</span>
              {/each}
            </div>
          </div>

          <div class="dw-stream">
            {#each session.weaves as weave}
              {@const wturns = parseTurns(weave)}
              <div class="dw-weave" data-ts={weave.timestamp}>
                <div class="dw-wmeta">
                  <span class="dw-copyable" onclick={(e) => { e.stopPropagation(); navigator.clipboard.writeText(weave.id) }}>{weave.branch}</span>
                  {#if showClusters && clusterLabel(weave.id)}
                    <span class="dw-cluster">{clusterLabel(weave.id)}</span>
                  {/if}
                  <span>{fmtTime(weave.timestamp)}</span>
                  <span>{weave.word_count}w {weave.turn_count}t</span>
                </div>
                {#each wturns as turn}
                  {@const k = turnKey(turn)}
                  <div
                    class="dw-turn"
                    class:selected={selectedTurns.has(k)}
                    class:human={turn.speaker === 'human'}
                    class:assistant={turn.speaker === 'assistant'}
                    class:tool={turn.speaker === 'tool'}
                    class:marker={turn.speaker === 'session' || turn.speaker === 'compaction' || turn.speaker === 'agent' || turn.speaker === 'task'}
                    onclick={() => toggle(turn)}
                    role="button"
                    tabindex="0"
                    onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(turn) }}}
                  >
                    <span class="dw-speaker">[{turn.speaker}]</span>
                    {#if turn.speaker === 'assistant'}
                      <span class="dw-text">{@html renderText(turn.text)}</span>
                    {:else}
                      <span class="dw-text">{turn.text}</span>
                    {/if}
                  </div>
                {/each}
              </div>
            {/each}
          </div>
        </div>

        <div
          class="dw-minimap"
          onpointerdown={(e) => onMinimapPointerDown(e, si)}
          onpointermove={(e) => onMinimapDrag(e, si)}
          onpointerup={onMinimapPointerUp}
        >
          <div class="dw-minimap-lane">
            {#each session.weaves as weave}
              <div class="dw-minimap-seg" style="height: {100 / session.weaves.length}%; background: {branchColor(weave.branch)}"></div>
            {/each}
          </div>
          <div class="dw-minimap-lane">
            {#each session.weaves as weave}
              {@const entry = clusterMap.get(weave.id)}
              <div class="dw-minimap-seg" style="height: {100 / session.weaves.length}%; background: {entry ? BRANCH_COLORS[entry.cluster_id % BRANCH_COLORS.length] : '#252625'}"></div>
            {/each}
          </div>
          {#if minimapStates[si]}
            <div
              class="dw-minimap-view"
              style="top: {minimapStates[si].viewTop}%; height: {minimapStates[si].viewHeight}%"
            ></div>
          {/if}
        </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@300;400;500&display=swap');

  :global(*) { box-sizing: border-box; margin: 0; padding: 0; }

  :global(body) {
    background: #2d2e36;
    color: #dfe1e0;
    font-family: 'JetBrains Mono', monospace;
    font-size: 12px;
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }

  .dw {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  /* Header */
  header {
    position: sticky;
    top: 0;
    z-index: 10;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 12px;
    background: #1a1b1a;
    border-bottom: 1px solid #3f4140;
  }

  .dw-title {
    color: #7dba8a;
    font-weight: 500;
    font-size: 13px;
  }

  .dw-stat { color: #878988; font-size: 11px; }

  .dw-toggle {
    background: #343534;
    color: #656766;
    border: 1px solid #3f4140;
    padding: 1px 6px;
    font: inherit;
    font-size: 11px;
    cursor: pointer;
  }
  .dw-toggle:hover { background: #3f4140; }
  .dw-toggle.active { color: #7dba8a; border-color: #7dba8a; }

  .dw-cluster { color: #878988; font-style: italic; }

  .dw-sel {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 6px;
    color: #dfe1e0;
    font-size: 11px;
  }

  .dw-sel button {
    background: #343534;
    color: #dfe1e0;
    border: 1px solid #3f4140;
    padding: 1px 6px;
    font: inherit;
    font-size: 11px;
    cursor: pointer;
  }
  .dw-sel button:hover { background: #3f4140; }

  /* Status messages */
  .dw-msg { padding: 24px; color: #878988; text-align: center; }
  .dw-err { color: #ef4544; }

  /* Mobile session dots */
  .dw-dots {
    display: flex;
    justify-content: center;
    gap: 6px;
    padding: 6px;
    background: #1a1b1a;
    border-bottom: 1px solid #3f4140;
  }

  .dw-dot {
    width: 6px; height: 6px;
    border: 1px solid #3f4140;
    background: transparent;
    padding: 0;
    cursor: pointer;
  }
  .dw-dot.active { background: #7dba8a; border-color: #7dba8a; }

  /* Timeline container */
  .dw-timeline {
    display: flex;
    flex: 1;
    overflow-x: auto;
    overflow-y: hidden;
  }

  /* Column wrapper */
  .dw-col-wrap {
    display: flex;
    flex: 0 0 100%;
    border-right: 1px solid #3f4140;
  }

  /* Session column */
  .dw-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    height: calc(100vh - 33px);
    scrollbar-width: none;
  }
  .dw-col::-webkit-scrollbar { display: none; }

  /* Minimap scrollbar */
  .dw-minimap {
    width: 24px;
    height: calc(100vh - 33px);
    background: #1a1b1a;
    display: flex;
    flex-direction: row;
    position: relative;
    cursor: pointer;
    flex-shrink: 0;
    border-left: 1px solid #252625;
  }
  .dw-minimap-lane {
    width: 12px;
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .dw-minimap-seg {
    flex-shrink: 0;
    opacity: 0.7;
  }
  .dw-minimap-view {
    position: absolute;
    left: 0;
    width: 100%;
    border: 1px solid #7dba8a;
    background: rgba(125,186,138,0.08);
    pointer-events: none;
  }

  /* Session header */
  .dw-session-hd {
    position: sticky;
    top: 0;
    z-index: 5;
    padding: 6px 10px;
    background: #252625;
    border-bottom: 1px solid #3f4140;
  }

  .dw-session-top {
    display: flex;
    gap: 8px;
    align-items: baseline;
  }

  .dw-sid { color: #a9abaa; font-size: 11px; font-weight: 500; }
  .dw-smeta { color: #656766; font-size: 10px; }

  .dw-branches {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }

  .dw-branch {
    font-size: 10px;
    color: #878988;
    border-left: 2px solid;
    padding-left: 4px;
    overflow-wrap: break-word;
    word-break: break-word;
  }

  /* Weave stream */
  .dw-stream { padding: 2px 0; flex: 1; }

  .dw-weave {
    border-left: 3px solid #3f4140;
    margin: 0 6px 1px 6px;
    padding: 3px 6px;
  }

  .dw-wmeta {
    display: flex;
    gap: 8px;
    color: #656766;
    font-size: 10px;
    padding-bottom: 2px;
    border-bottom: 1px solid #252625;
    margin-bottom: 1px;
  }
  .dw-wmeta span:last-child { margin-left: auto; }
  .dw-copyable { cursor: pointer; }
  .dw-copyable:hover { color: #a9abaa; }

  /* Turn */
  .dw-turn {
    padding: 2px 3px;
    cursor: pointer;
    user-select: none;
    overflow-wrap: break-word;
    word-break: break-word;
  }

  .dw-turn:hover { background: #2a2b2a; }
  .dw-turn.selected { background: #1f2a3d; }

  .dw-speaker {
    font-weight: 500;
    font-size: 10px;
    margin-right: 4px;
  }

  .dw-turn.human .dw-speaker { color: #7dba8a; }
  .dw-turn.assistant .dw-speaker { color: #6b9bd1; }
  .dw-turn.tool .dw-speaker { color: #ffab00; }
  .dw-turn.marker .dw-speaker { color: #7b20a2; }

  .dw-text {
    font-size: 12px;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    word-break: break-word;
  }

  .dw-turn.marker {
    font-size: 10px;
    color: #656766;
    font-style: italic;
  }
  .dw-turn.marker .dw-text { color: #656766; font-size: 10px; }

  :global(.dw-code) {
    background: #1a1b1a;
    border: 1px solid #3f4140;
    padding: 3px 6px;
    margin: 2px 0;
    font-size: 11px;
    overflow-wrap: break-word;
    word-break: break-word;
    white-space: pre-wrap;
  }
  :global(.dw-code code) { color: #a9abaa; }

  :global(.dw-inline-code) {
    background: #1a1b1a;
    padding: 0 3px;
    color: #a9abaa;
    font-size: 11px;
  }

  /* Desktop: multi-column */
  @media (min-width: 768px) {
    .mobile-only { display: none; }
    .dw-col-wrap {
      flex: 1 1 340px;
      min-width: 300px;
      max-width: 640px;
    }
    .dw-col-wrap.dw-hidden { display: flex; }
  }

  /* Mobile */
  @media (max-width: 767px) {
    .dw-col-wrap.dw-hidden { display: none; }
    .dw-col { height: calc(100vh - 51px); }
    .dw-minimap { height: calc(100vh - 51px); }
  }
</style>
