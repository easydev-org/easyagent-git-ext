<script lang="ts">
  // A repository's workflow DEFINITIONS (the .github/workflows files), as opposed to its runs.
  //
  // WHY BOTH: a run answers "what happened"; a workflow answers "what can happen". A reader who
  // wants to know whether this repository even has a deploy pipeline is asking the second question,
  // and the run list only shows it if something has already run.
  import { onMount } from 'svelte'
  import { api, type WorkflowSummary } from './api'

  let { repo }: { repo: string } = $props()

  let flows = $state<WorkflowSummary[]>([])
  let error = $state('')
  let loading = $state(true)

  async function load(): Promise<void> {
    loading = true
    try {
      flows = await api.workflows(repo)
      error = ''
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  onMount(() => {
    void load()
  })
</script>

{#if error}
  <div class="state err">
    <div>{error}</div>
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else}
  <div class="page">
    <header class="head">
      <h2 class="title">Workflows</h2>
      <span class="muted t-meta">{flows.length}</span>
    </header>
    <div class="body-scroll">
      {#if loading}
        <div class="muted">Loading…</div>
      {:else if flows.length === 0}
        <div class="muted">No workflows in this repository.</div>
      {:else}
        <ul class="flows">
          {#each flows as w (w.id)}
            <li class="flow">
              <span class="badge {w.state === 'active' ? 'ok' : 'muted'}">
                {w.state === 'active' ? 'active' : w.state}
              </span>
              <span class="grow">
                <span class="name ellipsis">{w.name}</span>
                <span class="path mono muted ellipsis">{w.path}</span>
              </span>
              <!-- Straight to the runs of THIS workflow: the reason to look at a workflow is usually
                   to see what it did last. -->
              <a class="ext" href={`#/runs/${repo}`}>runs →</a>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
{/if}

<style>
  .page { display: flex; flex-direction: column; min-height: 0; height: 100%; }
  .head {
    padding: var(--sp-md);
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .title { margin: 0; font-size: 1.05rem; font-weight: 600; }
  .body-scroll {
    overflow: auto;
    padding: var(--sp-md);
    min-height: 0;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .flows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .flow {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    padding: var(--sp-sm);
    border-radius: var(--radius-sm);
    min-height: 44px;
  }
  .flow:hover { background: var(--secondary); }
  .grow { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .name { font-size: var(--text-body, 14px); }
  .path { font-size: var(--text-micro, 10px); }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ext {
    flex: 0 0 auto;
    font-size: var(--text-micro, 11px);
    color: var(--primary);
    text-decoration: none;
  }
  .ext:hover { text-decoration: underline; }

  .badge {
    flex: 0 0 auto;
    font-size: var(--text-micro, 10px);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 2px 9px;
    border-radius: 999px;
    font-weight: 600;
    min-width: 5em;
    text-align: center;
  }
  .badge.ok { color: var(--success, #1a7f37); background: color-mix(in srgb, var(--success, #1a7f37) 14%, transparent); }
  .badge.muted { color: var(--muted-foreground); background: var(--secondary); }

  .state { padding: var(--sp-lg); text-align: center; }
  .state.err {
    color: var(--destructive);
    display: flex;
    flex-direction: column;
    gap: var(--sp-sm);
    align-items: center;
  }
  .retry {
    font: inherit;
    font-size: var(--text-meta, 12px);
    padding: 6px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
    color: var(--foreground);
    cursor: pointer;
  }

  @media (max-width: 720px) {
    .head { padding: var(--sp-sm); }
    .body-scroll { padding: var(--sp-sm); }
    .retry { padding: 10px 16px; }
  }
</style>
