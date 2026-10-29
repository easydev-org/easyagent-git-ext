<script lang="ts">
  // A pull request as a PAGE: current facts plus what it changes.
  //
  // Unlike a card, this reads GitHub. A card renders the result a tool already returned — a snapshot
  // of the moment it ran. A page is somewhere the reader goes to answer "what does this look like
  // NOW", and somebody may have pushed or closed it since.
  //
  // Two tabs, because the two halves are read at different times: the summary when deciding whether
  // to look, the files when actually reviewing.
  import { onMount } from 'svelte'
  import { api, type PullChecks, type PullFile, type PullView } from './api'

  let { repo, number }: { repo: string; number: number } = $props()

  let pull = $state<PullView | null>(null)
  let files = $state<PullFile[]>([])
  let checks = $state<PullChecks | null>(null)
  let error = $state('')
  let loading = $state(true)
  let tab = $state<'overview' | 'files'>('overview')
  /** Which file's diff is expanded. One at a time: a review is read a file at a time. */
  let openFile = $state('')

  async function load(): Promise<void> {
    error = ''
    loading = true
    try {
      // In parallel: three independent reads, and a page that appears in one round trip reads
      // better than one that fills in.
      const [p, f, c] = await Promise.all([
        api.pull(repo, number),
        api.pullFiles(repo, number),
        api.pullChecks(repo, number).catch(() => null),
      ])
      pull = p
      files = f
      checks = c
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  onMount(() => {
    void load()
  })

  const totals = $derived({
    adds: files.reduce((a, f) => a + f.additions, 0),
    dels: files.reduce((a, f) => a + f.deletions, 0),
  })

  /** State colouring, matching the host's success/destructive vocabulary. */
  function stateTone(s: string): string {
    switch (s.toLowerCase()) {
      case 'open':
      case 'success':
        return 'ok'
      case 'closed':
        return 'muted'
      case 'merged':
        return 'merged'
      case 'failure':
      case 'error':
        return 'bad'
      default:
        return 'muted'
    }
  }

  function fileTone(s: string): string {
    switch (s.toLowerCase()) {
      case 'added':
        return 'ok'
      case 'removed':
        return 'bad'
      case 'renamed':
        return 'warn'
      default:
        return 'muted'
    }
  }
</script>

{#if error}
  <div class="state err">
    {error}
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else if loading && !pull}
  <div class="state muted">Loading…</div>
{:else if pull}
  <div class="page">
    <header class="head">
      <div class="title-row">
        <span class="num mono">#{pull.number}</span>
        <span class="badge {stateTone(pull.merged ? 'merged' : pull.state)}">
          {pull.merged ? 'merged' : pull.state}
        </span>
        {#if pull.draft}<span class="badge muted">draft</span>{/if}
        <h2 class="title">{pull.title}</h2>
      </div>
      <div class="meta">
        <span class="mono">{pull.head} → {pull.base}</span>
        <span>by {pull.author || '—'}</span>
        {#if pull.created}<span>{pull.created}</span>{/if}
        {#if pull.url}
          <a href={pull.url} target="_blank" rel="noopener noreferrer" class="ext">On GitHub ↗</a>
        {/if}
      </div>
      {#if checks}
        <div class="meta">
          <span class="badge {stateTone(checks.state)}">CI {checks.state}</span>
          {#if checks.sha}<span class="mono muted">{checks.sha.slice(0, 7)}</span>{/if}
        </div>
      {/if}
    </header>

    <nav class="tabs">
      <button type="button" class:active={tab === 'overview'} onclick={() => (tab = 'overview')}>
        Overview
      </button>
      <button type="button" class:active={tab === 'files'} onclick={() => (tab = 'files')}>
        Files <span class="count">{files.length}</span>
      </button>
    </nav>

    {#if tab === 'overview'}
      <div class="body">
        {#if pull.body.trim()}
          <pre class="body-text">{pull.body}</pre>
        {:else}
          <div class="muted">No description.</div>
        {/if}
        {#if checks?.checks?.length}
          <h3 class="sec-title">Checks</h3>
          <ul class="rows">
            {#each checks.checks as c, i (c.name + i)}
              <li class="row">
                <span class="badge {stateTone(c.status)}">{c.status || '—'}</span>
                <span class="grow ellipsis">{c.name}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    {:else}
      <div class="body">
        <div class="muted totals">
          {files.length} file(s), <span class="adds">+{totals.adds}</span>
          <span class="dels">-{totals.dels}</span>
        </div>
        {#each files as f (f.path)}
          <div class="file">
            <button
              type="button"
              class="file-head"
              onclick={() => (openFile = openFile === f.path ? '' : f.path)}
            >
              <span class="badge {fileTone(f.status)}">{f.status}</span>
              <span class="mono grow ellipsis">{f.path}</span>
              <span class="adds">+{f.additions}</span>
              <span class="dels">-{f.deletions}</span>
            </button>
            {#if openFile === f.path}
              {#if f.patch_truncated}
                <!-- Saying so matters: an empty patch means GitHub did not send one (binary or too
                     large), not that the file did not change. -->
                <div class="muted pad">GitHub did not provide this patch (binary or too large).</div>
              {:else if f.patch}
                <pre class="diff">{f.patch}</pre>
              {:else}
                <div class="muted pad">No patch.</div>
              {/if}
            {/if}
          </div>
        {:else}
          <div class="muted">This pull request changes nothing.</div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .page {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
  }
  .head {
    padding: var(--sp-md);
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: var(--sp-xs, 4px);
    /* The same reader's measure as the issue page: a title and a branch line stretched across a wide
       window are harder to scan than the same text in a column. */
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
    /* A FLEX CHILD SHRINKS BY DEFAULT. Inside a column with a scrolling list below, a header that
       does not opt out of shrinking gets squashed as the list takes the space — its text clips
       rather than the layout scrolling. */
    flex: 0 0 auto;
  }
  .title-row {
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    min-width: 0;
  }
  .num {
    color: var(--muted-foreground);
    flex: 0 0 auto;
  }
  .title {
    margin: 0;
    font-size: 1.05rem;
    font-weight: 600;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-md);
    font-size: var(--text-micro, 11px);
    color: var(--muted-foreground);
  }
  .ext {
    color: var(--primary);
    text-decoration: none;
  }
  .ext:hover {
    text-decoration: underline;
  }
  .tabs {
    display: flex;
    gap: var(--sp-xs, 4px);
    padding: var(--sp-sm) var(--sp-md);
    border-bottom: 1px solid var(--border);
  }
  .tabs button {
    font: inherit;
    font-size: var(--text-meta, 12px);
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    border: 0;
    background: none;
    color: var(--muted-foreground);
    cursor: pointer;
  }
  .tabs button.active {
    background: var(--primary);
    color: var(--primary-foreground, #fff);
  }
  .count {
    opacity: 0.75;
    margin-left: 2px;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--sp-md);
    display: flex;
    flex-direction: column;
    gap: var(--sp-sm);
    /* A diff is the one thing here that wants width, so this cap is generous — it exists to stop a
       single short line from being stretched across a 4K monitor, not to narrow the code. */
    max-width: 100rem;
    width: 100%;
    margin: 0 auto;
  }
  .body-text {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: inherit;
    font-size: var(--text-meta, 12px);
    line-height: 1.55;
  }
  .sec-title {
    margin: var(--sp-sm) 0 0;
    font-size: var(--text-micro, 10px);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--muted-foreground);
  }
  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    padding: 2px 0;
  }
  .file {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .file-head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    width: 100%;
    padding: var(--sp-sm);
    border: 0;
    background: var(--card);
    color: inherit;
    font: inherit;
    font-size: var(--text-meta, 12px);
    text-align: left;
    cursor: pointer;
  }
  .file-head:hover {
    background: var(--muted);
  }
  .diff {
    margin: 0;
    padding: var(--sp-sm);
    max-height: 360px;
    overflow: auto;
    font-family: 'NotoSansMono', ui-monospace, monospace;
    font-size: 11px;
    line-height: 1.45;
    white-space: pre;
    background: var(--background);
  }
  .badge {
    flex: 0 0 auto;
    font-size: var(--text-micro, 10px);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .badge.ok { color: var(--success, #1a7f37); }
  .badge.bad { color: var(--destructive); }
  .badge.warn { color: var(--warning, #bf8700); }
  .badge.merged { color: var(--primary); }
  .badge.muted { color: var(--muted-foreground); }
  .grow { flex: 1 1 auto; min-width: 0; }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .adds { color: var(--success, #1a7f37); flex: 0 0 auto; }
  .dels { color: var(--destructive); flex: 0 0 auto; }
  .muted { color: var(--muted-foreground); }
  .totals { font-size: var(--text-micro, 11px); }
  .pad { padding: var(--sp-sm); }
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }
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
    padding: 4px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
    color: var(--foreground);
    cursor: pointer;
  }

  /**
   * NARROW SCREENS.
   *
   * The page is inside the host's side panel, which on a phone is the whole screen — so the width is
   * the phone's, not a desktop's. Two things need changing, and both are about the difference
   * between a mouse and a thumb:
   *
   *   the tab buttons grow to a 40px target, because a 4px gap between two 20px targets is a mis-tap
   *   waiting to happen;
   *   the header tightens, because a phone spends a sixth of its height on it before any content.
   */
  @media (max-width: 720px) {
    .head {
      padding: var(--sp-sm);
    }
    .tabs {
      padding: var(--sp-xs, 4px) var(--sp-sm);
    }
    .tabs button {
      padding: 10px 12px;
    }
    .retry {
      padding: 10px 16px;
    }
  }
</style>
