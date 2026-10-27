<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type Change, type LogEntry, type TreeEntry, type Workspace } from './api'
  import { announceReady } from './theme.svelte'
  import CodePane from './CodePane.svelte'
  import DiffPane from './DiffPane.svelte'
  import Tree from './Tree.svelte'

  // Routing is hash-based: the host deep-links with `#/ws/<id>/blob/<path>`, and the server needs no
  // route for it — which is why the UI can be plain embedded files.
  type Route =
    | { kind: 'root' }
    | { kind: 'tree'; ws: string; dir: string }
    | { kind: 'blob'; ws: string; path: string }
    | { kind: 'changes'; ws: string }
    | { kind: 'log'; ws: string }

  function parse(hash: string): Route {
    const parts = hash.replace(/^#/, '').split('/').filter(Boolean)
    if (parts[0] !== 'ws' || !parts[1]) return { kind: 'root' }
    const ws = parts[1]
    if (parts[2] === 'blob' && parts[3]) return { kind: 'blob', ws, path: parts.slice(3).join('/') }
    if (parts[2] === 'changes') return { kind: 'changes', ws }
    if (parts[2] === 'log') return { kind: 'log', ws }
    return { kind: 'tree', ws, dir: parts.slice(3).join('/') }
  }

  let route = $state<Route>(parse(location.hash))
  let workspaces = $state<Workspace[]>([])
  let entries = $state<TreeEntry[]>([])
  let changes = $state<Change[]>([])
  let branch = $state('')
  let commits = $state<LogEntry[]>([])
  let file = $state<{ path: string; size: number; binary?: boolean; content?: string } | null>(null)
  let diff = $state('')
  let error = $state('')
  let loading = $state(false)
  let tab = $state<'code' | 'changes' | 'log'>('code')

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
    <aside>
      <Tree
        {entries}
        dir={route.kind === 'tree' ? route.dir : ''}
        ws={wsID}
        activePath={route.kind === 'blob' ? route.path : ''}
        {go}
      />
    </aside>

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
          <CodePane {file} />
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
