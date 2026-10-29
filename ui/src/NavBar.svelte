<script lang="ts">
  // The panel's own navigation.
  //
  // WHY IT EXISTS: every page here is a deep link that a tool result opens directly, and until now
  // there was no way to move between them — a reader who arrived at a file had to edit the URL to
  // see the CI that built it. That is fine for a link and useless for a panel.
  //
  // IT IS SHOWN ONLY WHEN THERE IS A REPOSITORY TO NAVIGATE. A card is one tool's result embedded in
  // a conversation; a nav bar in it would offer to leave a place the reader is not in.
  let {
    repo,
    active,
  }: {
    /** owner/name, or '' when the current route names no repository. */
    repo: string
    /** Which tab is current: files | pulls | issues | runs. */
    active: string
  } = $props()

  const parts = $derived(repo.split('/'))
  const owner = $derived(parts[0] ?? '')
  const name = $derived(parts[1] ?? '')

  // The tabs are built from the repository so every one keeps the context. A tab that dropped it
  // would send the reader back to an empty picker.
  const tabs = $derived([
    { id: 'files', label: 'Files', href: `#/files/${repo}?branch=&dir=` },
    { id: 'pulls', label: 'Pulls', href: `#/pulls/${repo}` },
    { id: 'issues', label: 'Issues', href: `#/issues/${repo}` },
    { id: 'runs', label: 'CI', href: `#/runs/${repo}` },
  ])
</script>

{#if repo}
  <nav class="nav" aria-label="Repository">
    <a class="repo" href={`#/files/${repo}?branch=&dir=`} title={repo}>
      {#if owner}<span class="owner">{owner}/</span>{/if}<span class="name">{name}</span>
    </a>
    <div class="tabs">
      {#each tabs as t (t.id)}
        <a
          class="tab"
          class:active={t.id === active}
          href={t.href}
          aria-current={t.id === active ? 'page' : undefined}
        >
          {t.label}
        </a>
      {/each}
    </div>
  </nav>
{/if}

<style>
  .nav {
    display: flex;
    align-items: center;
    gap: var(--sp-md);
    padding: var(--sp-sm) var(--sp-md);
    border-bottom: 1px solid var(--border);
    /* Sticky: the point of navigation is that it is there when the reader is far down a log. */
    position: sticky;
    top: 0;
    z-index: 2;
    background: var(--background);
    min-height: 40px;
  }
  .repo {
    font: inherit;
    font-size: var(--text-meta, 12px);
    font-weight: 600;
    color: inherit;
    text-decoration: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 16em;
    flex: 0 1 auto;
  }
  .repo:hover { text-decoration: underline; }
  .owner { color: var(--muted-foreground); font-weight: 400; }

  .tabs {
    display: flex;
    gap: 2px;
    flex: 0 0 auto;
    /* The tabs never wrap: a second row would push the content down on every page, and a horizontal
       scroll on a narrow screen is one gesture where a wrapped row changes the layout. */
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar { display: none; }

  .tab {
    font: inherit;
    font-size: var(--text-meta, 12px);
    color: var(--muted-foreground);
    text-decoration: none;
    padding: 4px 10px;
    border-radius: var(--radius-sm);
    white-space: nowrap;
  }
  .tab:hover { background: var(--secondary); color: var(--foreground); }
  .tab:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
  /* The current tab is marked by weight AND a background, not colour alone. */
  .tab.active {
    color: var(--foreground);
    background: var(--secondary);
    font-weight: 600;
  }

  @media (max-width: 720px) {
    .nav { padding: var(--sp-xs, 4px) var(--sp-sm); gap: var(--sp-sm); }
    /* The owner is dropped before the tabs are: `easydev-org/test` on a phone leaves no room, and
       the repository NAME is what tells two of them apart. */
    .owner { display: none; }
    .tab { padding: 8px 10px; }
  }
</style>
