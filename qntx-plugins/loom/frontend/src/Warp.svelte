<script lang="ts">
  interface MinimapItem {
    type: 'weave' | 'gap'
    weave?: any
    weight: number
    gapHours?: number
    seam?: 'session' | 'compaction' | 'default'
    tool?: boolean
    hook?: boolean
  }

  interface ClusterEntry {
    cluster_id: number
    label: string | null
  }

  let {
    items,
    columnEl = null,
    branchColor,
    sessionColor,
    clusterMap,
    branchColors,
    onScrollSync,
  }: {
    items: MinimapItem[]
    columnEl: HTMLElement | null
    branchColor: (branch: string) => string
    sessionColor: (context: string) => string
    clusterMap: Map<string, ClusterEntry>
    branchColors: string[]
    onScrollSync?: (e: Event) => void
  } = $props()

  let scrollFraction: number = $state(0)
  let viewHeight: number = $state(100)
  let zoom: number = $state(1)

  let dragActive: boolean = false
  let dragStartY: number = 0
  let dragging: boolean = false
  const DRAG_DEADZONE = 4

  function updateFromColumn() {
    if (!columnEl) return
    const ratio = columnEl.clientHeight / columnEl.scrollHeight
    scrollFraction = columnEl.scrollTop / (columnEl.scrollHeight - columnEl.clientHeight || 1)
    viewHeight = ratio * 100
  }

  function onColumnScroll(e: Event) {
    updateFromColumn()
    onScrollSync?.(e)
  }

  // Bind scroll listener to column element
  $effect(() => {
    if (!columnEl) return
    columnEl.addEventListener('scroll', onColumnScroll)
    updateFromColumn()
    return () => columnEl!.removeEventListener('scroll', onColumnScroll)
  })

  function laneTranslate(): number {
    const laneTopMinimap = (50 - viewHeight / 2) - scrollFraction * (zoom * 100 - viewHeight)
    return laneTopMinimap / zoom
  }

  function itemHeight(item: MinimapItem): number {
    const total = items.reduce((s, i) => s + i.weight, 0)
    return (item.weight / total) * 100
  }

  function scrollToClick(e: PointerEvent, smooth = false) {
    if (!columnEl) return
    const warp = e.currentTarget as HTMLElement
    const rect = warp.getBoundingClientRect()
    const clickFrac = (e.clientY - rect.top) / rect.height
    const t = laneTranslate()
    const laneTopFrac = (t / 100) * zoom
    const contentFrac = (clickFrac - laneTopFrac) / zoom
    const scrollFrac = Math.max(0, Math.min(1, contentFrac))
    const target = scrollFrac * (columnEl.scrollHeight - columnEl.clientHeight)
    columnEl.scrollTo({ top: target, behavior: smooth ? 'smooth' : 'instant' })
  }

  function onPointerDown(e: PointerEvent) {
    dragging = true
    dragStartY = e.clientY
    dragActive = false
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    scrollToClick(e, true)
  }

  function onPointerMove(e: PointerEvent) {
    if (!dragging) return
    if (!dragActive) {
      if (Math.abs(e.clientY - dragStartY) < DRAG_DEADZONE) return
      dragActive = true
    }
    scrollToClick(e)
  }

  function onPointerUp() {
    dragging = false
    dragActive = false
  }

  function onWheel(e: WheelEvent) {
    if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
    e.preventDefault()
    const delta = e.deltaY > 0 ? 0.3 : -0.3
    zoom = Math.max(1, Math.min(10, zoom + delta))
  }

  function bindWheel(el: HTMLElement) {
    el.addEventListener('wheel', onWheel, { passive: false })
    return {
      destroy() { el.removeEventListener('wheel', onWheel) }
    }
  }
</script>

<div
  class="dw-warp"
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
  use:bindWheel
