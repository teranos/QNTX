<script lang="ts">
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

  interface ImportResult {
    session_id: string
    weaves: number
  }

  let {
    sessions,
    importingSession,
    importResult,
    onImport,
    hideUnweavedState = false,
  }: {
    sessions: SessionFile[]
    importingSession: string | null
    importResult: ImportResult | null
    onImport: (file: SessionFile) => void
    hideUnweavedState?: boolean
  } = $props()

  function fmtTime(ts: number): string {
    const d = new Date(ts)
    const mon = d.toLocaleString('en', { month: 'short' })
    const day = d.getDate()
    const hh = String(d.getHours()).padStart(2, '0')
    const mm = String(d.getMinutes()).padStart(2, '0')
    return `${mon} ${day} ${hh}:${mm}`
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return bytes + 'B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + 'KB'
    return (bytes / (1024 * 1024)).toFixed(1) + 'MB'
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
</script>

{#each sessions as sf}
  <div class="dw-jsonl-row">
    <span class="dw-jsonl-sid">{sf.session_id.substring(0, 8)}</span>
    <span class="dw-jsonl-detail">{fmtTime(sf.modified_at)}</span>
    <span class="dw-jsonl-detail">{sf.line_count}L {formatBytes(sf.file_size)}</span>
    {#if sf.weave_count > 0}
      <span class="dw-jsonl-detail">{sf.weave_count}w</span>
    {/if}
    {#if !(hideUnweavedState && sf.state === 'unweaved')}
      <span class="dw-jsonl-detail" style="color: {stateColor(sf.state)}; opacity: 1">{sf.state}</span>
    {/if}
    {#if importResult && importResult.session_id === sf.session_id}
      <span class="dw-jsonl-result">+{importResult.weaves}w</span>
    {/if}
    {#if importingSession === sf.session_id}
      <span class="dw-jsonl-importing">importing...</span>
    {:else if sf.state === 'unweaved' || sf.state === 'partial'}
      <button class="dw-jsonl-import" onclick={(e: MouseEvent) => { e.stopPropagation(); onImport(sf) }}>import</button>
    {:else if sf.state === 'stale' || sf.state === 'complete'}
      <button class="dw-jsonl-import" onclick={(e: MouseEvent) => { e.stopPropagation(); onImport(sf) }}>reimport</button>
    {/if}
  </div>
{/each}

<style>
  .dw-jsonl-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 2px 4px;
    font-size: var(--font-size-xs);
  }
  .dw-jsonl-row:hover { background: var(--bg-dark-hover); }

  .dw-jsonl-sid {
    color: var(--text-on-dark-secondary);
    font-weight: 500;
  }

  .dw-jsonl-detail {
    color: var(--text-on-dark-tertiary);
  }

  .dw-jsonl-import {
    margin-left: auto;
    background: none;
    border: 1px solid var(--accent-on-dark);
    color: var(--accent-on-dark);
    padding: 0 4px;
    font: inherit;
    font-size: var(--font-size-xs);
    cursor: pointer;
  }
  .dw-jsonl-import:hover { background: rgba(125, 186, 138, 0.15); }

  .dw-jsonl-importing {
    margin-left: auto;
    color: var(--color-warning);
    font-size: var(--font-size-xs);
  }

  .dw-jsonl-result {
    color: var(--accent-on-dark);
    font-weight: 500;
    font-size: var(--font-size-xs);
  }
</style>
