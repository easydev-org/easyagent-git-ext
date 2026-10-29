<script lang="ts">
  // Where the panel starts: the repositories and organisations the token can see.
  //
  // THIS IS THE DOOR. Every other page here needs a repository to name, so without this the panel
  // opened on an instruction — "open a repository with #/files/…" — and nothing to click.
  //
  // TWO LISTS, ONE SCREEN: an organisation is reached by clicking one, and its repositories replace
  // the list below. That is one level of drill-down, which is all a panel this size wants.
  import { onMount } from 'svelte'
  import { api, type OrgSummary, type RepoSummary } from './api'

  let repos = $state<RepoSummary[]>([])
  let orgs = $state<OrgSummary[]>([])
  let login = $state('')
  let org = $state('') // '' = the caller's own repositories
  let error = $state('')
  let loading = $state(true)

  async function load(): Promise<void> {
    loading = true
    try {
      const [r, o] = await Promise.all([
        api.repos(org),
        // The org list is only needed once; re-fetching it on every drill-down would spend a request
        // for a list that does not change while the page is open.
        orgs.length ? Promise.resolve(orgs) : api.orgs().catch(() => [] as OrgSummary[]),
      ])
      repos = r
      orgs = o
      error = ''
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  onMount(() => {
    // The login is cosmetic — a heading — so its failure must not stop the list.
    void api.whoami().then(l => (login = l)).catch(() => {})
    void load()
  })

  function pick(o: string): void {
    org = o
    void load()
  }
</script>

{#if error}
  <div class="state err">
    <div>{error}</div>
    <button type="button" class="retry" onclick={() => void load()}>Retry</button>
  </div>
{:else}
  <div class="page">
    <header class="head">
      <h2 class="title">Repositories</h2>
      {#if login}<span class="muted t-meta">as {login}</span>{/if}
    </header>

    {#if orgs.length}
      <div class="orgs" role="tablist">
        <button
          type="button"
          class="org"
          class:active={org === ''}
          onclick={() => pick('')}
          role="tab"
          aria-selected={org === ''}
        >
          Mine
        </button>
        {#each orgs as o (o.login)}
          <button
            type="button"
            class="org"
            class:active={org === o.login}
            onclick={() => pick(o.login)}
            role="tab"
            aria-selected={org === o.login}
            title={o.description}
          >
            {o.login}
          </button>
        {/each}
      </div>
    {/if}

    <div class="body-scroll">
      {#if loading}
        <div class="muted">Loading…</div>
      {:else if repos.length === 0}
        <div class="muted">No repositories{org ? ` in ${org}` : ''}.</div>
      {:else}
        <ul class="repos">
          {#each repos as r (r.fullName)}
            {@const parts = r.fullName.split('/')}
            <li>
              <a class="repo" href={`#/files/${parts[0]}/${parts[1]}`}>
                <span class="grow">
                  <span class="name ellipsis">
                    <span class="muted">{parts[0]}/</span>{parts[1]}
                  </span>
                  {#if r.description}
                    <span class="sub muted ellipsis">{r.description}</span>
                  {/if}
                </span>
                {#if r.private}<span class="badge">private</span>{/if}
                {#if r.archived}<span class="badge warn">archived</span>{/if}
                {#if r.fork}<span class="badge muted">fork</span>{/if}
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

  .orgs {
    display: flex;
    gap: var(--sp-xs, 4px);
    padding: 0 var(--sp-md) var(--sp-sm);
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .orgs::-webkit-scrollbar { display: none; }
  .org {
    font: inherit;
    font-size: var(--text-meta, 12px);
    padding: 4px 12px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--card);
    color: var(--muted-foreground);
    cursor: pointer;
    white-space: nowrap;
  }
  .org:hover { background: var(--secondary); color: var(--foreground); }
  .org:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
  .org.active {
    background: var(--secondary);
    color: var(--foreground);
    font-weight: 600;
    border-color: var(--muted-foreground);
  }

  .body-scroll {
    overflow: auto;
    padding: 0 var(--sp-md) var(--sp-md);
    min-height: 0;
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .repos { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .repo {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    padding: var(--sp-sm);
    border-radius: var(--radius-sm);
    text-decoration: none;
    color: inherit;
    min-height: 48px;
  }
  .repo:hover { background: var(--secondary); }
  .repo:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
  .grow { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .name { font-size: var(--text-body, 14px); font-weight: 600; }
  .sub { font-size: var(--text-micro, 11px); }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .badge {
    flex: 0 0 auto;
    font-size: var(--text-micro, 10px);
    padding: 1px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    color: var(--muted-foreground);
  }
  .badge.warn { color: var(--warning, #9a6700); border-color: currentColor; }
  .badge.muted { opacity: 0.7; }

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
    .orgs { padding: 0 var(--sp-sm) var(--sp-sm); }
    .body-scroll { padding: 0 var(--sp-sm) var(--sp-sm); }
    .org { padding: 8px 12px; }
    .retry { padding: 10px 16px; }
  }
</style>
