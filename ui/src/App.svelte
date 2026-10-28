<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type Change, type LogEntry, type TreeEntry, type Workspace } from './api'
  import { announceReady, reportCardHeight } from './theme.svelte'
  import CodePane from './CodePane.svelte'
  import DiffPane from './DiffPane.svelte'
  import Tree from './Tree.svelte'

  // Routing is hash-based: the host deep-links with `#/ws/<id>/blob/<path>`, and the server needs no
  // route for it — which is why the UI can be plain embedded files.
  type Route =
    | { kind: 'root' }
    // A compact summary of one tool CALL, for the host's tool card. Separate from the workspace
    // routes above: it answers "what did this call do", not "show me this repository", and it is
    // read-only — nothing here navigates, because the frame it renders is a few hundred pixels tall.
    | { kind: 'card'; ws: string }
    | { kind: 'tree'; ws: string; dir: string }
    | { kind: 'blob'; ws: string; path: string }
    | { kind: 'changes'; ws: string }
    | { kind: 'log'; ws: string }

  function parse(hash: string): Route {
    const parts = hash.replace(/^#/, '').split('/').filter(Boolean)
    // `#/card/<workspace>` — the call's own summary.
    if (parts[0] === 'card' && parts[1]) return { kind: 'card', ws: parts[1] }
    if (parts[0] !== 'ws' || !parts[1]) return { kind: 'root' }
    const ws = parts[1]
    if (parts[2] === 'blob' && parts[3]) return { kind: 'blob', ws, path: parts.slice(3).join('/') }
    if (parts[2] === 'changes') return { kind: 'changes', ws }
    if (parts[2] === 'log') return { kind: 'log', ws }
    return { kind: 'tree', ws, dir: parts.slice(3).join('/') }
  }

  let route = $state<Route>(parse(location.hash))
  let workspaces = $state<Workspace[]>([])
  // State for the `card` route only; unused elsewhere, which is cheaper than a discriminated state
  // object for two fields.
  let cardWs = $state<Workspace | null>(null)
  let cardBranch = $state('')
  let entries = $state<TreeEntry[]>([])
  let changes = $state<Change[]>([])
  let branch = $state('')
  let commits = $state<LogEntry[]>([])
  let file = $state<{ path: string; size: number; binary?: boolean; content?: string } | null>(null)
  let diff = $state('')
  let error = $state('')
  let loading = $state(false)
  let tab = $state<'code' | 'changes' | 'log'>('code')

  // Narrow = the panel is a phone-sized column, where a 240px tree beside a file leaves neither
  // enough room. On a narrow screen the tree and the file are SEPARATE PAGES: picking a file shows
  // it full width with a back arrow, which is what a phone file browser does.
  //
  // 720px rather than the host's 640px because this UI also runs inside a SIDE PANEL on a desktop,
  // where the available width is the panel's, not the window's.
  let narrow = $state(false)
  onMount(() => {
    const mq = window.matchMedia('(max-width: 720px)')
    const on = (): void => {
      narrow = mq.matches
    }
    on()
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  })

  const wsID = $derived(route.kind === 'root' ? '' : route.ws)

  function go(hash: string): void {
    location.hash = hash
  }

  // Load is keyed on the ROUTE, not on individual clicks: a deep link, a back button and a click all
  // take the same path, so there is one place where "what should be on screen" is decided.
  $effect(() => {
    const r = route
    void load(r)
  })

  // A card frame is sized by the HOST, using a height only this document knows. Started once per
  // mount, and only on the card route — on the other routes this UI is a full page and the host is
  // sizing nothing.
  let heightReporting = false
  $effect(() => {
    if (route.kind !== 'card' || heightReporting) return
    heightReporting = true
    reportCardHeight()
  })

  async function load(r: Route): Promise<void> {
    error = ''
    loading = true
    try {
      if (r.kind === 'root') {
        workspaces = await api.workspaces()
        // One repository is the common case: go straight to it rather than making the user click
        // through a list of one.
        if (workspaces.length === 1) {
          go(`#/ws/${workspaces[0]!.id}/tree/`)
          return
        }
        return
      }
      if (r.kind === 'tree') {
        tab = 'code'
        entries = await api.tree(r.ws, r.dir)
        file = null
      } else if (r.kind === 'card') {
        // A card shows the same three facts the workspace picker does, plus the branch. One request,
        // because a card that needs several round trips is a card that renders late inside a frame
        // the host has already sized.
        const list = await api.workspaces()
        cardWs = list.find(w => w.id === r.ws) ?? null
        if (cardWs) {
          cardBranch = await api.status(r.ws).then(s => s.branch).catch(() => cardWs!.branch)
        }
      } else if (r.kind === 'blob') {
        tab = 'code'
        file = await api.file(r.ws, r.path)
      } else if (r.kind === 'changes') {
        tab = 'changes'
        const s = await api.status(r.ws)
        changes = s.changes ?? []
        branch = s.branch
        diff = await api.diff(r.ws)
      } else if (r.kind === 'log') {
        tab = 'log'
        commits = await api.log(r.ws)
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  onMount(() => {
    announceReady()
    const onHash = (): void => {
      route = parse(location.hash)
    }
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  })
</script>

{#if error}
  <div class="state error">{error}</div>
{:else if route.kind === 'card'}
  <!-- The compact summary an agent tool card embeds.

       Deliberately NOT the full browser: it is rendered in a frame a few hundred pixels tall, so it
       says what this workspace IS and offers the one action worth taking from here. Everything else
       belongs in the panel the "Open" button already provides. -->
  <div class="card-body">
    {#if !cardWs}
      <div class="state muted">This workspace is gone — it may have expired.</div>
    {:else}
      <div class="card-head">
        <span class="repo">{cardWs.repo || cardWs.id}</span>
        {#if cardBranch}<span class="pill mono">{cardBranch}</span>{/if}
      </div>
      <dl class="kv">
        <dt>workspace</dt>
        <dd class="mono">{cardWs.id}</dd>
        <dt>branch</dt>
        <dd class="mono">{cardBranch || cardWs.branch || '—'}</dd>
      </dl>
      <a class="open" href={`#/ws/${cardWs.id}/tree/`}>Browse files →</a>
    {/if}
  </div>
{:else if route.kind === 'root'}
  {#if workspaces.length === 0}
    <div class="state">
      No workspace in this session.
      <div class="muted t-meta">
        Ask the agent to run <span class="mono">git-ws-clone</span> — it returns the id this panel
        opens.
      </div>
      <!-- Workspaces live in memory, so a restart clears them. An old tool card still LOOKS
           clickable after that, so say it here rather than leaving the reader to guess why a
           workspace they used a minute ago is gone. -->
      <div class="muted t-meta">
        An empty list can also mean the service restarted: workspaces are kept in memory and do not
        survive it. Cloning again is the fix, not a retry.
      </div>
    </div>
  {:else}
    <div class="picker">
      {#each workspaces as w (w.id)}
        <button class="pick" onclick={() => go(`#/ws/${w.id}/tree/`)}>
          <span class="mono t-meta muted">{w.branch || 'repo'}</span>
          <span class="name">{w.repo || w.id}</span>
        </button>
      {/each}
    </div>
  {/if}
{:else}
  <div class="layout">
    <!-- NARROW: one page at a time. The tree is the first page; opening a file replaces it and the
         back arrow returns. Showing both side by side on a phone leaves ~120px for each.
         WIDE: the tree is a fixed rail and the content fills the rest, which is how this reads
         best inside the host's side panel. -->
    <!-- On a narrow screen the tree belongs to the TREE PAGE only. Changes and History are their own
         pages with their own tabs; leaving the rail visible there halves the width for content that
         does not need a file list. -->
    {#if !narrow || route.kind === 'tree'}
      <aside>
        <Tree
          {entries}
          dir={route.kind === 'tree' ? route.dir : ''}
          ws={wsID}
          activePath={route.kind === 'blob' ? route.path : ''}
          {go}
        />
      </aside>
    {/if}

    <!-- The tree page's own content would be an empty "Select a file." panel beside the rail, so it
         is not rendered on a narrow screen — the tree IS that page. -->
    {#if !(narrow && route.kind === 'tree')}
    <main>
      <nav class="tabs">
        <button class:active={tab === 'code'} onclick={() => go(`#/ws/${wsID}/tree/`)}>Code</button>
        <button class:active={tab === 'changes'} onclick={() => go(`#/ws/${wsID}/changes`)}>
          Changes{#if changes.length}<span class="pill">{changes.length}</span>{/if}
        </button>
        <button class:active={tab === 'log'} onclick={() => go(`#/ws/${wsID}/log`)}>History</button>
      </nav>

      <div class="body">
        {#if loading}
          <div class="state muted">Loading…</div>
        {:else if tab === 'code'}
          <!-- On a narrow screen the file is its own page, so it needs a way back to the list. -->
          <CodePane {file} onback={narrow && route.kind === 'blob' ? () => go(`#/ws/${wsID}/tree/`) : null} />
        {:else if tab === 'changes'}
          <DiffPane {changes} {branch} {diff} {go} ws={wsID} />
        {:else}
          <div class="log">
            {#each commits as c (c.Hash)}
              <div class="commit">
                <span class="mono t-meta muted">{(c.Hash ?? '').slice(0, 7)}</span>
                <span class="msg">{(c.Message ?? '').split('\n')[0]}</span>
              </div>
            {:else}
              <div class="state muted">No commits.</div>
            {/each}
          </div>
        {/if}
      </div>
    </main>
    {/if}
  </div>
{/if}

<style>
  .layout {
    display: flex;
    height: 100%;
    min-height: 0;
  }
  aside {
    width: 240px;
    flex: 0 0 auto;
    border-right: 1px solid var(--border);
    overflow: auto;
    padding: var(--sp-sm);
  }
  /* Narrow: the tree IS the page, so it takes the whole area and needs no divider. */
  @media (max-width: 720px) {
    aside {
      width: 100%;
      flex: 1 1 auto;
      border-right: 0;
    }
    /* The tabs are a 44px-tall target on a touch screen; a 4px gap between them is a mis-tap
       waiting to happen. */
    .tabs button {
      padding: 8px 12px;
    }
  }
  main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .tabs {
    display: flex;
    gap: var(--sp-xs);
    padding: var(--sp-sm);
    border-bottom: 1px solid var(--border);
    /* The row must be reachable while a file is scrolled long: sticky keeps Changes one tap away
       instead of a scroll to the top. */
    position: sticky;
    top: 0;
    background: var(--background);
    z-index: 1;
  }
  .tabs button {
    font: inherit;
    color: inherit;
    background: none;
    border: 0;
    cursor: pointer;
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .tabs button:hover {
    background: var(--secondary);
  }
  .tabs button.active {
    background: var(--secondary);
    color: var(--primary);
    font-weight: 600;
  }
  .pill {
    background: var(--primary);
    color: var(--primary-foreground);
    border-radius: 999px;
    padding: 0 6px;
    font-size: 10px;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--sp-md);
  }
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }
  /* The card is embedded in the host's tool card: it must read as a summary, not as a page. Tighter
     spacing and no page-level padding, because the frame is already inset by the host. */
  .card-body {
    padding: var(--sp-sm) var(--sp-md);
    display: flex;
    flex-direction: column;
    gap: var(--sp-sm);
  }
  .card-head {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    min-width: 0;
  }
  .card-head .repo {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kv {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px var(--sp-sm);
    margin: 0;
  }
  .kv dt {
    color: var(--muted-foreground);
    font-size: var(--text-micro, 11px);
  }
  .kv dd {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .card-body .open {
    align-self: flex-start;
    color: var(--primary);
    font-size: var(--text-meta, 12px);
    text-decoration: none;
  }
  .card-body .open:hover {
    text-decoration: underline;
  }
  .error {
    color: var(--destructive);
    white-space: pre-wrap;
  }
  .picker {
    padding: var(--sp-sm);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .pick {
    font: inherit;
    color: inherit;
    background: none;
    border: 0;
    cursor: pointer;
    display: flex;
    gap: var(--sp-sm);
    align-items: center;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    text-align: left;
  }
  .pick:hover {
    background: var(--secondary);
  }
  .log {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .commit {
    display: flex;
    gap: var(--sp-sm);
    padding: 4px 8px;
    border-radius: var(--radius-sm);
  }
  .commit:hover {
    background: var(--secondary);
  }
  .msg {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