>
  <div class="dw-warp-lanes" style="height: {zoom * 100}%; transform: translateY({laneTranslate()}%)">
    <div class="dw-warp-lane dw-lane-branch">
      {#each items as item}
        {#if item.type === 'gap'}
          <div class="dw-warp-seg dw-warp-gap" style="height: {itemHeight(item)}%"></div>
        {:else if item.weave}
          <div class="dw-warp-seg dw-warp-seam seam-{item.seam}" style="height: {itemHeight(item)}%; background: {branchColor(item.weave.branch)}"></div>
        {/if}
      {/each}
    </div>
    <div class="dw-warp-lane dw-lane-session">
      {#each items as item}
        {#if item.type === 'gap'}
          <div class="dw-warp-seg dw-warp-gap" style="height: {itemHeight(item)}%"></div>
        {:else if item.weave}
          <div class="dw-warp-seg" style="height: {itemHeight(item)}%; background: {sessionColor(item.weave.context)}"></div>
        {/if}
      {/each}
    </div>
    <div class="dw-warp-tool-overlay">
      {#each items as item}
        {#if item.type === 'gap'}
          <div class="dw-warp-seg" style="height: {itemHeight(item)}%"></div>
        {:else if item.weave}
          <div class="dw-warp-seg" style="height: {itemHeight(item)}%">{#if item.hook}<span class="dw-warp-hook">&#x25cf;</span>{/if}{#if item.tool}<span class="dw-warp-tool">&#x25c6;</span>{/if}</div>
        {/if}
      {/each}
    </div>
    <div class="dw-warp-lane dw-lane-cluster">
      {#each items as item}
        {#if item.type === 'gap'}
          <div class="dw-warp-seg dw-warp-gap" style="height: {itemHeight(item)}%"></div>
        {:else if item.weave}
          {#if clusterMap.get(item.weave.id)}
            <div class="dw-warp-seg dw-warp-seam seam-{item.seam}" style="height: {itemHeight(item)}%; background: {branchColors[clusterMap.get(item.weave.id)!.cluster_id % branchColors.length]}"></div>
          {:else}
            <div class="dw-warp-seg dw-warp-seam seam-{item.seam}" style="height: {itemHeight(item)}%; background: #252625"></div>
          {/if}
        {/if}
      {/each}
    </div>
  </div>
  {#if viewHeight < 100}
    <div
      class="dw-warp-view"
      style="top: {50 - (viewHeight * zoom) / 2}%; height: {viewHeight * zoom}%"
    ></div>
  {/if}
</div>

<style>
  .dw-warp {
    width: 24px;
    background: var(--bg-almost-black);
    position: relative;
    cursor: pointer;
    flex-shrink: 0;
    border-left: 1px solid var(--bg-secondary);
    overflow: hidden;
  }
  .dw-warp-lanes {
    display: flex;
    flex-direction: row;
    width: 100%;
    height: 100%;
    will-change: transform;
  }
  .dw-warp-lane {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .dw-lane-branch { width: 10px; }
  .dw-lane-session { width: 4px; }
  .dw-lane-cluster { width: 10px; }
  .dw-warp-seg {
    flex-shrink: 0;
    opacity: 0.7;
  }
  .dw-warp-seam {
    border-bottom: 1px solid rgba(223,225,224,0.1);
  }
  .dw-warp-seam.seam-session {
    border-bottom: 2px solid rgba(125,186,138,0.6);
  }
  .dw-warp-seam.seam-compaction {
    border-bottom: 2px solid rgba(255,171,0,0.6);
  }
  .dw-warp-gap {
    background: var(--bg-almost-black);
  }
  .dw-warp-tool-overlay {
    position: absolute;
    left: 0;
    top: 0;
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    pointer-events: none;
    z-index: 1;
  }
  .dw-warp-tool-overlay .dw-warp-seg {
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .dw-warp-tool {
    color: var(--color-warning);
    font-size: 8px;
    line-height: 1;
  }
  .dw-warp-hook {
    color: #d94a4a;
    font-size: 6px;
    line-height: 1;
  }
  .dw-warp-view {
    position: absolute;
    left: 0;
    width: 100%;
    border: 1px solid var(--accent-on-dark);
    background: rgba(125,186,138,0.08);
    pointer-events: none;
  }
</style>
