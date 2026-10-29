<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type FileBody, type TreeEntry } from './api'
  import {
    announceReady,
    receiveCardData,
    reportCardHeight,
    requestOpenView,
    type CardPayload,
  } from './theme.svelte'
  import ToolCard from './ToolCard.svelte'
  import PullPage from './PullPage.svelte'
  import IssuePage from './IssuePage.svelte'
  import RunsPage from './RunsPage.svelte'
  import RunPage from './RunPage.svelte'
  import ItemsPage from './ItemsPage.svelte'
  import NavBar from './NavBar.svelte'
  import CodePane from './CodePane.svelte'
  import Tree from './Tree.svelte'

  // Routing is hash-based: the host deep-links, and the server needs no route for it — which is why
  // the UI can be plain embedded files.
  //
  //   #/card/<template>            a finished tool call, rendered from data the host hands over
  //   #/pull/<owner>/<name>/<n>    a pull request on the upstream
  //   #/files/<owner>/<name>       the MIRROR's files, optionally with ?branch=&dir=
  type Route =
    | { kind: 'root' }
    // A tool CARD: a drawing of one result, from data the host hands over. It fetches nothing, which
    // is why it still works long after the call finished.
    | { kind: 'card'; template: string }
    // A pull request as a PAGE. It reads GitHub: a PR is not in the mirror, and the point of opening
    // the page is to see it NOW rather than as it was when a tool ran.
    | { kind: 'pull'; repo: string; number: number }
    // An ISSUE as a page. Same reasoning, and the same route shape: GitHub numbers both in one
    // sequence, so one page can answer for either and says which it is.
    | { kind: 'issue'; repo: string; number: number }
    // CI. `runs` is the repository's list; `run` is one run's jobs and logs. Two routes because they
    // are two levels: you arrive at the list, then open one.
    | { kind: 'runs'; repo: string }
    | { kind: 'run'; repo: string; id: number }
    // The two LISTS: a tab bar is useless if every tab leads to a page that needs a number.
    | { kind: 'pulls'; repo: string }
    | { kind: 'issues'; repo: string }
    | { kind: 'files'; repo: string; branch: string; dir: string }

  function parse(hash: string): Route {
    // THE QUERY IS SPLIT OFF FIRST. Splitting the whole hash on "/" left it glued to the last
    // segment — `e2e?branch=main` — and every route built from that segment then carried the old
    // query into the new URL. The panel still worked, so the damage only showed as a growing hash:
    // `?branch=a&dir=?branch=b&dir=`.
    const raw = hash.replace(/^#/, '')
    const qi = raw.indexOf('?')
    const q = new URLSearchParams(qi >= 0 ? raw.slice(qi + 1) : '')
    const parts = (qi >= 0 ? raw.slice(0, qi) : raw).split('/').filter(Boolean)
    if (parts[0] === 'card' && parts[1]) {
      return { kind: 'card', template: decodeURIComponent(parts[1]) }
    }
    // The repository is TWO path segments, not one escaped segment: `owner/name` is a path, and
    // escaping the slash would make it a single opaque token nobody reads that way.
    if (parts[0] === 'pull' && parts[1] && parts[2] && parts[3]) {
      const n = Number(parts[3])
      if (Number.isFinite(n) && n > 0) {
        return { kind: 'pull', repo: `${parts[1]}/${parts[2]}`, number: n }
      }
    }
    if (parts[0] === 'issue' && parts[1] && parts[2] && parts[3]) {
      const n = Number(parts[3])
      if (Number.isFinite(n) && n > 0) {
        return { kind: 'issue', repo: `${parts[1]}/${parts[2]}`, number: n }
      }
    }
    if (parts[0] === 'runs' && parts[1] && parts[2]) {
      return { kind: 'runs', repo: `${parts[1]}/${parts[2]}` }
    }
    if (parts[0] === 'pulls' && parts[1] && parts[2]) {
      return { kind: 'pulls', repo: `${parts[1]}/${parts[2]}` }
    }
    if (parts[0] === 'issues' && parts[1] && parts[2]) {
      return { kind: 'issues', repo: `${parts[1]}/${parts[2]}` }
    }
    if (parts[0] === 'run' && parts[1] && parts[2] && parts[3]) {
      const n = Number(parts[3])
      if (Number.isFinite(n) && n > 0) {
        return { kind: 'run', repo: `${parts[1]}/${parts[2]}`, id: n }
      }
    }
    if (parts[0] === 'files' && parts[1] && parts[2]) {
      return {
        kind: 'files',
        repo: `${parts[1]}/${parts[2]}`,
        // The branch and directory ride in the query rather than the path: a branch name contains
        // slashes (`feat/x`), which a path segment cannot carry without changing what the route
        // means.
        branch: q.get('branch') ?? '',
        dir: q.get('dir') ?? '',
      }
    }
    return { kind: 'root' }
  }

  let route = $state<Route>(parse(location.hash))

  /**
   * The repository this route is about, and which tab it belongs to.
   *
   * Derived from the route rather than passed down by each page: a page that forgot to say which
   * tab it is would leave the nav bar highlighting nothing, and every page would have to know about
   * a bar it does not own.
   *
   * A CARD HAS NEITHER. It is one tool's result embedded in a conversation — a nav bar there would
   * offer to leave a place the reader is not in.
   */
  const navFor = $derived.by(() => {
    switch (route.kind) {
      case 'files':
        return { repo: route.repo, active: 'files' }
      case 'pull':
        return { repo: route.repo, active: 'pulls' }
      case 'pulls':
        return { repo: route.repo, active: 'pulls' }
      case 'issue':
        return { repo: route.repo, active: 'issues' }
      case 'issues':
        return { repo: route.repo, active: 'issues' }
      case 'run':
        return { repo: route.repo, active: 'runs' }
      case 'runs':
        return { repo: route.repo, active: 'runs' }
      default:
        return { repo: '', active: '' }
    }
  })

  let entries = $state<TreeEntry[]>([])
  let branches = $state<string[]>([])
  let branch = $state('')
  let file = $state<FileBody | null>(null)
  let error = $state('')
  let loading = $state(false)
  let narrow = $state(false)
  let cardPayload = $state<CardPayload | null>(null)
  // Where a card offers to go, when the tool that produced it named a view.
  let openTarget = $state<{ route: string; label: string; title: string } | null>(null)

  function go(hash: string): void {
    location.hash = hash
  }

  /** The path of the file currently open, or '' — a nested path keeps its slashes. */
  let openPath = $state('')

  async function load(r: Route): Promise<void> {
    error = ''
    loading = true
    try {
      if (r.kind === 'files') {
        const info = await api.repoInfo(r.repo)
        branches = info.branches ?? []
        // The branch defaults to the repository's own default, not to "main": not every repository
        // uses that name, and guessing would open an empty page with no explanation.
        branch = r.branch || info.defaultBranch || branches[0] || ''
        if (openPath !== '') {
          file = await api.file(r.repo, branch, openPath)
        } else {
          entries = await api.tree(r.repo, branch, r.dir)
          file = null
        }
      } else {
        // A card renders what the host handed over; a pull request page fetches for itself. Neither
        // needs anything from here.
        entries = []
        file = null
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  onMount(() => {
    const onHash = () => {
      const next = parse(location.hash)
      // Opening a file from the tree keeps the branch; leaving the page clears it.
      if (next.kind !== 'files') openPath = ''
      route = next
      void load(next)
    }
    onHash()
    window.addEventListener('hashchange', onHash)

    const mq = matchMedia('(max-width: 720px)')
    narrow = mq.matches
    mq.addEventListener('change', (e) => {
      narrow = (e as MediaQueryListEvent).matches
    })

    announceReady()
    return () => window.removeEventListener('hashchange', onHash)
  })

  // The card frame is sized by the HOST, using a height only this document knows.
  $effect(() => {
    void cardPayload
    void route
    reportCardHeight()
  })

  receiveCardData((p) => {
    cardPayload = p
    error = ''
    // A tool may say where its result lives. The route is the tool's to build — only it knows which
    // repository the result was about.
    const v = (p as { view?: { route?: string; title?: string } }).view
    openTarget = v?.route
      ? { route: v.route, label: 'Open', title: v.title ?? 'Open' }
      : null
  })

  /** Open one file, keeping the branch and the directory behind it. */
  function openFile(path: string): void {
    openPath = path
    if (route.kind === 'files') void load(route)
  }

  function backToTree(): void {
    openPath = ''
    if (route.kind === 'files') void load(route)
  }

  /** Move to a directory, keeping the branch. Observed via the hash so the URL stays shareable. */
  function openDir(dir: string): void {
    openPath = ''
    if (route.kind !== 'files') return
    go(
      `#/files/${route.repo}?branch=${encodeURIComponent(branch)}&dir=${encodeURIComponent(dir)}`,
    )
  }

  function pickBranch(b: string): void {
    branch = b
    openPath = ''
    if (route.kind === 'files') {
      go(`#/files/${route.repo}?branch=${encodeURIComponent(b)}&dir=${encodeURIComponent(route.dir)}`)
    }
  }
</script>

{#if error}
  <div class="state error">{error}</div>
{:else if route.kind === 'card'}
  <!-- A tool card: the render of one finished call. Everything shown comes from the message the host
       handed over, so there is nothing to wait for and nothing that can have expired. -->
  {#if cardPayload}
    <ToolCard payload={cardPayload} />
    {#if openTarget}
      <div class="card-action">
        <button type="button" class="open" onclick={() => requestOpenView(openTarget!.route, openTarget!.title)}>
          {openTarget.label} →
        </button>
      </div>
    {/if}
  {:else}
    <div class="state muted">waiting for data…</div>
  {/if}
{:else}
  <!-- Every OTHER page shares one frame: a sticky nav bar above, the page below it.

       WHY HERE AND NOT IN EACH PAGE: the bar is the same everywhere and is derived from the route,
       so putting it in each page would be seven copies to keep in step — and a page that forgot it
       would be a room with no door. -->
  <div class="framed">
    <NavBar repo={navFor.repo} active={navFor.active} />
    <div class="framed-body">
      {#if route.kind === 'pull'}
        <PullPage repo={route.repo} number={route.number} />
      {:else if route.kind === 'issue'}
        <IssuePage repo={route.repo} number={route.number} />
      {:else if route.kind === 'pulls'}
        <ItemsPage repo={route.repo} kind="pr" />
      {:else if route.kind === 'issues'}
        <ItemsPage repo={route.repo} kind="issue" />
      {:else if route.kind === 'runs'}
        <RunsPage repo={route.repo} />
      {:else if route.kind === 'run'}
        <RunPage repo={route.repo} id={route.id} />
      {:else if route.kind === 'files'}
        <div class="layout">
          <!-- NARROW: one page at a time, and opening a file replaces the tree. WIDE: the tree is a
               rail and the content fills the rest, which is how this reads best in a side panel. -->
          {#if !narrow || openPath === ''}
            <aside>
              {#if branches.length > 1}
                <select class="branch" value={branch} onchange={(e) => pickBranch(e.currentTarget.value)}>
                  {#each branches as b (b)}
                    <option value={b}>{b}</option>
                  {/each}
                </select>
              {/if}
              <Tree
                {entries}
                dir={route.dir}
                ws={route.repo}
                activePath={openPath}
                onopen={openFile}
                ondir={openDir}
              />
            </aside>
          {/if}
          {#if !(narrow && openPath === '')}
            <main>
              {#if loading}
                <div class="state muted">Loading…</div>
              {:else}
                <CodePane {file} onback={narrow && openPath !== '' ? backToTree : null} />
              {/if}
            </main>
          {/if}
        </div>
      {:else}
        <div class="state">
          Nothing selected.
          <div class="muted t-meta">
            Open a repository with <span class="mono">#/files/&lt;owner&gt;/&lt;name&gt;</span>, or run a
            tool — its result opens here.
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  /* The frame every non-card page shares: a sticky bar, then the page filling what is left.
     `min-height: 0` on the body is what lets a page inside it scroll instead of growing — without it
     the flex child refuses to shrink below its content and the bar scrolls away with the page. */
  .framed {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .framed-body {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .framed-body > :global(*) {
    flex: 1 1 auto;
    min-height: 0;
  }

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
  }
  main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }
  /* The action sits after the card's own sections: a reader arrives here after reading what the call
     asked for and what it returned, which is the point at which going somewhere makes sense. */
  .card-action {
    padding: 0 var(--sp-md) var(--sp-sm);
  }
  .card-action .open {
    color: var(--primary);
    font-size: var(--text-meta, 12px);
    text-decoration: none;
    /* A real <button> for the click, styled as the link it reads as. The UA's chrome has to go. */
    background: none;
    border: 0;
    padding: 0;
    font-family: inherit;
    cursor: pointer;
  }
  .card-action .open:hover {
    text-decoration: underline;
  }
  .error {
    color: var(--destructive);
    white-space: pre-wrap;
  }
</style>
