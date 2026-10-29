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

  /**
   * Which jobs' logs are open — a SET, not one id.
   *
   * ONE AT A TIME WAS WRONG. Reading a failed CI run is usually a comparison — "did unit fail
   * before ui did?" — and a panel that closed the first log when the second opened made that
   * impossible. Each job has its own button and its own box directly under it; opening one no longer
   * closes another.
   */
  let openLogs = $state<Set<number>>(new Set())
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

  /** Toggle one job's log. Independent of every other job's. */
  async function toggleLog(job: JobView): Promise<void> {
    // A NEW Set each time: mutating the existing one would not notify Svelte, because the reference
    // has not changed.
    const next = new Set(openLogs)
    if (next.has(job.id)) {
      next.delete(job.id)
      openLogs = next
      return
    }
    next.add(job.id)
    openLogs = next

    // Already fetched, or already known to have no log: nothing more to do.
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

  /** How many jobs could be opened at all — used to decide whether "open all" is worth showing. */
  const openableJobs = $derived(jobs.filter(j => j.logAvailable))

  /** Open every finished job's log, or close them all if they are all already open. */
  async function toggleAll(): Promise<void> {
    const allOpen = openableJobs.length > 0 && openableJobs.every(j => openLogs.has(j.id))
    if (allOpen) {
      openLogs = new Set()
      return
    }
    const next = new Set(openLogs)
    for (const j of openableJobs) next.add(j.id)
    openLogs = next
    // Fetch the ones that are missing, one at a time — in parallel they would all hit the rate limit
    // at once, and the reader is looking at the first one anyway.
    for (const j of openableJobs) {
      if (logs[j.id] === undefined && !logError[j.id]) {
        logLoading = j.id
        try {
          logs = { ...logs, [j.id]: await api.jobLog(repo, j.id) }
        } catch (e) {
          logError = { ...logError, [j.id]: e instanceof Error ? e.message : String(e) }
        } finally {
          logLoading = null
        }
      }
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

  /**
   * Render a job log as lines, dropping GitHub's own markers.
   *
   * `##[group]` / `##[endgroup]` are instructions to GitHub's own log viewer, not content. Shown
   * raw they are noise in the middle of the thing the reader came to read — and a build log is
   * already dense enough.
   *
   * The timestamp is SPLIT OFF into its own span so it can be dimmed: every line starts with one, so
   * at full contrast the eye lands on thirty identical timestamps before finding a line of output.
   */
  function logLines(text: string): { ts: string; body: string }[] {
    return text
      .split('\n')
      .filter(l => l !== '' && !l.includes('##[group]') && !l.includes('##[endgroup]'))
      .map(l => {
        // The FIRST line of a GitHub job log starts with a UTF-8 BOM, which defeats a regex anchored
        // at `^` and left the timestamp in the body where it could not be dimmed. Stripped first.
        const clean = l.replace(/^\uFEFF/, '')
        // ISO-8601 with a trailing Z, which is what the runner emits.
        const m = clean.match(/^(\d{4}-\d{2}-\d{2}T[\d:.]+Z)\s(.*)$/)
        return m ? { ts: m[1], body: m[2] } : { ts: '', body: clean }
      })
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
        {#if openableJobs.length > 1}
          <!-- Only worth offering when there IS more than one log: a "expand all" with one item is
               a control that does what the row already does. -->
          <div class="jobs-bar">
            <span class="muted t-micro">
              {jobs.length} job{jobs.length === 1 ? '' : 's'}
            </span>
            <button type="button" class="allbtn" onclick={() => void toggleAll()}>
              {openableJobs.every(j => openLogs.has(j.id)) ? 'Hide all logs' : 'Show all logs'}
            </button>
          </div>
        {/if}
        <ul class="jobs">
          {#each jobs as j (j.id)}
            {@const pg = progress(j)}
            {@const opened = openLogs.has(j.id)}
            <li class="job">
              <!-- THE WHOLE ROW IS THE CONTROL, and the chevron is its affordance. A dedicated
                   button at the right edge is a small target next to a large one that looks live;
                   making the row clickable means a reader who clicks the job's NAME gets the log,
                   which is what they were reaching for. -->
              <button
                type="button"
                class="job-head"
                onclick={() => j.logAvailable && void toggleLog(j)}
                disabled={!j.logAvailable}
                aria-expanded={j.logAvailable ? opened : undefined}
              >
                <span class="chev" class:open={opened} aria-hidden="true">
                  {j.logAvailable ? '▸' : '·'}
                </span>
                <span class="badge {tone(j)}">{stateOf(j)}</span>
                <span class="job-name grow ellipsis">{j.name}</span>
                {#if pg.total > 0}
                  <span class="muted t-micro nowrap">{pg.done}/{pg.total}</span>
                {/if}
                {#if !j.logAvailable}
                  <!-- Said in place of the chevron-less row: a job still running HAS no log, and a
                       row that looks expandable but does nothing is worse than one that explains. -->
                  <span class="muted t-micro nowrap">log after it finishes</span>
                {/if}
              </button>

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

              {#if opened}
                <div class="log">
                  {#if logLoading === j.id}
                    <div class="muted">Loading log…</div>
                  {:else if logError[j.id]}
                    <div class="muted">{logError[j.id]}</div>
                  {:else if logs[j.id] === ''}
                    <div class="muted">(this job produced no output)</div>
                  {:else}
                    <div class="pre">
                      {#each logLines(logs[j.id]) as line, i (i)}
                        <div class="line">
                          {#if line.ts}<span class="ts">{line.ts}</span>{/if}<span class="body">{line.body}</span>
                        </div>
                      {/each}
                    </div>
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
    /* A FLEX CHILD SHRINKS BY DEFAULT. Inside a column with a scrolling list below, a header that
       does not opt out of shrinking gets squashed as the list takes the space — its text clips
       rather than the layout scrolling. */
    flex: 0 0 auto;
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
  .jobs-bar {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-sm);
    margin-bottom: var(--sp-xs, 4px);
  }
  .allbtn {
    font: inherit;
    font-size: var(--text-micro, 11px);
    padding: 3px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--background);
    color: var(--foreground);
    cursor: pointer;
  }
  .allbtn:hover { background: var(--secondary); }
  .allbtn:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }

  /* The whole row is the button: it is the largest target and the one a reader aims at when they
     click a job's name. Styled to look like a row, not like the UA's default button. */
  .job-head {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    min-width: 0;
    width: 100%;
    font: inherit;
    color: inherit;
    text-align: left;
    background: none;
    border: 0;
    padding: 0;
    cursor: pointer;
  }
  .job-head:hover .job-name { text-decoration: underline; }
  .job-head:focus-visible { outline: 2px solid var(--primary); outline-offset: 3px; border-radius: var(--radius-sm); }
  /* A job with no log is not a control: it says why and does nothing. */
  .job-head:disabled { cursor: default; }
  .job-head:disabled:hover .job-name { text-decoration: none; }

  /* The chevron is the affordance. Rotating it rather than swapping the glyph keeps the row's width
     stable, which stops the name from shifting when a log opens. */
  .chev {
    flex: 0 0 auto;
    width: 1em;
    color: var(--muted-foreground);
    transition: transform 0.12s ease;
  }
  .chev.open { transform: rotate(90deg); }

  .job-name { font-weight: 600; font-size: var(--text-body, 14px); }
  .grow { flex: 1 1 auto; min-width: 0; }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .nowrap { white-space: nowrap; }

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
  }
  /* WRAPPED, not scrolling sideways. A build log has long paths and URLs; a horizontal scrollbar
     inside a vertically scrolling box means the reader has to move in two directions to read one
     message, and the second one is the one they forget is there.

     `pre-wrap` keeps the significant whitespace that makes a log readable — indentation, aligned
     columns — while still breaking a line that would otherwise overflow. */
  .line {
    display: flex;
    gap: var(--sp-sm);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  /* Dimmed: EVERY line starts with a timestamp, so at full contrast the eye lands on thirty
     identical numbers before it finds a line of output. */
  .ts {
    flex: 0 0 auto;
    color: var(--muted-foreground);
    opacity: 0.75;
  }
  .body { flex: 1 1 auto; min-width: 0; }

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
