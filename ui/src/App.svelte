<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type FileBody, type Ref, type TreeEntry } from './api'
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
  import HomePage from './HomePage.svelte'
  import ReleasesPage from './ReleasesPage.svelte'
  import WorkflowsPage from './WorkflowsPage.svelte'
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
    // The layer ABOVE a repository: the pages a reader reaches before one is chosen.
    | { kind: 'home' }
    | { kind: 'releases'; repo: string }
    | { kind: 'workflows'; repo: string }
    | { kind: 'issues'; repo: string }
    // The mirror, as TWO pages: a tree to navigate and a file to read.
    //
    // They used to be one page with a rail on the left, which spent 240px of a narrow side panel on a
    // listing most readers only pass through on the way to one file. Each now has the full width.
    | { kind: 'tree'; repo: string; ref: string; dir: string }
    | { kind: 'blob'; repo: string; ref: string; path: string }

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
    if (parts[0] === 'releases' && parts[1] && parts[2]) {
      return { kind: 'releases', repo: `${parts[1]}/${parts[2]}` }
    }
    if (parts[0] === 'workflows' && parts[1] && parts[2]) {
      return { kind: 'workflows', repo: `${parts[1]}/${parts[2]}` }
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
      // KEPT AS AN ALIAS for the shape a tool emitted before the split, so an old link lands on the
      // tree instead of on the home page.
      return {
        kind: 'tree',
        repo: `${parts[1]}/${parts[2]}`,
        ref: q.get('branch') ?? q.get('ref') ?? '',
        dir: q.get('dir') ?? '',
      }
    }
    // THE TREE: which rep, at which ref, in which directory. The ref and directory ride in the QUERY
    // rather than the path because a branch name contains slashes (`feat/x`), and a path segment
    // cannot carry one without changing what the route means.
    if (parts[0] === 'tree' && parts[1] && parts[2]) {
      return {
        kind: 'tree',
        repo: `${parts[1]}/${parts[2]}`,
        ref: q.get('ref') ?? '',
        dir: q.get('dir') ?? '',
      }
    }
    // THE FILE: the path IS the rest of the route, because that is what it is.
    if (parts[0] === 'blob' && parts[1] && parts[2] && parts[3]) {
      return {
        kind: 'blob',
        repo: `${parts[1]}/${parts[2]}`,
        ref: q.get('ref') ?? '',
        path: parts.slice(3).join('/'),
      }
    }
    return { kind: 'home' }
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
      case 'tree':
        return { repo: route.repo, active: 'files' }
      case 'blob':
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
      case 'releases':
        return { repo: route.repo, active: 'releases' }
      case 'workflows':
        return { repo: route.repo, active: 'workflows' }
      default:
        return { repo: '', active: '' }
    }
  })

  let entries = $state<TreeEntry[]>([])
  /** Every ref of the current repository, branches and tags, for the switcher. */
  let refs = $state<Ref[]>([])
  /** The ref being browsed: a branch name or a tag name. */
  let ref = $state('')
  let file = $state<FileBody | null>(null)
  let error = $state('')
  let loading = $state(false)
  let cardPayload = $state<CardPayload | null>(null)
  // Where a card offers to go, when the tool that produced it named a view.
  let openTarget = $state<{ route: string; label: string; title: string } | null>(null)

  function go(hash: string): void {
    location.hash = hash
  }

  async function load(r: Route): Promise<void> {
    error = ''
    loading = true
    try {
      if (r.kind === 'tree' || r.kind === 'blob') {
        // The refs are loaded for BOTH pages: the file page keeps the switcher, so a reader can
        // change branch without going back to the tree first.
        const all = await api.refs(r.repo)
        refs = all
        // The ref defaults to the repository's own, not to "main": not every repository uses that
        // name, and guessing would open nothing with no explanation.
        const firstBranch = all.find(x => x.kind === 'branch')?.name ?? ''
        ref = r.ref || firstBranch
        if (r.kind === 'blob') {
          file = await api.file(r.repo, ref, r.path)
          entries = []
        } else {
          entries = await api.tree(r.repo, ref, r.dir)
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
      route = next
      void load(next)
    }
    onHash()
    window.addEventListener('hashchange', onHash)

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

  /**
   * Navigate the MIRROR by rewriting the hash, never by mutating state directly.
   *
   * One path for every move — open a directory, open a file, change ref — because each is a PLACE:
   * the URL must say where the reader is, and a state mutation that skipped it would give a page
   * that cannot be linked, refreshed, or reached with the back button.
   */
  function goTree(repo: string, refName: string, dir: string): void {
    go('#/tree/' + repo + '?ref=' + encodeURIComponent(refName) + '&dir=' + encodeURIComponent(dir))
  }

  function openDir(dir: string): void {
    if (route.kind !== 'tree') return
    goTree(route.repo, ref, dir)
  }

  function openFile(path: string): void {
    if (route.kind !== 'tree') return
    // The ref travels with the file: opening `src/x.go` on `feat/x` must not silently read `main`.
    go('#/blob/' + route.repo + '/' + path + '?ref=' + encodeURIComponent(ref))
  }

  function pickRef(name: string): void {
    // Keep the reader where they are: switching ref on a file page stays on that file, and on the
    // tree page stays in that directory.
    if (route.kind === 'tree') {
      goTree(route.repo, name, route.dir)
    } else if (route.kind === 'blob') {
      go('#/blob/' + route.repo + '/' + route.path + '?ref=' + encodeURIComponent(name))
    }
  }

</script>

{#if route.kind === 'card'}
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
      {#if error}
        <!-- THE ERROR STAYS INSIDE THE FRAME. It used to replace the whole page, which took the nav
             bar with it: a reader whose repository did not load was left on a line of text with no
             way back to the list they came from. A failure of the CONTENT must not remove the
             navigation. -->
        <div class="state error">{error}</div>
      {:else if route.kind === 'pull'}
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
      {:else if route.kind === 'releases'}
        <ReleasesPage repo={route.repo} />
      {:else if route.kind === 'workflows'}
        <WorkflowsPage repo={route.repo} />
      {:else if route.kind === 'tree'}
        <!-- THE TREE PAGE. Full width, and nothing else: the rail that used to share this page with
             the file spent 240px on a listing a reader passes through on the way to one file. -->
        <div class="page-pad">
          <div class="refbar">
            <label class="reflabel" for="refsel">ref</label>
            <select id="refsel" class="refsel" value={ref} onchange={(e) => pickRef(e.currentTarget.value)}>
              {#if refs.length === 0}
                <option value="">(no branches)</option>
              {/if}
              {#if refs.some(r => r.kind === 'branch')}
                <optgroup label="branches">
                  {#each refs.filter(r => r.kind === 'branch') as r (r.name)}
                    <option value={r.name}>{r.name}</option>
                  {/each}
                </optgroup>
              {/if}
              {#if refs.some(r => r.kind === 'tag')}
                <optgroup label="tags">
                  {#each refs.filter(r => r.kind === 'tag') as r (r.name)}
                    <option value={r.name}>{r.name}</option>
                  {/each}
                </optgroup>
              {/if}
            </select>
            {#if refs.length === 0 && !loading}
              <span class="muted t-micro">this repository has no commits</span>
            {/if}
          </div>
          {#if loading}
            <div class="state muted">Loading…</div>
          {:else if entries.length === 0}
            <div class="state muted">
              {ref ? 'Empty directory.' : 'Nothing to show: this repository has no commits.'}
            </div>
          {:else}
            <Tree
              {entries}
              dir={route.dir}
              ws={route.repo}
              activePath=""
              onopen={openFile}
              ondir={openDir}
            />
          {/if}
        </div>
      {:else if route.kind === 'blob'}
        <!-- THE FILE PAGE. Also full width, with the ref switcher kept so a reader can compare the
             same file on two branches without going back to the tree. -->
        <div class="page-pad">
          <div class="refbar">
            <a class="backlink" href={ref && route.repo ? `#/tree/${route.repo}?ref=${encodeURIComponent(ref)}&dir=${encodeURIComponent(route.path.split('/').slice(0, -1).join('/'))}` : `#/tree/${route.repo}`}>
              ← {route.repo}
            </a>
            <select class="refsel" value={ref} onchange={(e) => pickRef(e.currentTarget.value)}>
              {#each refs as r (r.name)}
                <option value={r.name}>{r.name}</option>
              {/each}
            </select>
          </div>
          {#if loading}
            <div class="state muted">Loading…</div>
          {:else}
            <CodePane {file} onback={null} />
          {/if}
        </div>
      {:else}
        <!-- The root route IS the home page: a reader who opens the panel has to start somewhere,
             and before this they started on an instruction with nothing to click. -->
        <HomePage />
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

  /* The mirror's two pages: a ref bar, then the content, both in the reader's column. */
  .page-pad {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
    padding: var(--sp-sm) var(--sp-md) var(--sp-md);
    max-width: 68rem;
    width: 100%;
    margin: 0 auto;
  }
  .refbar {
    display: flex;
    align-items: center;
    gap: var(--sp-sm);
    padding-bottom: var(--sp-sm);
    flex: 0 0 auto;
    min-width: 0;
  }
  .reflabel {
    font-size: var(--text-micro, 11px);
    color: var(--muted-foreground);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  /* The switcher is a select, not a menu: it is one control with N options and a browser's own
     keyboard behaviour, which a custom dropdown would have to re-earn. */
  .refsel {
    font: inherit;
    font-size: var(--text-meta, 12px);
    padding: 4px 8px;
    max-width: 20rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--background);
    color: var(--foreground);
  }
  .refsel:focus-visible { outline: 2px solid var(--primary); outline-offset: 1px; }
  .backlink {
    font-size: var(--text-meta, 12px);
    color: var(--primary);
    text-decoration: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .backlink:hover { text-decoration: underline; }

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
