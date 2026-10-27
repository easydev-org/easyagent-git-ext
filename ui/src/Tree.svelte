<script lang="ts">
  import type { TreeEntry } from './api'

  let {
    entries,
    dir,
    ws,
    activePath,
    go,
  }: {
    entries: TreeEntry[]
    dir: string
    ws: string
    activePath: string
    go: (hash: string) => void
  } = $props()

  const segments = $derived(dir.split('/').filter(Boolean))

  function upTo(i: number): string {
    return segments.slice(0, i + 1).join('/')
  }
</script>

<!-- Breadcrumbs are also the way back up: a ".." row alone makes a deep path hard to leave one
     level at a time. -->
<nav class="crumb">
  <button onclick={() => go(`#/ws/${ws}/tree/`)}>repo</button>
  {#each segments as seg, i (i)}
    <span class="muted">/</span>
    <button onclick={() => go(`#/ws/${ws}/tree/${upTo(i)}`)}>{seg}</button>
  {/each}
</nav>

{#if dir}
  <button class="row" onclick={() => go(`#/ws/${ws}/tree/${segments.slice(0, -1).join('/')}`)}>
    <span class="kind muted mono">..</span>
  </button>
{/if}

{#each entries as e (e.path)}
  {@const name = e.path.split('/').pop() ?? e.path}
  <button
    class="row"
    class:active={e.path === activePath}
    title={e.path}
    onclick={() => go(e.dir ? `#/ws/${ws}/tree/${e.path}` : `#/ws/${ws}/blob/${e.path}`)}
  >
    <span class="kind mono muted">{e.dir ? 'dir' : 'file'}</span>
    <span class="name">{name}</span>
  </button>
{:else}
  <div class="empty muted t-meta">Empty directory.</div>
{/each}

<style>
  .crumb {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px;
    padding-bottom: var(--sp-sm);
  }
  .crumb button {
    font: inherit;
    color: inherit;
    background: none;
    border: 0;
    cursor: pointer;
    padding: 1px 4px;
    border-radius: var(--radius-sm);
    max-width: 12em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .crumb button:hover {
    background: var(--secondary);
  }
  .row {
    font: inherit;
    color: inherit;
    background: none;
    border: 0;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 3px 6px;
    border-radius: var(--radius-sm);
    text-align: left;
  }
  .row:hover {
    background: var(--secondary);
  }
  .row.active {
    background: var(--secondary);
    color: var(--primary);
  }
  .kind {
    flex: 0 0 auto;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty {
    padding: var(--sp-md);
    text-align: center;
  }
</style>
