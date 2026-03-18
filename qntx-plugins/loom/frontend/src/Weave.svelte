<script lang="ts">
  import { parseTurns, turnKey, fmtTime, type Turn } from './turns'

  interface WeaveData {
    id: string
    branch: string
    context: string
    timestamp: number
    text: string | null
    word_count: number | null
    turn_count: number | null
    paths?: Record<string, string>
  }

  let {
    weave,
    clusterLabel = null,
    selectedTurns,
    onToggle,
  }: {
    weave: WeaveData
    clusterLabel: string | null
    selectedTurns: Set<string>
    onToggle: (turn: Turn) => void
  } = $props()

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

  const turns = parseTurns(weave)
</script>

<div class="dw-weave" data-ts={weave.timestamp}>
  <div class="dw-wmeta">
    <span class="dw-copyable" onclick={(e) => { e.stopPropagation(); navigator.clipboard.writeText(weave.id) }}>{weave.branch}</span>
    {#if clusterLabel}
      <span class="dw-cluster">{clusterLabel}</span>
    {/if}
    <span>{fmtTime(weave.timestamp)}</span>
    <span>{weave.word_count}w {weave.turn_count}t</span>
  </div>
  {#each turns as turn}
    {@const k = turnKey(turn)}
    <div
      class="dw-turn"
      class:selected={selectedTurns.has(k)}
      class:human={turn.speaker === 'human'}
      class:assistant={turn.speaker === 'assistant'}
      class:tool={turn.speaker === 'tool'}
      class:edit={turn.speaker === 'edit'}
      class:read={turn.speaker === 'read'}
      class:search={turn.speaker === 'search'}
      class:write={turn.speaker === 'write'}
      class:hook={turn.speaker === 'hook'}
      class:marker={turn.speaker === 'session' || turn.speaker === 'compaction' || turn.speaker === 'agent' || turn.speaker === 'task'}
      onclick={() => onToggle(turn)}
      role="button"
      tabindex="0"
      onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onToggle(turn) }}}
    >
      <span class="dw-speaker">[{turn.speaker}]</span>
      {#if turn.speaker === 'assistant'}
        <span class="dw-text">{@html renderText(turn.text)}</span>
      {:else if turn.fullPath}
        <span
          class="dw-text dw-path"
          title={turn.fullPath}
          onclick={(e) => { e.stopPropagation(); navigator.clipboard.writeText(turn.fullPath!) }}
        >{turn.text}</span>
      {:else}
        <span class="dw-text">{turn.text}</span>
      {/if}
    </div>
  {/each}
</div>

<style>
  .dw-weave {
    border-left: 3px solid var(--border-on-dark);
    margin: 0 6px 1px 6px;
    padding: 3px 6px;
  }

  .dw-wmeta {
    display: flex;
    gap: 8px;
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
    padding-bottom: 2px;
    border-bottom: 1px solid var(--bg-secondary);
    margin-bottom: 1px;
  }
  .dw-wmeta span:last-child { margin-left: auto; }
  .dw-copyable { cursor: pointer; }
  .dw-copyable:hover { color: var(--text-on-dark-secondary); }

  .dw-cluster { color: var(--text-on-dark-tertiary); font-style: italic; }

  /* Turn */
  .dw-turn {
    padding: 0px 3px;
    cursor: pointer;
    user-select: none;
    overflow-wrap: break-word;
    word-break: break-word;
  }

  .dw-turn:hover { background: var(--bg-dark-hover); }
  .dw-turn.selected { background: var(--glyph-status-running-bg); }

  .dw-speaker {
    font-weight: 500;
    font-size: var(--font-size-xs);
    margin-right: 4px;
  }

  .dw-turn.human .dw-speaker { color: var(--accent-on-dark); }
  .dw-turn.assistant .dw-speaker { color: var(--glyph-status-running-text); }
  .dw-turn.tool .dw-speaker { color: var(--color-warning); }
  .dw-turn.marker .dw-speaker { color: var(--color-scheduled); }

  .dw-turn.tool {
    background: var(--bg-almost-black);
    border-left: 2px solid var(--color-warning);
    padding-left: 4px;
    margin: 1px 0;
  }
  .dw-turn.tool .dw-text {
    color: var(--text-on-dark-secondary);
    font-size: 8px;
  }

  .dw-turn.edit,
  .dw-turn.read,
  .dw-turn.search,
  .dw-turn.write {
    background: var(--bg-almost-black);
    border-left: 2px solid #5b8dd9;
    padding-left: 4px;
    margin: 1px 0;
  }
  .dw-turn.edit .dw-speaker { color: #5b8dd9; }
  .dw-turn.read .dw-speaker { color: #5b8dd9; }
  .dw-turn.search .dw-speaker { color: #5b8dd9; }
  .dw-turn.write .dw-speaker { color: #5b8dd9; }
  .dw-turn.edit .dw-text,
  .dw-turn.read .dw-text,
  .dw-turn.search .dw-text,
  .dw-turn.write .dw-text {
    color: var(--text-on-dark-secondary);
    font-size: 8px;
  }
  .dw-path {
    cursor: pointer;
    border-bottom: 1px dotted var(--border-on-dark);
  }
  .dw-path:hover {
    color: var(--text-on-dark);
    border-bottom-color: #5b8dd9;
  }

  .dw-turn.hook {
    background: var(--bg-almost-black);
    border-left: 2px solid #d94a4a;
    padding-left: 4px;
    margin: 1px 0;
  }
  .dw-turn.hook .dw-speaker { color: #d94a4a; }
  .dw-turn.hook .dw-text {
    color: var(--text-on-dark-secondary);
    font-size: 8px;
  }

  .dw-text {
    font-size: 9px;
    line-height: 1.05;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    word-break: break-word;
  }

  .dw-turn.marker {
    font-size: 10px;
    color: var(--text-secondary);
    font-style: italic;
  }
  .dw-turn.marker .dw-text { color: var(--text-secondary); font-size: 10px; }

  :global(.dw-code) {
    background: var(--bg-almost-black);
    border: 1px solid var(--border-on-dark);
    padding: 3px 6px;
    margin: 2px 0;
    font-size: 11px;
    overflow-wrap: break-word;
    word-break: break-word;
    white-space: pre-wrap;
  }
  :global(.dw-code code) { color: var(--text-on-dark-secondary); }

  :global(.dw-inline-code) {
    background: var(--bg-almost-black);
    padding: 0 3px;
    color: var(--text-on-dark-secondary);
    font-size: 11px;
  }
</style>
