<script lang="ts">
  // One issue: its title, its body and its conversation.
  //
  // IT REFETCHES, like the pull-request page and unlike a card. A page is somewhere the reader goes
  // to see the state NOW; somebody may have commented since the tool ran, and repeating a snapshot
  // would be a page that is quietly out of date.
  import { onMount } from 'svelte'
  import { api, type IssueView, type CommentView } from './api'
  import { renderMarkdown } from './markdown'

  let { repo, number }: { repo: string; number: number } = $props()

  let issue = $state<IssueView | null>(null)
  let comments = $state<CommentView[]>([])
  let error = $state('')
  let loading = $state(true)

  async function load(): Promise<void> {
    loading = true
    error = ''
    try {
      const [i, c] = await Promise.all([
        api.issue(repo, number),
        // The conversation is optional: its failure must not hide the issue, which is the thing the
        // reader asked for.
        api.issueComments(repo, number).catch(() => [] as CommentView[]),
      ])
      issue = i
      comments = c
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  onMount(() => {
    void load()
  })

  /** Colour a state the way GitHub does, so "open" reads as open before it is read. */
  function stateTone(state: string): string {
    return state.toLowerCase() === 'open' ? 'open' : 'closed'
  }
</script>

{#if error}
  <div class="state err">
    <div>{error}</div>
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else if loading || !issue}
  <div class="state muted">Loading…</div>
{:else}
  <div class="page">
    <header class="head">
      <div class="title-row">
        <span class="num mono">#{issue.number}</span>
        <span class="badge {stateTone(issue.state)}">{issue.state}</span>
        {#if issue.isPullRequest}
          <!-- GitHub numbers issues and pull requests in one sequence, so this page can be reached
               for either. Saying so lets the reader switch to the page that has the code. -->
          <a class="badge link" href={`#/pull/${repo}/${issue.number}`}>pull request →</a>
        {/if}
        <h2 class="title">{issue.title}</h2>
      </div>
      <div class="meta">
        {#if issue.author}<span>by {issue.author}</span>{/if}
        {#if issue.labels?.length}
          <span class="labels">
            {#each issue.labels as l (l)}<span class="label">{l}</span>{/each}
          </span>
        {/if}
        {#if issue.url}
          <a class="ext" href={issue.url} target="_blank" rel="noreferrer">open on GitHub ↗</a>
        {/if}
      </div>
    </header>

    <div class="body-scroll">
      {#if issue.body}
        <article class="md">{@html renderMarkdown(issue.body)}</article>
      {:else}
        <div class="muted">(no description)</div>
      {/if}

      {#if comments.length}
        <h3 class="sec-title">{comments.length} comment{comments.length === 1 ? '' : 's'}</h3>
        <ul class="comments">
          {#each comments as c (c.id)}
            <li class="comment">
              <span class="avatar" aria-hidden="true">{(c.author || '?').slice(0, 1)}</span>
              <div class="comment-head">
                <span class="who">{c.author || 'someone'}</span>
                {#if c.when}<span class="muted t-micro">{c.when}</span>{/if}
              </div>
              <div class="md comment-body">{@html renderMarkdown(c.body)}</div>
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
  .head {
    padding: var(--sp-md);
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: var(--sp-xs, 4px);
    /* A header is a label for the thread below it, so it shares the thread's measure rather than
       spanning a wide window. */
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .title-row {
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    flex-wrap: wrap;
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
  .badge {
    flex: 0 0 auto;
    font-size: var(--text-micro, 10px);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    /* A pill, not bare letters: an unadorned word in colour reads as a typo, and a shape reads as a
       status at a glance. */
    padding: 1px 8px;
    border-radius: 999px;
    border: 1px solid currentColor;
    line-height: 1.6;
  }
  .badge.open { color: var(--success, #1a7f37); }
  .badge.closed { color: var(--destructive, #cf222e); }
  .badge.link {
    text-transform: none;
    color: var(--primary);
    text-decoration: none;
    border-color: var(--border);
  }
  .badge.link:hover { text-decoration: underline; }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-md);
    font-size: var(--text-micro, 11px);
    color: var(--muted-foreground);
    align-items: center;
  }
  .labels { display: flex; gap: 4px; flex-wrap: wrap; }
  .label {
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--secondary);
    color: var(--secondary-foreground, var(--foreground));
  }
  .ext { color: var(--primary); text-decoration: none; }
  .ext:hover { text-decoration: underline; }

  /* The body scrolls, the header does not: the title and state stay visible while reading a long
     thread, which is the whole reason to open the page instead of a card. */
  .body-scroll {
    overflow: auto;
    padding: var(--sp-md);
    display: flex;
    flex-direction: column;
    gap: var(--sp-md);
    min-height: 0;
    /* THE READER'S MEASURE, not the window's. A one-line comment stretched across a 2400px monitor
       is unreadable — the eye loses the line on the way back — and the fix is a cap, not smaller
       text. 68rem is roughly 90 characters at this size. */
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .sec-title {
    margin: var(--sp-sm) 0 0;
    font-size: var(--text-meta, 12px);
    font-weight: 600;
    color: var(--muted-foreground);
  }
  .comments {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-sm);
  }
  .comment {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-sm);
    background: var(--card);
    /* Room for the avatar column, so the text of every comment starts at the same x. */
    display: grid;
    grid-template-columns: 28px 1fr;
    column-gap: var(--sp-sm);
  }
  .comment-head {
    display: flex;
    gap: var(--sp-sm);
    align-items: baseline;
    margin-bottom: 4px;
    grid-column: 2;
  }
  /* A placeholder mark, not a picture: fetching every commenter's avatar costs a request per
     comment on a page that may show fifty of them, and the initial carries the same information —
     WHO — that the avatar would. */
  .avatar {
    grid-row: 1 / span 2;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: var(--secondary);
    color: var(--muted-foreground);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--text-micro, 10px);
    font-weight: 600;
    text-transform: uppercase;
    user-select: none;
  }
  .comment-body { grid-column: 2; min-width: 0; }
  .who { font-weight: 600; font-size: var(--text-meta, 12px); }
  .md { overflow-wrap: anywhere; }
  .md :global(p) { margin: 0 0 var(--sp-sm); }
  .md :global(p:last-child) { margin-bottom: 0; }
  .md :global(pre) {
    overflow: auto;
    padding: var(--sp-sm);
    border-radius: var(--radius-sm);
    background: var(--card);
  }
  .md :global(code) { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .md :global(a) { color: var(--primary); }

  .state {
    padding: var(--sp-lg);
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
