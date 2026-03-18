<script lang="ts">
  let {
    branches,
    branchColor,
  }: {
    branches: string[]
    branchColor: (branch: string) => string
  } = $props()
</script>

{#if branches.length > 0}
  <div class="dw-branches">
    {#each branches as branch}
      <span class="dw-branch" style="border-left-color: {branchColor(branch)}">{branch}</span>
    {/each}
  </div>
{/if}

<style>
  .dw-branches {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }

  .dw-branch {
    font-size: var(--font-size-xs);
    color: var(--text-on-dark-tertiary);
    border-left: 2px solid;
    padding-left: 4px;
    overflow-wrap: break-word;
  }
</style>
