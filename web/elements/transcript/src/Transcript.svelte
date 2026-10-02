<script lang="ts">
  import Warp from './Warp.svelte'
  import { timeSpacers } from './timespacers'
  import { msOf, type Transcript as Read, type Turn } from './transcript'

  let {
    sessions,
    session,
    turns,
    said,
    onChoose,
    onOpen,
  }: {
    sessions: Read[]
    session: string
    turns: Turn[]
    said: string
    onChoose: (session: string) => void
    onOpen: (turn: Turn) => void
  } = $props()

  let columnEl: HTMLElement | null = $state(null)

  // Loom's minimal markdown for what the assistant said: code blocks, bold,
  // inline code. String methods only.
  function escapeHtml(s: string): string {
    return s.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  }

  function between(text: string, mark: string, open: string, close: string): string {
    let out = ''
    let pos = 0
    while (pos < text.length) {
      const start = text.indexOf(mark, pos)
      if (start < 0) { out += text.substring(pos); break }
      const end = text.indexOf(mark, start + mark.length)
      if (end < 0) { out += text.substring(pos); break }
      out += text.substring(pos, start) + open + text.substring(start + mark.length, end) + close
      pos = end + mark.length
    }
    return out
  }

  function renderText(text: string): string {
    let out = ''
    let inCode = false
    for (const line of text.split('\n')) {
      if (line.startsWith('```')) {
        out += inCode ? '</code></pre>' : '<pre class="tr-code"><code>'
        inCode = !inCode
        continue
      }
      if (inCode) { out += escapeHtml(line) + '\n'; continue }
      out += between(between(escapeHtml(line), '**', '<b>', '</b>'), '`', '<code class="tr-inline-code">', '</code>') + '\n'
    }
    if (inCode) out += '</code></pre>'
    return out
  }

  function when(at: string): string {
    const d = new Date(msOf(at))
    return d.toLocaleString('en', { month: 'short' }) + ' ' + d.getDate() + ' ' +
      String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0')
  }

  const small = new Set(['tool', 'edit', 'read', 'search', 'write'])
  const marker = new Set(['session', 'compaction', 'agent', 'task', 'rite'])
  function weight(speaker: string): string {
    if (small.has(speaker)) return 'small'
    if (marker.has(speaker)) return 'marker'
    return speaker
  }
</script>

<div class="tr">
  {#if !session}
    <div class="tr-said">{said}</div>
    {#each sessions as s}
      <button class="tr-session" onclick={() => onChoose(s.session)}>
        <span class="tr-when">{when(s.started)}</span>
        <span class="tr-opening">{s.turns.find(t => t.speaker === 'human')?.text ?? s.session}</span>
      </button>
    {/each}
  {:else}
    <div class="tr-head">
      <span class="tr-sid">{session.substring(0, 8)}</span>
      {#if turns.length > 0}<span class="tr-when">{when(turns[0].at)}</span>{/if}
      <span class="tr-when">{turns.length}t</span>
      {#if said}<span class="tr-when">{said}</span>{/if}
    </div>
    <div class="tr-body">
      <div class="tr-col" bind:this={columnEl}>
        {#each turns as turn, i (turn.of)}
          {#each timeSpacers(i > 0 ? msOf(turns[i - 1].at) : 0, msOf(turn.at)) as px}
            <div class="tr-spacer" style="height: {px}px"></div>
          {/each}
          <div class="tr-turn {weight(turn.speaker)} sp-{turn.speaker}" title={turn.of} role="button" tabindex="0"
            onclick={() => onOpen(turn)} onkeydown={(e) => { if (e.key === 'Enter') onOpen(turn) }}>
            <span class="tr-speaker">[{turn.speaker}]</span>
            {#if turn.speaker === 'assistant'}
              <span class="tr-text">{@html renderText(turn.text)}</span>
            {:else}
              <span class="tr-text">{turn.text}</span>
            {/if}
          </div>
        {/each}
      </div>
      <Warp {turns} {columnEl} />
    </div>
  {/if}
</div>

<style>
  .tr {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    color: var(--text-on-dark);
  }
  .tr-said { color: var(--text-on-dark-tertiary); padding: 4px 6px; }
  .tr-session {
    display: flex;
    gap: 8px;
    align-items: baseline;
    text-align: left;
    background: none;
    border: none;
    border-bottom: 1px solid var(--bg-secondary);
    color: inherit;
    font: inherit;
    padding: 3px 6px;
    cursor: pointer;
  }
  .tr-session:hover { background: var(--bg-dark-hover); }
  .tr-opening { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tr-head {
    display: flex;
    gap: 8px;
    align-items: baseline;
    padding: 3px 6px;
    border-bottom: 1px solid var(--border-on-dark);
    flex-shrink: 0;
  }
  .tr-sid { color: var(--text-on-dark-secondary); font-weight: 500; }
  .tr-when { color: var(--text-secondary); font-size: var(--font-size-xs, 10px); white-space: nowrap; }
  .tr-body { display: flex; flex: 1; min-height: 0; }
  .tr-col { flex: 1; overflow-y: auto; scrollbar-width: none; padding: 2px 0; }
  .tr-col::-webkit-scrollbar { display: none; }
  .tr-spacer {
    margin: 0 6px;
    border-left: 1px dashed var(--border-on-dark);
    border-right: 1px dashed var(--border-on-dark);
  }

  .tr-turn { cursor: pointer; padding: 0 6px; overflow-wrap: break-word; word-break: break-word; }
  .tr-speaker { font-weight: 500; font-size: var(--font-size-xs, 10px); margin-right: 4px; }
  .tr-text { font-size: 11px; line-height: 1.3; white-space: pre-wrap; }

  .sp-human .tr-speaker { color: var(--accent-on-dark); }
  .sp-assistant .tr-speaker { color: var(--element-status-running-text); }

  .small {
    background: var(--bg-almost-black);
    border-left: 2px solid var(--element-status-running-text);
    margin: 1px 6px;
    padding-left: 4px;
  }
  .small .tr-speaker { color: var(--element-status-running-text); }
  .small .tr-text { color: var(--text-on-dark-secondary); font-size: 8px; }
  .sp-tool { border-left-color: var(--color-warning); }
  .sp-tool .tr-speaker { color: var(--color-warning); }

  .ground {
    background: var(--bg-almost-black);
    border-left: 2px solid var(--color-error);
    margin: 1px 6px;
    padding-left: 4px;
  }
  .ground .tr-speaker { color: var(--color-error); }
  .ground .tr-text { color: var(--text-on-dark-secondary); font-size: 8px; }

  .marker { font-style: italic; }
  .marker .tr-speaker { color: var(--color-scheduled); }
  .marker .tr-text { color: var(--text-secondary); font-size: 10px; }

  :global(.tr-code) {
    background: var(--bg-almost-black);
    border: 1px solid var(--border-on-dark);
    padding: 3px 6px;
    margin: 2px 0;
    font-size: 11px;
    white-space: pre-wrap;
  }
  :global(.tr-inline-code) {
    background: var(--bg-almost-black);
    padding: 0 3px;
    color: var(--text-on-dark-secondary);
  }
</style>
