<script lang="ts">
  import type { Change } from './api'

  let {
    changes,
    branch,
    diff,
    ws,
    go,
  }: {
    changes: Change[]
    branch: string
    diff: string
    ws: string
    go: (hash: string) => void
  } = $props()

  // Colour by the diff's own line prefixes rather than parsing hunks: a unified diff already encodes
  // add/remove/context, and re-deriving it would be a second parser to keep correct.
  function cls(line: string): string {
    if (line.startsWith('+++') || line.startsWith('---')) return 'meta'
    if (line.startsWith('@@')) return 'hunk'
    if (line.startsWith('+')) return 'add'
    if (line.startsWith('-')) return 'del'
    return ''
  }
</script>

{#if branch}
  <div class="t-meta muted">branch <span class="mono">{branch}</span></div>
{/if}

{#if changes.length === 0}
  <div class="state muted">No local changes.</div>
{:else}
  <ul class="files">
    {#each changes as c (c.Path)}
      <li>
        <button onclick={() => go(`#/ws/${ws}/blob/${c.Path}`)}>
          <span class="status mono t-micro">{c.Status}</span>
          <span class="path">{c.Path}</span>
        </button>
      </li>
    {/each}
  </ul>
  <pre class="mono diff"
    >{#each diff.split('\n') as line}<span class={cls(line)}>{line || ' '}
</span>{/each}</pre
  >
{/if}

<style>
  .files {
    list-style: none;
    margin: var(--sp-sm) 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .files button {
    font: inherit;
    color: inherit;
    background: none;
    border: 0;
    cursor: pointer;
    display: flex;
    gap: var(--sp-sm);
    width: 100%;
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    text-align: left;
  }
  .files button:hover {
    background: var(--secondary);
  }
  .status {
    flex: 0 0 auto;
    width: 5.5em;
    color: var(--muted-foreground);
    text-transform: uppercase;
  }
  .path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .diff {
    margin: var(--sp-sm) 0 0;
    padding: var(--sp-md);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: auto;
    font-size: 12px;
    line-height: 1.45;
  }
  .diff :global(.add) {
    color: var(--success);
  }
  .diff :global(.del) {
    color: var(--destructive);
  }
  .diff :global(.hunk) {
    color: var(--primary);
  }
  .diff :global(.meta) {
    color: var(--muted-foreground);
  }
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }
</style>
