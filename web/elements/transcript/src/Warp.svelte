<script lang="ts">
  import { msOf, type Turn } from './transcript'

  // Loom's warp, as the transcript's own scrollbar: one segment per turn, a gap
  // past twelve hours weighs three, the wheel zooms, a press or a drag scrolls.

  let { turns, columnEl = null }: { turns: Turn[]; columnEl: HTMLElement | null } = $props()

  const GAP_MS = 12 * 60 * 60 * 1000
  const GAP_WEIGHT = 3

  interface Item { gap: boolean; weight: number; turn?: Turn }

  const items: Item[] = $derived.by(() => {
    const out: Item[] = []
    for (let i = 0; i < turns.length; i++) {
      if (i > 0 && msOf(turns[i].at) - msOf(turns[i - 1].at) > GAP_MS) out.push({ gap: true, weight: GAP_WEIGHT })
      out.push({ gap: false, weight: 1, turn: turns[i] })
    }
    return out
  })
  const total = $derived(items.reduce((s, i) => s + i.weight, 0) || 1)

  let scrollFraction = $state(0)
  let viewHeight = $state(100)
  let zoom = $state(1)
  let dragging = false

  function updateFromColumn() {
    if (!columnEl) return
    viewHeight = (columnEl.clientHeight / (columnEl.scrollHeight || 1)) * 100
    scrollFraction = columnEl.scrollTop / (columnEl.scrollHeight - columnEl.clientHeight || 1)
  }

  $effect(() => {
    if (!columnEl) return
    const el = columnEl
    el.addEventListener('scroll', updateFromColumn)
    updateFromColumn()
    return () => el.removeEventListener('scroll', updateFromColumn)
  })

  function laneTranslate(): number {
    return ((50 - viewHeight / 2) - scrollFraction * (zoom * 100 - viewHeight)) / zoom
  }

  function scrollTo(e: PointerEvent, smooth: boolean) {
    if (!columnEl) return
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const clickFrac = (e.clientY - rect.top) / rect.height
    const contentFrac = (clickFrac - (laneTranslate() / 100) * zoom) / zoom
    const target = Math.max(0, Math.min(1, contentFrac)) * (columnEl.scrollHeight - columnEl.clientHeight)
    columnEl.scrollTo({ top: target, behavior: smooth ? 'smooth' : 'instant' })
  }

  function onPointerDown(e: PointerEvent) {
    dragging = true
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    scrollTo(e, true)
  }
  function onPointerMove(e: PointerEvent) { if (dragging) scrollTo(e, false) }
  function onPointerUp() { dragging = false }

  function bindWheel(el: HTMLElement) {
    const onWheel = (e: WheelEvent) => {
      if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
      e.preventDefault()
      zoom = Math.max(1, Math.min(10, zoom + (e.deltaY > 0 ? 0.3 : -0.3)))
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return { destroy() { el.removeEventListener('wheel', onWheel) } }
  }

  function seam(turn: Turn): string {
    return turn.speaker === 'session' || turn.speaker === 'compaction' ? turn.speaker : ''
  }
</script>

<div class="tw-warp" onpointerdown={onPointerDown} onpointermove={onPointerMove} onpointerup={onPointerUp} use:bindWheel>
  <div class="tw-lanes" style="height: {zoom * 100}%; transform: translateY({laneTranslate()}%)">
    {#each items as item}
      {#if item.gap}
        <div class="tw-seg tw-gap" style="height: {(item.weight / total) * 100}%"></div>
      {:else}
        <div class="tw-seg sp-{item.turn!.speaker} seam-{seam(item.turn!)}" style="height: {(item.weight / total) * 100}%">
          {#if item.turn!.speaker === 'ground'}<span class="tw-mark tw-ground">&#x25cf;</span>{/if}
          {#if item.turn!.speaker === 'tool'}<span class="tw-mark tw-tool">&#x25c6;</span>{/if}
        </div>
      {/if}
    {/each}
  </div>
  {#if viewHeight < 100}
    <div class="tw-view" style="top: {50 - (viewHeight * zoom) / 2}%; height: {viewHeight * zoom}%"></div>
  {/if}
</div>

<style>
  .tw-warp {
    width: 14px;
    flex-shrink: 0;
    position: relative;
    overflow: hidden;
    cursor: pointer;
    background: var(--bg-almost-black);
    border-left: 1px solid var(--bg-secondary);
  }
  .tw-lanes { display: flex; flex-direction: column; width: 100%; will-change: transform; }
  .tw-seg { flex-shrink: 0; opacity: 0.7; display: flex; align-items: center; justify-content: center; background: var(--bg-secondary); }
  .tw-gap { background: var(--bg-almost-black); }
  .sp-human { background: var(--accent-on-dark); }
  .sp-assistant { background: var(--element-status-running-text); }
  .sp-ground { background: var(--color-error); }
  .sp-rite { background: var(--color-scheduled); }
  .seam-session { border-bottom: 2px solid rgba(125, 186, 138, 0.6); }
  .seam-compaction { border-bottom: 2px solid rgba(255, 171, 0, 0.6); }
  .tw-mark { font-size: 6px; line-height: 1; }
  .tw-tool { color: var(--color-warning); }
  .tw-ground { color: var(--color-error); }
  .tw-view {
    position: absolute;
    left: 0;
    width: 100%;
    border: 1px solid var(--accent-on-dark);
    background: rgba(125, 186, 138, 0.08);
    pointer-events: none;
  }
</style>
