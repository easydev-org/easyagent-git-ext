<script lang="ts">
  // A repository's recent CI runs.
  //
  // IT POLLS WHILE ANYTHING IS RUNNING. A list of CI runs is the one list here whose contents change
  // on their own while it is open, and a page that shows a run as "in progress" twenty minutes after
  // it finished is worse than one that never claimed to be live. The poll stops as soon as every run
  // is done, so a finished page costs nothing.
  import { onDestroy, onMount } from 'svelte'
  import { api, type RunView } from './api'

  let { repo }: { repo: string } = $props()

  let runs = $state<RunView[]>([])
  let error = $state('')
  let loading = $state(true)
  let timer: ReturnType<typeof setTimeout> | null = null

  async function load(): Promise<void> {
    try {
      runs = await api.runs(repo, 25)
      error = ''
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  function schedule(): void {
    if (timer) clearTimeout(timer)
    // Ten seconds: often enough to see a step land, rare enough not to spend the rate limit.
    timer = setTimeout(() => {
      void load().then(() => {
        if (runs.some(r => !r.done)) schedule()
      })
    }, 10_000)
  }

  onMount(() => {
    void load().then(() => {
      if (runs.some(r => !r.done)) schedule()
    })
  })

  onDestroy(() => {
    if (timer) clearTimeout(timer)
  })

  /** The word to show for a run: its conclusion once it has one, else its status. */
  function stateOf(r: RunView): string {
    return r.conclusion || r.status
  }

  /** Colour by outcome. A run that is still going gets neither green nor red — it has no verdict. */
  function tone(r: RunView): string {
    if (!r.done) return 'run'
    switch (r.conclusion) {
      case 'success':
        return 'ok'
      case 'failure':
      case 'timed_out':
        return 'bad'
      case 'cancelled':
      case 'skipped':
        return 'warn'
      default:
        return 'muted'
    }
  }
</script>

{#if error}
  <div class="state err">
    <div>{error}</div>
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else if loading}
  <div class="state muted">Loading…</div>
{:else if runs.length === 0}
  <div class="state muted">No runs in this repository.</div>
{:else}
  <div class="page">
    <div class="rule">
      <header class="head">
        <h2 class="title">{repo}</h2>
        <span class="muted t-meta">
          {runs.length} run{runs.length === 1 ? '' : 's'}
          {#if runs.some(r => !r.done)}· updating{/if}
        </span>
      </header>
    </div>
    <div class="body-scroll">
      <ul class="runs">
        {#each runs as r (r.id)}
          <li>
            <a class="run" href={`#/run/${repo}/${r.id}`}>
              <span class="badge {tone(r)}">{stateOf(r)}</span>
              <span class="grow">
                <span class="name ellipsis">{r.name}</span>
                <span class="sub mono muted ellipsis">
                  {r.branch}{#if r.event} · {r.event}{/if}{#if r.attempt > 1} · attempt {r.attempt}{/if}
                </span>
              </span>
              <span class="mono muted sha">{(r.headSha || '').slice(0, 7)}</span>
            </a>
          </li>
        {/each}
      </ul>
    </div>
  </div>
{/if}

<style>
  .page {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
  }
  .rule { border-bottom: 1px solid var(--border); }
  .head {
    padding: var(--sp-md);
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    flex-wrap: wrap;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
    /* A FLEX CHILD SHRINKS BY DEFAULT. Inside a column with a scrolling list below, a header that
       does not opt out of shrinking gets squashed as the list takes the space — its text clips
       rather than the layout scrolling. */
    flex: 0 0 auto;
  }
  .title { margin: 0; font-size: 1.05rem; font-weight: 600; overflow-wrap: anywhere; }
  .body-scroll {
    overflow: auto;
    padding: var(--sp-md);
    min-height: 0;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .runs { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .run {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    padding: var(--sp-sm);
    border-radius: var(--radius-sm);
    text-decoration: none;
    color: inherit;
    min-height: 44px;
  }
  .run:hover { background: var(--secondary); }
  .run:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
  .grow { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .name { font-size: var(--text-body, 14px); }
  .sub { font-size: var(--text-micro, 10px); }
  .sha { flex: 0 0 auto; font-size: var(--text-micro, 10px); }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .badge {
    flex: 0 0 auto;
    font-size: var(--text-micro, 10px);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 2px 9px;
    border-radius: 999px;
    font-weight: 600;
    min-width: 5.5em;
    text-align: center;
  }
  .badge.ok {
    color: var(--success, #1a7f37);
    background: color-mix(in srgb, var(--success, #1a7f37) 14%, transparent);
  }
  .badge.bad {
    color: var(--destructive, #cf222e);
    background: color-mix(in srgb, var(--destructive, #cf222e) 14%, transparent);
  }
  .badge.warn {
    color: var(--warning, #9a6700);
    background: color-mix(in srgb, var(--warning, #9a6700) 16%, transparent);
  }
  .badge.run {
    color: var(--primary);
    background: color-mix(in srgb, var(--primary) 14%, transparent);
  }
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
    /* The sha is the first thing to go: the branch already identifies the work at this width. */
    .sha { display: none; }
    .retry { padding: 10px 16px; }
  }
</style>
