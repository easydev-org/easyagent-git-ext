<script lang="ts">
  // A repository's releases.
  //
  // THE NOTES ARE SHOWN INLINE, not behind a click. A release IS its notes — what shipped and why —
  // and a list of tag names would send the reader to GitHub to find out what any of them mean.
  import { onMount } from 'svelte'
  import { api, type ReleaseSummary } from './api'
  import { renderMarkdown } from './markdown'

  let { repo }: { repo: string } = $props()

  let releases = $state<ReleaseSummary[]>([])
  let error = $state('')
  let loading = $state(true)

  async function load(): Promise<void> {
    loading = true
    try {
      releases = await api.releases(repo, 30)
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
      <h2 class="title">Releases</h2>
      <span class="muted t-meta">{releases.length}</span>
    </header>
    <div class="body-scroll">
      {#if loading}
        <div class="muted">Loading…</div>
      {:else if releases.length === 0}
        <div class="muted">No releases in this repository.</div>
      {:else}
        <ul class="releases">
          {#each releases as r (r.tag)}
            <li class="release">
              <div class="rel-head">
                <span class="tag mono">{r.tag}</span>
                {#if r.draft}<span class="badge warn">draft</span>{/if}
                {#if r.prerelease}<span class="badge muted">pre-release</span>{/if}
                {#if r.name && r.name !== r.tag}<span class="rel-name ellipsis">{r.name}</span>{/if}
                <span class="muted t-micro nowrap">{r.publishedAt?.slice(0, 10) ?? ''}</span>
                {#if r.assets > 0}
                  <span class="muted t-micro nowrap">{r.assets} asset{r.assets === 1 ? '' : 's'}</span>
                {/if}
                <a class="ext" href={r.url} target="_blank" rel="noreferrer">GitHub ↗</a>
              </div>
              {#if r.body}
                <details class="notes">
                  <summary>notes</summary>
                  <div class="md">{@html renderMarkdown(r.body)}</div>
                </details>
              {/if}
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
    /* A FLEX CHILD SHRINKS BY DEFAULT. Inside a column with a scrolling list below, a header that
       does not opt out of shrinking gets squashed as the list takes the space — its text clips
       rather than the layout scrolling. */
    flex: 0 0 auto;
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
  .releases { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sp-sm); }
  .release {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-sm);
    background: var(--card);
  }
  .rel-head { display: flex; align-items: baseline; gap: var(--sp-sm); flex-wrap: wrap; min-width: 0; }
  .tag { font-weight: 600; }
  .rel-name { color: var(--muted-foreground); min-width: 0; }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .nowrap { white-space: nowrap; }
  .ext { color: var(--primary); text-decoration: none; font-size: var(--text-micro, 11px); margin-left: auto; }
  .ext:hover { text-decoration: underline; }

  .badge {
    font-size: var(--text-micro, 10px);
    padding: 1px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
  }
  .badge.warn { color: var(--warning, #9a6700); border-color: currentColor; }
  .badge.muted { color: var(--muted-foreground); }

  .notes { margin-top: var(--sp-sm); }
  .notes summary {
    cursor: pointer;
    font-size: var(--text-micro, 11px);
    color: var(--muted-foreground);
  }
  .notes summary:hover { color: var(--foreground); }
  .md { margin-top: var(--sp-sm); overflow-wrap: anywhere; max-height: 24rem; overflow: auto; }
  .md :global(p) { margin: 0 0 var(--sp-sm); }

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
