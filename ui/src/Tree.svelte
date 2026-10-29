<script lang="ts">
  import type { TreeEntry } from './api'

  let {
    entries,
    dir,
    ws,
    activePath,
    onopen,
    ondir,
  }: {
    entries: TreeEntry[]
    dir: string
    ws: string
    activePath: string
    /** Open one file. */
    onopen: (path: string) => void
    /** Move to a directory, '' being the root. */
    ondir: (dir: string) => void
  } = $props()

  const segments = $derived(dir.split('/').filter(Boolean))

  function upTo(i: number): string {
    return segments.slice(0, i + 1).join('/')
  }
</script>

<!-- Breadcrumbs are also the way back up: a ".." row alone makes a deep path hard to leave one
     level at a time. -->
<nav class="crumb">
  <button onclick={() => ondir('')}>{ws.split('/').pop() ?? ws}</button>
  {#each segments as seg, i (i)}
    <span class="muted">/</span>
    <button onclick={() => ondir(upTo(i))}>{seg}</button>
  {/each}
</nav>

{#if dir}
  <button class="row" onclick={() => ondir(segments.slice(0, -1).join('/'))}>
    <span class="kind muted mono">..</span>
  </button>
{/if}

{#each entries as e (e.path)}
  {@const name = e.path.split('/').pop() ?? e.path}
  <button
    class="row"
    class:active={e.path === activePath}
    data-dir={e.dir ? 'true' : 'false'}
    title={e.path}
    onclick={() => (e.dir ? ondir(e.path) : onopen(e.path))}
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
    /* 26px is comfortable for a mouse and too small for a thumb; the narrow-screen block below
       raises it. Kept explicit rather than inherited so the row height does not drift with the
       reader's font size. */
    min-height: 26px;
    padding: 3px 6px;
    border-radius: var(--radius-sm);
    text-align: left;
  }
  .row:hover {
    background: var(--secondary);
  }
  /* Keyboard focus must be visible: the tree is a long list of buttons, and tabbing through it with
     no indicator is unusable. :focus-visible so a mouse click does not leave a ring behind. */
  .row:focus-visible,
  .crumb button:focus-visible {
    outline: 2px solid var(--primary);
    outline-offset: -2px;
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
    /* A directory is a place and a file is a thing; the arrow is what makes that legible before the
       colour is noticed. */
    color: var(--muted-foreground);
  }
  .row[data-dir='true'] .kind {
    color: var(--primary);
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

  /**
   * NARROW SCREENS: the rows become thumb targets.
   *
   * 44px is the usual floor and this sits just under it at 40px — the tree is a dense list, and
   * pushing every row to 44 costs a visible number of entries per screen. The rows are still
   * separated by hover/active backgrounds, which is what a thumb actually aims at.
   */
  @media (max-width: 720px) {
    .row {
      min-height: 40px;
      padding: 8px 8px;
    }
    .crumb button {
      min-height: 36px;
    }
  }
</style>
