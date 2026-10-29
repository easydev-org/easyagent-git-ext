<script lang="ts">
  // One CI run: its jobs, each job's steps, and — on request — that job's log.
  //
  // LOGS ARE FETCHED WHEN OPENED, never up front. A run's logs are megabytes across its jobs, and a
  // reader opens this page to see WHERE it is, which the step list answers in a few hundred bytes. A
  // page that fetched every log would spend seconds before showing anything, for data most readers
  // never scroll to.
  //
  // IT POLLS WHILE THE RUN IS GOING. Steps turn green one at a time; that is the whole reason a page
  // beats a card here. The poll stops when the run completes.
  import { onDestroy, onMount } from 'svelte'
  import { api, type JobView, type RunView } from './api'

  let { repo, id }: { repo: string; id: number } = $props()

  let run = $state<RunView | null>(null)
  let jobs = $state<JobView[]>([])
  let error = $state('')
  let loading = $state(true)
  let timer: ReturnType<typeof setTimeout> | null = null

  // Which job's log is open, and its text per job id — kept so re-opening does not refetch.
  let openLog = $state<number | null>(null)
  let logs = $state<Record<number, string>>({})
  let logError = $state<Record<number, string>>({})
  let logLoading = $state<number | null>(null)

  async function load(): Promise<void> {
    try {
      const [r, j] = await Promise.all([api.run(repo, id), api.runJobs(repo, id)])
      run = r
      jobs = j
      error = ''
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  function schedule(): void {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      void load().then(() => {
        if (run && !run.done) schedule()
      })
    }, 8000)
  }

  onMount(() => {
    void load().then(() => {
      if (run && !run.done) schedule()
    })
  })

  onDestroy(() => {
    if (timer) clearTimeout(timer)
  })

  /** Toggle one job's log, fetching it the first time it is opened. */
  async function toggleLog(job: JobView): Promise<void> {
    if (openLog === job.id) {
      openLog = null
      return
    }
    openLog = job.id
    if (logs[job.id] !== undefined || logError[job.id]) return
    logLoading = job.id
    try {
      const text = await api.jobLog(repo, job.id)
      logs = { ...logs, [job.id]: text }
    } catch (e) {
      // A job that has not finished has no log yet; the server says so with a 409 and the message is
      // what the reader needs, not "failed".
      logError = { ...logError, [job.id]: e instanceof Error ? e.message : String(e) }
    } finally {
      logLoading = null
    }
  }

  function stateOf(x: { status: string; conclusion: string }): string {
    return x.conclusion || x.status
  }

  function tone(x: { status: string; conclusion: string }): string {
    if (x.status !== 'completed') return 'run'
    switch (x.conclusion) {
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

  /** How far through its steps a job is, for the progress bar. */
  function progress(j: JobView): { done: number; total: number } {
    const steps = j.steps ?? []
    const done = steps.filter(s => s.status === 'completed' || s.conclusion !== '').length
    return { done, total: steps.length }
  }
</script>

{#if error}
  <div class="state err">
    <div>{error}</div>
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else if loading || !run}
  <div class="state muted">Loading…</div>
{:else}
  <div class="page">
    <div class="rule">
      <header class="head">
        <div class="title-row">
          <span class="badge {tone(run)}">{stateOf(run)}</span>
          <h2 class="title">{run.name}</h2>
          {#if !run.done}<span class="muted t-micro">· updating</span>{/if}
        </div>
        <div class="meta">
          <span class="mono">{run.branch}</span>
          {#if run.event}<span>· {run.event}</span>{/if}
          <span class="mono muted">{(run.headSha || '').slice(0, 7)}</span>
          <a class="ext" href={run.url} target="_blank" rel="noreferrer">open on GitHub ↗</a>
        </div>
      </header>
    </div>

    <div class="body-scroll">
      {#if jobs.length === 0}
        <div class="muted">This run has no jobs yet.</div>
      {:else}
        <ul class="jobs">
          {#each jobs as j (j.id)}
            {@const pg = progress(j)}
            <li class="job">
              <div class="job-head">
                <span class="badge {tone(j)}">{stateOf(j)}</span>
                <span class="job-name grow ellipsis">{j.name}</span>
                {#if pg.total > 0}
                  <span class="muted t-micro nowrap">{pg.done}/{pg.total}</span>
                {/if}
                {#if j.logAvailable}
                  <button
                    type="button"
                    class="logbtn"
                    onclick={() => void toggleLog(j)}
                    aria-expanded={openLog === j.id}
                  >
                    {openLog === j.id ? 'Hide log' : 'Show log'}
                  </button>
                {:else}
                  <!-- No button for a job that has none: a control that cannot work is worse than
                       the absence of one. -->
                  <span class="muted t-micro nowrap">log after it finishes</span>
                {/if}
              </div>

              {#if (j.steps ?? []).length > 0}
                <ol class="steps">
                  {#each j.steps ?? [] as s (s.number)}
                    <li class="step">
                      <span class="mark {tone(s)}" aria-hidden="true"></span>
                      <span class="ellipsis">{s.name}</span>
                      {#if s.conclusion && s.conclusion !== 'success'}
                        <span class="t-micro {tone(s)}">{s.conclusion}</span>
                      {/if}
                    </li>
                  {/each}
                </ol>
              {/if}

              {#if openLog === j.id}
                <div class="log">
                  {#if logLoading === j.id}
                    <div class="muted">Loading log…</div>
                  {:else if logError[j.id]}
                    <div class="muted">{logError[j.id]}</div>
                  {:else if logs[j.id] === ''}
                    <div class="muted">(this job produced no output)</div>
                  {:else}
                    <pre class="pre">{logs[j.id]}</pre>
                  {/if}
                </div>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
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
    flex-direction: column;
    gap: var(--sp-xs, 4px);
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .title-row { display: flex; align-items: baseline; gap: var(--sp-sm); flex-wrap: wrap; min-width: 0; }
  .title { margin: 0; font-size: 1.05rem; font-weight: 600; overflow-wrap: anywhere; }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-md);
    font-size: var(--text-micro, 11px);
    color: var(--muted-foreground);
  }
  .ext { color: var(--primary); text-decoration: none; }
  .ext:hover { text-decoration: underline; }

  .body-scroll {
    overflow: auto;
    padding: var(--sp-md);
    min-height: 0;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .jobs { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sp-sm); }
  .job {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-sm);
    background: var(--card);
  }
  .job-head { display: flex; align-items: center; gap: var(--sp-sm); min-width: 0; }
  .job-name { font-weight: 600; font-size: var(--text-body, 14px); }
  .grow { flex: 1 1 auto; min-width: 0; }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .nowrap { white-space: nowrap; }

  .logbtn {
    font: inherit;
    font-size: var(--text-micro, 11px);
    padding: 4px 10px;
    flex: 0 0 auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--background);
    color: var(--foreground);
    cursor: pointer;
  }
  .logbtn:hover { background: var(--secondary); }
  .logbtn:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }

  .steps { list-style: none; margin: var(--sp-sm) 0 0; padding: 0 0 0 2px; display: flex; flex-direction: column; gap: 3px; }
  .step { display: flex; align-items: center; gap: var(--sp-sm); font-size: var(--text-meta, 12px); min-width: 0; }
  /* A dot, not an icon font: the state is a colour and a word beside it, so it still reads without
     the colour. */
  .mark { width: 7px; height: 7px; border-radius: 50%; flex: 0 0 auto; background: var(--muted-foreground); }
  .mark.ok { background: var(--success, #1a7f37); }
  .mark.bad { background: var(--destructive, #cf222e); }
  .mark.warn { background: var(--warning, #9a6700); }
  .mark.run { background: var(--primary); }

  .log { margin-top: var(--sp-sm); }
  .pre {
    margin: 0;
    max-height: 28rem;
    overflow: auto;
    padding: var(--sp-sm);
    border-radius: var(--radius-sm);
    background: var(--background);
    border: 1px solid var(--border);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: var(--text-micro, 11px);
    line-height: 1.5;
    white-space: pre;
  }

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
  .badge.ok { color: var(--success, #1a7f37); background: color-mix(in srgb, var(--success, #1a7f37) 14%, transparent); }
  .badge.bad { color: var(--destructive, #cf222e); background: color-mix(in srgb, var(--destructive, #cf222e) 14%, transparent); }
  .badge.warn { color: var(--warning, #9a6700); background: color-mix(in srgb, var(--warning, #9a6700) 16%, transparent); }
  .badge.run { color: var(--primary); background: color-mix(in srgb, var(--primary) 14%, transparent); }
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
    .logbtn { padding: 8px 12px; }
    .pre { max-height: 20rem; }
    .retry { padding: 10px 16px; }
  }
</style>
