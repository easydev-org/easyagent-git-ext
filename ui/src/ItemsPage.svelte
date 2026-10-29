<script lang="ts">
  // A repository's issues, or its pull requests.
  //
  // ONE COMPONENT FOR BOTH because GitHub numbers them in one sequence and the two lists are the same
  // query with a filter. `kind` chooses which, and the only other difference is which page a row
  // opens — so two components would be the same file twice, drifting apart.
  import { onMount } from 'svelte'
  import { api, type IssueView } from './api'

  let { repo, kind }: { repo: string; kind: 'issue' | 'pr' } = $props()

  let items = $state<IssueView[]>([])
  let error = $state('')
  let loading = $state(true)
  let state = $state('open')

  async function load(): Promise<void> {
    loading = true
    try {
      items = await api.issues(repo, kind, state)
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

  function toggleState(): void {
    state = state === 'open' ? 'closed' : 'open'
    void load()
  }

  /** Where a row goes. A pull request has its own page; an issue has one too now. */
  function hrefOf(it: IssueView): string {
    return it.isPullRequest ? `#/pull/${repo}/${it.number}` : `#/issue/${repo}/${it.number}`
  }

  const plural = $derived(kind === 'pr' ? 'pull request' : 'issue')
</script>

{#if error}
  <div class="state err">
    <div>{error}</div>
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else}
  <div class="page">
    <header class="head">
      <h2 class="title">{plural === 'issue' ? 'Issues' : 'Pull requests'}</h2>
      <span class="muted t-meta">{items.length} {state}</span>
      <button type="button" class="toggle" onclick={toggleState}>
        show {state === 'open' ? 'closed' : 'open'}
      </button>
    </header>
    <div class="body-scroll">
      {#if loading}
        <div class="muted">Loading…</div>
      {:else if items.length === 0}
        <div class="muted">No {state} {plural}s.</div>
      {:else}
        <ul class="items">
          {#each items as it (it.number)}
            <li>
              <a class="item" href={hrefOf(it)}>
                <span class="badge {it.state === 'open' ? 'open' : 'closed'}">{it.state}</span>
                <span class="grow">
                  <span class="name ellipsis">{it.title}</span>
                  <span class="sub muted ellipsis">
                    #{it.number}{#if it.author} · {it.author}{/if}
                    {#if it.comments > 0}· {it.comments} comment{it.comments === 1 ? '' : 's'}{/if}
                  </span>
                </span>
                {#if (it.labels ?? []).length > 0}
                  <span class="labels">
                    {#each (it.labels ?? []).slice(0, 2) as l (l)}<span class="label">{l}</span>{/each}
                  </span>
                {/if}
              </a>
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
    flex-wrap: wrap;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .title { margin: 0; font-size: 1.05rem; font-weight: 600; }
  .toggle {
    font: inherit;
    font-size: var(--text-micro, 11px);
    margin-left: auto;
    padding: 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
    color: var(--foreground);
    cursor: pointer;
  }
  .toggle:hover { background: var(--secondary); }
  .toggle:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }

  .body-scroll {
    overflow: auto;
    padding: var(--sp-md);
    min-height: 0;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .items { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .item {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    padding: var(--sp-sm);
    border-radius: var(--radius-sm);
    text-decoration: none;
    color: inherit;
    min-height: 44px;
  }
  .item:hover { background: var(--secondary); }
  .item:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
  .grow { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .name { font-size: var(--text-body, 14px); }
  .sub { font-size: var(--text-micro, 10px); }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

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
  .badge.open { color: var(--success, #1a7f37); background: color-mix(in srgb, var(--success, #1a7f37) 14%, transparent); }
  .badge.closed { color: var(--destructive, #cf222e); background: color-mix(in srgb, var(--destructive, #cf222e) 14%, transparent); }

  .labels { display: flex; gap: 4px; flex: 0 1 auto; overflow: hidden; }
  .label {
    font-size: var(--text-micro, 10px);
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--secondary);
    color: var(--secondary-foreground, var(--foreground));
    white-space: nowrap;
  }

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
    /* Labels go first: at this width they take the room the title needs. */
    .labels { display: none; }
    .toggle { padding: 8px 12px; }
    .retry { padding: 10px 16px; }
  }
</style>
