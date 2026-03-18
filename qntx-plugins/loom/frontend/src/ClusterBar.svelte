<script lang="ts">
  interface Weave {
    id: string
    [key: string]: any
  }

  interface ClusterEntry {
    cluster_id: number
    label: string | null
  }

  const CLUSTER_COLORS = [
    'rgba(125,186,138,0.12)', 'rgba(107,155,209,0.12)', 'rgba(212,184,255,0.12)',
    'rgba(255,171,0,0.12)', 'rgba(239,69,68,0.12)', 'rgba(34,198,94,0.12)',
    'rgba(59,131,246,0.12)', 'rgba(123,32,162,0.12)', 'rgba(224,128,80,0.12)',
    'rgba(80,176,176,0.12)', 'rgba(192,160,64,0.12)', 'rgba(208,96,144,0.12)',
  ]

  let {
    weaves,
    clusterMap,
  }: {
    weaves: Weave[]
    clusterMap: Map<string, ClusterEntry>
  } = $props()

  function distribution(): { cluster_id: number, label: string, pct: number, color: string }[] {
    const counts = new Map<number, { label: string, count: number }>()
    let total = 0
    for (const w of weaves) {
      const entry = clusterMap.get(w.id)
      if (!entry) continue
      total++
      const existing = counts.get(entry.cluster_id)
      if (existing) existing.count++
      else counts.set(entry.cluster_id, { label: entry.label || `cluster ${entry.cluster_id}`, count: 1 })
    }
    if (total === 0) return []
    return [...counts.entries()]
      .map(([id, { label, count }]) => ({
        cluster_id: id,
        label,
        pct: Math.round((count / total) * 100),
        color: CLUSTER_COLORS[id % CLUSTER_COLORS.length],
      }))
      .filter(c => c.pct >= 2)
      .sort((a, b) => b.pct - a.pct)
  }
</script>

{#if distribution().length > 0}
  <div class="dw-clusters">
    {#each distribution() as c}
      <span class="dw-cluster-chip" style="border-left-color: {c.color}; background: {c.color}">{c.label} {c.pct}%</span>
    {/each}
  </div>
{/if}

<style>
  .dw-clusters {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }

  .dw-cluster-chip {
    font-size: var(--font-size-xs);
    color: var(--text-on-dark-secondary);
    border-left: 2px solid;
    padding-left: 4px;
  }
</style>
