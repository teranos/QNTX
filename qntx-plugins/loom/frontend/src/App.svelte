<script lang="ts">
  import { onMount } from 'svelte'
  import { flip } from 'svelte/animate'
  import SessionList from './SessionList.svelte'
  import BranchBar from './BranchBar.svelte'
  import ClusterBar from './ClusterBar.svelte'
  import Weave from './Weave.svelte'
  import Warp from './Warp.svelte'
  import { timeSpacers } from './timespacers'
  import { parseTurns, turnKey, fmtTime, type Turn } from './turns'

  // --- Types ---

  interface Weave {
    id: string
    branch: string
    context: string
    timestamp: number
    text: string | null
    word_count: number | null
    turn_count: number | null
    paths?: Record<string, string>
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

  // --- Session browser types ---

  interface SessionFile {
    session_id: string
    project: string
    file_path: string
    file_size: number
    line_count: number
    modified_at: number
    state: 'unweaved' | 'partial' | 'complete' | 'stale'
    weave_count: number
  }

  interface ProjectInfo {
    sessions: SessionFile[]
    session_count: number
  }

  interface SessionsResponse {
    projects: Record<string, ProjectInfo>
    project_count: number
    total_sessions: number
  }

  // --- State ---

  let sessions: Session[] = $state([])
  let loading = $state(true)
  let error = $state('')
  let selectedTurns: Set<string> = $state(new Set())
  let totalWeaves = $state(0)
  let mobileIdx = $state(0)
  const showClusters = true
  let clusterMap: Map<string, { cluster_id: number, label: string | null }> = $state(new Map())



  // Session browser state (per-project, inline in headers)
  let sessionsData: SessionsResponse | null = $state(null)
  let expandedProjects: Set<string> = $state(new Set())
  let importingSession: string | null = $state(null)
  let importResult: { session_id: string, weaves: number } | null = $state(null)

  async function fetchSessions() {
    try {
      const res = await fetch('/api/sessions')
      sessionsData = await res.json()
    } catch {
      sessionsData = null
    }
  }

  // Match a timeline project name (e.g. "tmp3/QNTX") to Claude project slugs
  // Slug format: -Users-s-b-vanhouten-SBVH-teranos-tmp3-QNTX
  // Convert project name slashes to dashes and check if slug ends with it
  function sessionsForProject(projectName: string): SessionFile[] {
    if (!sessionsData) return []
    const suffix = projectName.split('/').join('-')
    const results: SessionFile[] = []
    for (const [slug, info] of Object.entries(sessionsData.projects)) {
      if (slug.endsWith(suffix)) {
        results.push(...info.sessions)
      }
    }
    results.sort((a, b) => b.modified_at - a.modified_at)
    return results
  }

  function openDrawer(projectName: string) {
    const next = new Set(expandedProjects)
    next.add(projectName)
    expandedProjects = next
    // Mouse is over the header when opening, so not ready to close yet
    drawerMouseOut.delete(projectName)
  }

  // Track which drawers are in "full" mode (75vh, scrollable)
  let fullDrawers: Set<string> = $state(new Set())

  function closeDrawer(projectName: string) {
    const next = new Set(expandedProjects)
    next.delete(projectName)
    expandedProjects = next
    drawerMouseOut.delete(projectName)
    const nf = new Set(fullDrawers)
    nf.delete(projectName)
    fullDrawers = nf
  }

  // Track which drawers have had mouse-out (ready to close on scroll-up)
  let drawerMouseOut: Set<string> = $state(new Set())

  // Close drawers that are open + mouse-out when the column scrolls up
  let lastScrollTop: number[] = $state([])

  function onColumnScrollCloseDrawer(e: Event, projectName: string) {
    const el = e.target as HTMLElement
    const idx = columnEls.indexOf(el)
    if (idx < 0) return
    const prev = lastScrollTop[idx] ?? 0
    const cur = el.scrollTop
    lastScrollTop[idx] = cur
    if (cur < prev && drawerMouseOut.has(projectName)) {
      closeDrawer(projectName)
    }
  }

  // Scroll down on header → open drawer, mouse-out marks ready, scroll-up closes
  function bindHdDrawer(el: HTMLElement, project: string) {
    let expandLocked = false
    function onWheel(e: WheelEvent) {
      if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
      // When drawer is full, let it scroll internally
      if (fullDrawers.has(project)) {
        const drawer = el.querySelector('.dw-drawer-inner') as HTMLElement
        if (drawer) {
          e.preventDefault()
          e.stopPropagation()
          drawer.scrollTop += e.deltaY
        }
        return
      }
      e.preventDefault()
      e.stopPropagation()
      if (e.deltaY > 2) {
        if (!expandedProjects.has(project)) {
          openDrawer(project)
          expandLocked = true
          setTimeout(() => { expandLocked = false }, 600)
        } else if (!fullDrawers.has(project) && !expandLocked) {
          const nf = new Set(fullDrawers)
          nf.add(project)
          fullDrawers = nf
        }
      } else if (e.deltaY < -2) {
        if (expandedProjects.has(project) && !fullDrawers.has(project)) {
          closeDrawer(project)
        }
      }
    }

    function onClick(e: MouseEvent) {
      // Ignore clicks from inside the drawer or expanded body (e.g. import buttons)
      const target = e.target as HTMLElement
      if (target.closest('.dw-drawer') || target.closest('.dw-col-expanded-body')) return
      if (expandedProjects.has(project)) closeDrawer(project)
      else openDrawer(project)
    }

    function onLeave() {
      if (expandedProjects.has(project)) {
        const next = new Set(drawerMouseOut)
        next.add(project)
        drawerMouseOut = next
      }
    }

    function onEnter() {
      const next = new Set(drawerMouseOut)
      next.delete(project)
      drawerMouseOut = next
    }

    el.addEventListener('wheel', onWheel, { passive: false, capture: true })
    el.addEventListener('click', onClick)
    el.addEventListener('mouseleave', onLeave)
    el.addEventListener('mouseenter', onEnter)
    return {
      destroy() {
        el.removeEventListener('wheel', onWheel, { capture: true } as EventListenerOptions)
        el.removeEventListener('click', onClick)
        el.removeEventListener('mouseleave', onLeave)
        el.removeEventListener('mouseenter', onEnter)
      }
    }
  }

  async function importSession(file: SessionFile) {
    importingSession = file.session_id
    importResult = null
    try {
      const res = await fetch('/api/import', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ file_path: file.file_path }),
      })
      const data = await res.json()
      if (data.success) {
        importResult = { session_id: file.session_id, weaves: data.weaves_created }
        const savedExpanded = new Set(expandedProjects)
        const savedMouseOut = new Set(drawerMouseOut)
        const savedFull = new Set(fullDrawers)
        await Promise.all([fetchSessions(), load()])
        expandedProjects = savedExpanded
        drawerMouseOut = savedMouseOut
        fullDrawers = savedFull
      }
    } catch {
      // import failed silently
    }
    importingSession = null
  }

  function stateIndicator(state: string): string {
    switch (state) {
      case 'unweaved': return '--'
      case 'partial': return '..'
      case 'complete': return 'ok'
      case 'stale': return '!!'
      default: return '??'
    }
  }

  function stateColor(state: string): string {
    switch (state) {
      case 'unweaved': return '#e07030'
      case 'partial': return 'var(--color-warning)'
      case 'complete': return 'var(--accent-on-dark)'
      case 'stale': return '#ef4544'
      default: return 'var(--text-on-dark-tertiary)'
    }
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return bytes + 'B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + 'KB'
    return (bytes / (1024 * 1024)).toFixed(1) + 'MB'
  }

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

  // --- Session colors (deterministic by context) ---

  const SESSION_COLORS = [
    '#5e8a6a', '#5a7fa3', '#8a7ab3', '#b38a40',
    '#a35a5a', '#4a9a6a', '#4a70b3', '#7a4a8a',
    '#a37050', '#4a8a8a', '#8a7a40', '#9a5070',
  ]

  function sessionColor(context: string): string {
    let h = 0
    for (let i = 0; i < context.length; i++) h = ((h << 5) - h + context.charCodeAt(i)) | 0
    return SESSION_COLORS[Math.abs(h) % SESSION_COLORS.length]
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


  // --- Selection ---

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


  // --- Fetch data ---

  async function load() {
    try {
      const res = await fetch('/api/weaves')
      if (!res.ok) throw new Error(`/api/weaves ${res.status}: ${await res.text()}`)
      const data = await res.json()
      if (!data.branches) throw new Error(`/api/weaves returned no branches: ${JSON.stringify(data).substring(0, 500)}`)
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

      // Add empty columns for projects from session discovery that have no weaves yet
      if (sessionsData) {
        const existingProjects = new Set(built.map(s => s.context))
        for (const [slug, info] of Object.entries(sessionsData.projects)) {
          // Check if this slug is already covered by an existing weave project.
          // Can't just split slug by '-' because directory names may contain dashes
          // (e.g. slug -...-ctf-bsides-2026 for dir ctf-bsides/2026).
          // Instead, check if slug ends with any existing project's dash-form.
          const slugCovered = [...existingProjects].some(p => {
            const suffix = p.split('/').join('-')
            return slug.endsWith(suffix)
          })
          if (slugCovered || info.sessions.length === 0) continue

          // Derive display name: last two dash-separated components (best effort)
          const parts = slug.split('-').filter(p => p.length > 0)
          const projectName = parts.length >= 2
            ? parts[parts.length - 2] + '/' + parts[parts.length - 1]
            : parts[parts.length - 1] || slug
          existingProjects.add(projectName)
          const latest = info.sessions.reduce((max, s) => Math.max(max, s.modified_at), 0)
          const earliest = info.sessions.reduce((min, s) => Math.min(min, s.modified_at), Infinity)
          built.push({
            context: projectName,
            weaves: [],
            turns: [],
            branches: [],
            earliest,
            latest,
            totalWords: 0,
          })
        }
      }

      // Preserve existing column order if reloading, new columns go to end
      if (sessions.length > 0) {
        const prevOrder = sessions.map(s => s.context)
        built.sort((a, b) => {
          const ai = prevOrder.indexOf(a.context)
          const bi = prevOrder.indexOf(b.context)
          if (ai === -1 && bi === -1) return a.earliest - b.earliest
          if (ai === -1) return 1
          if (bi === -1) return -1
          return ai - bi
        })
      } else {
        built.sort((a, b) => a.earliest - b.earliest)
      }
      sessions = built
      loading = false

      // Fetch cluster memberships for all weave IDs
      const allIds = all.map(w => w.id)
      loadClusters(allIds)

      // Warps init themselves via $effect when columnEls are set
    } catch (e: any) {
      error = `${e.message || 'fetch failed'}\n${e.stack || ''}`
      console.error('load() failed:', e)
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

  function weaveMinimapColor(weave: Weave): string {
    if (showClusters) {
      const entry = clusterMap.get(weave.id)
      if (entry) return BRANCH_COLORS[entry.cluster_id % BRANCH_COLORS.length]
    }
    return branchColor(weave.branch)
  }

  // --- Minimap segment layout with time gaps ---

  const GAP_THRESHOLD_MS = 12 * 60 * 60 * 1000  // 12 hours
  const GAP_WEIGHT = 3  // a 12h gap occupies the space of 3 weaves

  interface MinimapItem {
    type: 'weave' | 'gap'
    weave?: Weave
    weight: number
    gapHours?: number
    seam?: 'session' | 'compaction' | 'default'
    tool?: boolean
    hook?: boolean
  }

  function hasSpeaker(w: Weave, speaker: string): boolean {
    const turns = parseTurns(w)
    return turns.some(t => t.speaker === speaker)
  }

  function firstSpeaker(w: Weave): string {
    if (!w.text) return ''
    // Check what the first turn's speaker is
    if (w.text.startsWith('[session]')) return 'session'
    if (w.text.startsWith('[compaction]')) return 'compaction'
    return ''
  }

  function computeMinimapItems(weaves: Weave[]): MinimapItem[] {
    if (weaves.length === 0) return []
    const items: MinimapItem[] = []
    for (let i = 0; i < weaves.length; i++) {
      if (i > 0) {
        const gap = weaves[i].timestamp - weaves[i - 1].timestamp
        if (gap > GAP_THRESHOLD_MS) {
          items.push({ type: 'gap', weight: GAP_WEIGHT, gapHours: Math.round(gap / (60 * 60 * 1000)) })
        }
      }
      const speaker = firstSpeaker(weaves[i])
      const seam: 'session' | 'compaction' | 'default' = speaker === 'session' ? 'session' : speaker === 'compaction' ? 'compaction' : 'default'
      items.push({ type: 'weave', weave: weaves[i], weight: 1, seam, tool: hasSpeaker(weaves[i], 'tool'), hook: hasSpeaker(weaves[i], 'hook') })
    }
    return items
  }

  function itemHeight(items: MinimapItem[], item: MinimapItem): number {
    const total = items.reduce((s, i) => s + i.weight, 0)
    return (item.weight / total) * 100
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
    if (e.key === 'Escape') {
      if (expandedProjects.size > 0) { expandedProjects = new Set(); return }
      clearSelection()
    }
  }

  onMount(async () => {
    await fetchSessions()
    load()
    document.addEventListener('keydown', onKeydown)
    return () => document.removeEventListener('keydown', onKeydown)
  })
</script>

<div class="dw">
  <header>
    <span class="dw-title">loom</span>
    {#if !loading}
      <span class="dw-stat">{sessions.length} sessions</span>
      <span class="dw-stat">{totalWeaves} weaves</span>
    {/if}
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
      {#each sessions as session, si (session.context)}
        <div class="dw-col-wrap" class:dw-col-empty={session.weaves.length === 0} class:dw-col-expanded={session.weaves.length === 0 && expandedProjects.has(session.context)} class:dw-hidden={si !== mobileIdx} animate:flip={{ duration: 400 }}>
          {#if session.weaves.length === 0}
            <div class="dw-col-empty-inner" use:bindHdDrawer={session.context}>
              <span class="dw-col-collapsed-name">{session.context}</span>
              <span class="dw-col-collapsed-count">{sessionsForProject(session.context).length} jsonl</span>
              {#if expandedProjects.has(session.context)}
                <div class="dw-col-expanded-body">
                  <SessionList sessions={sessionsForProject(session.context)} {importingSession} {importResult} onImport={importSession} hideUnweavedState={true} />
                </div>
              {/if}
            </div>
          {:else}
          <div
            class="dw-session-hd-zone"
            class:dw-hd-expandable={sessionsForProject(session.context).length > 0}
            use:bindHdDrawer={session.context}
          >
            <div class="dw-drawer" class:dw-drawer-open={expandedProjects.has(session.context) && !fullDrawers.has(session.context)} class:dw-drawer-full={fullDrawers.has(session.context)}>
              <div class="dw-drawer-inner">
                <BranchBar branches={session.branches} {branchColor} />
                <ClusterBar weaves={session.weaves} {clusterMap} />
                {#if sessionsForProject(session.context).length > 0}
                  <div class="dw-jsonl-list">
                    <SessionList sessions={sessionsForProject(session.context)} {importingSession} {importResult} onImport={importSession} />
                  </div>
                {/if}
              </div>
            </div>
            <div class="dw-session-hd">
              <div class="dw-session-top">
                <span class="dw-sid">{session.context === '_' ? 'untracked' : session.context.substring(0, 16)}</span>
                <span class="dw-smeta">{fmtTime(session.earliest)}</span>
                <span class="dw-smeta">{session.weaves.length}w {session.totalWords} words</span>
                {#if sessionsForProject(session.context).length > 0}
                  <span class="dw-smeta dw-jsonl-count">{sessionsForProject(session.context).length} jsonl</span>
                {/if}
              </div>
            </div>
          </div>
          <div class="dw-col-body">
        <div
          class="dw-col"
          use:bindColumn={si}
          onscroll={(e) => { onColumnScroll(e); onColumnScrollCloseDrawer(e, session.context) }}
          onpointerenter={onColumnEnter}
          onpointerleave={onColumnLeave}
        >
          <div class="dw-stream">
            {#each session.weaves as weave, wi}
              {@const prevTs = wi > 0 ? session.weaves[wi - 1].timestamp : 0}
              {@const spacers = timeSpacers(prevTs, weave.timestamp)}
              {#each spacers as sz}
                <div class="dw-time-spacer" style="height: {sz}px"></div>
              {/each}
              <Weave {weave} clusterLabel={clusterLabel(weave.id)} {selectedTurns} onToggle={toggle} />
            {/each}
          </div>
        </div>

        <Warp
          items={computeMinimapItems(session.weaves)}
          columnEl={columnEls[si]}
          {branchColor}
          {sessionColor}
          {clusterMap}
          branchColors={BRANCH_COLORS}
        />
          </div>
        {/if}
        </div>
      {/each}
    </div>
  {/if}

</div>

<style>
  @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@300;400;500&display=swap');

  :global(*) { box-sizing: border-box; margin: 0; padding: 0; }

  :global(body) {
    background: var(--bg-canvas);
    color: var(--text-on-dark);
    font-family: var(--font-mono);
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
    background: var(--bg-almost-black);
    border-bottom: 1px solid var(--border-on-dark);
  }

  .dw-title {
    color: var(--accent-on-dark);
    font-weight: 500;
    font-size: var(--font-size-md);
  }

  .dw-stat { color: var(--text-on-dark-tertiary); font-size: var(--font-size-sm); }


  .dw-sel {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-on-dark);
    font-size: var(--font-size-sm);
  }

  .dw-sel button {
    background: var(--bg-dark-light);
    color: var(--text-on-dark);
    border: 1px solid var(--border-on-dark);
    padding: 1px 6px;
    font: inherit;
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
  .dw-sel button:hover { background: var(--border-on-dark); }

  /* Status messages */
  .dw-msg { padding: 24px; color: var(--text-on-dark-tertiary); text-align: center; }
  .dw-err { color: var(--color-error); white-space: pre-wrap; font-size: 11px; }

  /* Mobile session dots */
  .dw-dots {
    display: flex;
    justify-content: center;
    gap: 6px;
    padding: 6px;
    background: var(--bg-almost-black);
    border-bottom: 1px solid var(--border-on-dark);
  }

  .dw-dot {
    width: 6px; height: 6px;
    border: 1px solid var(--border-on-dark);
    background: transparent;
    padding: 0;
    cursor: pointer;
  }
  .dw-dot.active { background: var(--accent-on-dark); border-color: var(--accent-on-dark); }

  /* Timeline container */
  .dw-timeline {
    display: flex;
    flex: 1;
    overflow-x: auto;
    overflow-y: hidden;
  }

  /* Column wrapper — vertical: header on top, body below */
  .dw-col-wrap {
    display: flex;
    flex-direction: column;
    flex: 0 0 100%;
    height: calc(100vh - 33px);
    border-right: 1px solid var(--border-on-dark);
  }

  /* Body row: weaves + warp side by side, fills remaining height */
  .dw-col-body {
    display: flex;
    flex: 1;
    min-height: 0;
  }

  /* Session column */
  .dw-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    scrollbar-width: none;
  }
  .dw-col::-webkit-scrollbar { display: none; }

  /* Session header */
  .dw-session-hd-zone {
    flex-shrink: 0;
  }

  .dw-session-hd {
    padding: 6px 10px;
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-on-dark);
  }

  .dw-hd-expandable { cursor: ns-resize; }

  .dw-drawer {
    max-height: 0;
    overflow: hidden;
    transition: max-height 0.25s ease-out;
    background: var(--bg-dark);
    border-bottom: 1px solid var(--border-on-dark);
  }
  .dw-drawer-open {
    max-height: 300px;
    transition: max-height 0.3s ease-in;
  }
  .dw-drawer-full {
    max-height: 75vh;
    transition: max-height 0.3s ease-in;
  }
  .dw-drawer-full .dw-drawer-inner {
    max-height: calc(75vh - 12px);
    overflow-y: auto;
  }
  .dw-drawer-inner {
    padding: 4px 10px 6px;
  }

  .dw-session-top {
    display: flex;
    gap: 8px;
    align-items: baseline;
  }

  .dw-sid { color: var(--text-on-dark-secondary); font-size: var(--font-size-sm); font-weight: 500; }
  .dw-smeta { color: var(--text-secondary); font-size: var(--font-size-xs); }


  /* Time spacer (temporal alignment) */
  .dw-time-spacer {
    margin: 0 6px;
    border-left: 1px dashed var(--border-on-dark);
    border-right: 1px dashed var(--border-on-dark);
  }
  .dw-gap-label {
    color: var(--border-on-dark);
    font-size: 9px;
  }

  /* Weave stream */
  .dw-stream { padding: 2px 0; flex: 1; }


  /* Inline session list (per-project, in header) */
  .dw-sessions-btn {
    background: none;
    border: 1px solid var(--border-on-dark);
    color: var(--text-on-dark-tertiary);
    padding: 0 4px;
    font: inherit;
    font-size: var(--font-size-xs);
    cursor: pointer;
    margin-left: auto;
  }
  .dw-sessions-btn:hover { color: var(--accent-on-dark); border-color: var(--accent-on-dark); }

  .dw-jsonl-list {
    margin-top: 4px;
    border-top: 1px solid var(--border-on-dark);
    padding-top: 2px;
  }

  /* Empty column (no weaves yet) — same wrapper element, CSS transition on flex */
  .dw-col-wrap.dw-col-empty {
    flex: 0 0 36px;
    cursor: pointer;
    overflow: hidden;
    transition: flex-basis 0.4s ease, min-width 0.4s ease;
    min-width: 0;
  }
  .dw-col-wrap.dw-col-empty:hover { background: var(--bg-dark-hover); }
  .dw-col-empty-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    height: 100%;
  }
  .dw-col-collapsed-name {
    writing-mode: vertical-rl;
    text-orientation: mixed;
    color: var(--text-on-dark-tertiary);
    font-size: 11px;
    padding-top: 12px;
    white-space: nowrap;
  }
  .dw-col-collapsed-count {
    writing-mode: vertical-rl;
    text-orientation: mixed;
    color: var(--text-on-dark-tertiary);
    font-size: 9px;
    opacity: 0.6;
    margin-top: 8px;
  }
  /* Expanded empty column */
  .dw-col-wrap.dw-col-expanded {
    flex: 0 0 280px;
  }
  .dw-col-expanded .dw-col-empty-inner {
    align-items: stretch;
  }
  .dw-col-expanded .dw-col-collapsed-name {
    writing-mode: horizontal-tb;
    text-orientation: initial;
    padding: 6px 10px 2px;
    font-weight: 500;
    color: var(--text-on-dark-secondary);
  }
  .dw-col-expanded .dw-col-collapsed-count {
    writing-mode: horizontal-tb;
    text-orientation: initial;
    padding: 0 10px 4px;
    margin-top: 0;
  }
  .dw-col-expanded-body {
    flex: 1;
    overflow-y: auto;
    padding: 0 6px;
  }
  /* Transition from empty to full column */
  .dw-col-wrap {
    transition: flex-basis 0.4s ease, min-width 0.4s ease, flex-grow 0.4s ease;
  }

  /* Desktop: multi-column */
  @media (min-width: 768px) {
    .mobile-only { display: none; }
    .dw-col-wrap {
      flex: 1 1 560px;
      min-width: 480px;
      max-width: 900px;
    }
    .dw-col-wrap.dw-col-empty {
      flex: 0 0 36px;
      min-width: 0;
      max-width: none;
    }
    .dw-col-wrap.dw-col-expanded {
      flex: 0 0 280px;
    }
    .dw-col-wrap.dw-hidden { display: flex; }
  }

  /* Mobile */
  @media (max-width: 767px) {
    .dw-col-wrap.dw-hidden { display: none; }
    .dw-col-wrap { height: calc(100vh - 51px); }
  }
</style>
