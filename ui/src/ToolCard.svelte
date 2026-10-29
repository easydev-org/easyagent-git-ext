<script lang="ts">
  // Renders ONE tool call, from the data the host hands over.
  //
  // Pure: no fetches, no session, no workspace. Everything below comes from the tool's own result, so
  // a card still renders correctly after the workspace behind it has expired — and it always agrees
  // with the result text beside it, because it is reading the same thing.
  //
  // The host picks this component by the `template` the tool declared; the template name IS the tool
  // name, which keeps the mapping obvious and means a new tool needs no new concept.
  import type { CardPayload } from './theme.svelte'

  let { payload }: { payload: CardPayload } = $props()

  /** Rows of a `files` array, tolerating both the tool's PascalCase and a future camelCase. */
  const fileRows = $derived.by(() => {
    const raw = payload.data['files']
    if (!Array.isArray(raw)) return []
    return raw.map(f => {
      const o = (f ?? {}) as Record<string, unknown>
      const str = (k: string): string => (typeof o[k] === 'string' ? (o[k] as string) : '')
      const num = (k: string): number => (typeof o[k] === 'number' ? (o[k] as number) : 0)
      return {
        path: str('Path') || str('path'),
        status: str('Status') || str('status'),
        adds: num('Additions') || num('additions'),
        dels: num('Deletions') || num('deletions'),
        truncated: (o['PatchTruncated'] ?? o['patchTruncated']) === true,
      }
    })
  })

  const logRows = $derived.by(() => {
    const raw = payload.data['log']
    if (!Array.isArray(raw)) return []
    return raw.map(c => {
      const o = (c ?? {}) as Record<string, unknown>
      const str = (k: string): string => (typeof o[k] === 'string' ? (o[k] as string) : '')
      return {
        sha: (str('SHA') || str('sha')).slice(0, 7),
        message: str('Message') || str('message'),
        author: str('Author') || str('author'),
        when: str('When') || str('when'),
      }
    })
  })

  const totals = $derived.by(() => {
    const adds = typeof payload.data['additions'] === 'number' ? (payload.data['additions'] as number) : 0
    const dels = typeof payload.data['deletions'] === 'number' ? (payload.data['deletions'] as number) : 0
    return { adds, dels, count: fileRows.length }
  })

  /** The mirror's directory listing: `git-file-list` returns `entries` of {path,name,dir,size}. */
  const dirRows = $derived.by(() => {
    const raw = payload.data['entries']
    if (!Array.isArray(raw)) return []
    return raw.map(e => {
      const o = (e ?? {}) as Record<string, unknown>
      const str = (k: string): string => (typeof o[k] === 'string' ? (o[k] as string) : '')
      return {
        path: str('path') || str('Path'),
        name: str('name') || str('Name') || (str('path') || '').split('/').pop() || '',
        dir: o['dir'] === true || o['Dir'] === true,
        size: typeof o['size'] === 'number' ? (o['size'] as number) : undefined,
      }
    })
  })

  /**
   * What one patch changed: `git-file-patch` returns `changes` of {kind,path}.
   *
   * A patch is one commit, so this list is the WHOLE change — not a summary of several commits, which
   * is why the card names the commit once and lists the files under it.
   */
  const changeRows = $derived.by(() => {
    const raw = payload.data['changes']
    if (!Array.isArray(raw)) return []
    return raw.map(c => {
      const o = (c ?? {}) as Record<string, unknown>
      const str = (k: string): string => (typeof o[k] === 'string' ? (o[k] as string) : '')
      return { kind: str('kind') || str('Kind') || 'change', path: str('path') || str('Path') }
    })
  })

  const s = (k: string): string =>
    typeof payload.data[k] === 'string' ? (payload.data[k] as string) : ''
  const inputStr = (k: string): string =>
    typeof payload.input[k] === 'string' ? (payload.input[k] as string) : ''

  /** Abbreviate a sha for display; anything shorter is shown as-is rather than padded. */
  const mono7 = (sha: string): string => (sha ? sha.slice(0, 7) : '—')

  /** Status colouring, matching how the host colours additions/deletions. */
  function tone(status: string): string {
    switch (status.toLowerCase()) {
      case 'added':
        return 'text-success'
      case 'removed':
        return 'text-destructive'
      case 'renamed':
        return 'text-warning'
      default:
        return 'text-muted-foreground'
    }
  }
</script>

<div class="card">
  {#if payload.error}
    <section class="sec">
      <h3 class="sec-title">Error</h3>
      <pre class="pre err">{payload.error}</pre>
    </section>
  {:else if payload.tool === 'git-file-list'}
    <!-- The mirror's directory listing. A tree is a LIST, and a list in a paragraph is unreadable;
         the count goes first because it answers "did anything change" before the reader scans. -->
    <section class="sec">
      <h3 class="sec-title">
        {s('dir') ? s('dir') : 'Repository root'}
        <span class="muted">· {s('branch')}</span>
      </h3>
      {#if dirRows.length === 0}
        <div class="muted">empty directory</div>
      {:else}
        <ul class="rows">
          {#each dirRows as e, i (e.path + i)}
            <li class="row">
              <span class="badge {e.dir ? 'dir' : 'file'}">{e.dir ? 'dir' : 'file'}</span>
              <span class="mono grow ellipsis">{e.name}</span>
              {#if typeof e.size === 'number' && !e.dir}
                <span class="muted nowrap">{e.size} B</span>
              {/if}
            </li>
          {/each}
        </ul>
        <div class="muted totals">{dirRows.length} entr{dirRows.length === 1 ? 'y' : 'ies'}</div>
      {/if}
    </section>

  {:else if payload.tool === 'git-file-read'}
    <section class="sec">
      <h3 class="sec-title">
        <span class="mono">{s('path') || inputStr('path') || '—'}</span>
        {#if typeof payload.data['size'] === 'number'}
          <span class="muted">· {payload.data['size']} B</span>
        {/if}
      </h3>
      <!-- The file, line-numbered, exactly as the tool returned it. -->
      <pre class="pre scroll">{payload.output}</pre>
    </section>

  {:else if payload.tool === 'git-file-patch'}
    <!-- ONE patch is ONE commit; the card's job is to show that the change is a single act and to
         name the files it touched. -->
    <section class="sec">
      <h3 class="sec-title">
        Commit
        <span class="mono muted">{(s('commit') || '').slice(0, 7)}</span>
        <span class="muted">· {s('branch')}</span>
      </h3>
      {#if changeRows.length === 0}
        <div class="muted">no changes</div>
      {:else}
        <ul class="rows">
          {#each changeRows as c, i (c.path + i)}
            <li class="row">
              <span class="badge {c.kind === 'delete' ? 'del' : c.kind === 'add' ? 'add' : 'mod'}">
                {c.kind}
              </span>
              <span class="mono grow ellipsis">{c.path}</span>
            </li>
          {/each}
        </ul>
        <div class="muted totals">{changeRows.length} file(s) in one commit</div>
      {/if}
    </section>

  {:else if payload.tool === 'git-sync-upstream'}
    <section class="sec">
      <h3 class="sec-title">
        {s('branch')}
        <span class="badge {payload.data['moved'] === true ? 'add' : 'mod'}">
          {payload.data['moved'] === true ? 'fast-forwarded' : 'already current'}
        </span>
      </h3>
      <dl class="kv">
        <dt>before</dt>
        <dd class="mono">{mono7(s('before'))}</dd>
        <dt>after</dt>
        <dd class="mono">{mono7(s('after'))}</dd>
      </dl>
    </section>

  {:else if payload.tool === 'gh-pr-files'}
    <section class="sec">
      <h3 class="sec-title">Changed files</h3>
      {#if fileRows.length === 0}
        <div class="muted">clean (nothing changed)</div>
      {:else}
        <ul class="rows">
          {#each fileRows as f, i (f.path + i)}
            <li class="row">
              <span class="badge {tone(f.status)}">{f.status}</span>
              <span class="mono grow ellipsis">{f.path}</span>
              <span class="adds">+{f.adds}</span>
              <span class="dels">-{f.dels}</span>
            </li>
          {/each}
        </ul>
        <div class="muted totals">{fileRows.length} file(s), +{totals.adds} -{totals.dels}</div>
      {/if}
    </section>

  {:else if payload.tool === 'gh-pr-diff'}
    <section class="sec">
      <h3 class="sec-title">Diff</h3>
      {#if payload.data['truncated'] === true}
        <div class="muted">(truncated — see the tool output for the rest)</div>
      {/if}
      <pre class="pre scroll">{payload.output}</pre>
    </section>

  {:else if payload.tool === 'gh-pr-checks'}
    <section class="sec">
      <h3 class="sec-title">Checks</h3>
      <dl class="kv">
        <dt>state</dt>
        <dd class="mono">{s('state') || 'none'}</dd>
        {#if s('sha')}<dt>commit</dt><dd class="mono">{s('sha').slice(0, 7)}</dd>{/if}
      </dl>
    </section>
    <pre class="pre scroll">{payload.output}</pre>

  {:else if payload.tool === 'gh-issue-show'}
    <section class="sec">
      <h3 class="sec-title">Item</h3>
      <pre class="pre scroll">{payload.output}</pre>
    </section>

  {:else}
    <!-- A template this build does not know. Showing the raw text is the honest fallback: better a
         readable dump than a blank card. -->
    <pre class="pre scroll">{payload.output}</pre>
  {/if}
</div>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--sp-sm);
    padding: var(--sp-sm) var(--sp-md);
    font-size: var(--text-meta, 12px);
  }
  .sec {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }
  .sec-title {
    margin: 0;
    font-size: var(--text-micro, 10px);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--muted-foreground);
  }
  .kv {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px var(--sp-sm);
    margin: 0;
  }
  .kv dt {
    color: var(--muted-foreground);
    font-size: var(--text-micro, 10px);
  }
  .kv dd {
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pre {
    margin: 0;
    padding: var(--sp-sm);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card);
    font-family: 'NotoSansMono', ui-monospace, monospace;
    font-size: 11px;
    line-height: 1.45;
    white-space: pre-wrap;
    word-break: break-word;
  }
  /* Only the tall bodies scroll; a two-line output does not deserve its own scrollbar. */
  .pre.scroll {
    max-height: 260px;
    overflow: auto;
  }
  .pre.err {
    color: var(--destructive);
  }
  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
  }
  .row {
    display: flex;
    align-items: baseline;
    gap: var(--sp-sm);
    padding: 2px 0;
  }
  .badge {
    flex: 0 0 auto;
    font-size: var(--text-micro, 10px);
    text-transform: uppercase;
  }
  /* Semantic colouring for the mirror's rows. Colour alone never carries the meaning — the badge
     also says `dir`, `add`, `del` — so it still reads with a monochrome palette or for somebody who
     cannot distinguish the hues. */
  .badge.dir { color: var(--muted-foreground); }
  .badge.file { color: var(--muted-foreground); }
  .badge.add { color: var(--success, #1a7f37); }
  .badge.del { color: var(--destructive, #cf222e); }
  .badge.mod { color: var(--warning, #9a6700); }
  .shac {
    flex: 0 0 auto;
    color: var(--muted-foreground);
  }
  .grow { flex: 1 1 auto; min-width: 0; }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .nowrap { white-space: nowrap; }
  .adds { color: var(--success, #1a7f37); flex: 0 0 auto; }
  .dels { color: var(--destructive); flex: 0 0 auto; }
  .muted { color: var(--muted-foreground); }
  .totals { font-size: var(--text-micro, 10px); }

  /**
   * NARROW SCREENS.
   *
   * A card is embedded in the host's transcript, which on a phone is narrow. The rows are already
   * `min-width: 0` so they can shrink; what a phone needs on top is for the FILE PATH to keep the
   * room, because a truncated path with its directory gone is unreadable — the badge and the counts
   * are what get dropped instead.
   */
  @media (max-width: 720px) {
    .card {
      padding: var(--sp-xs, 4px) var(--sp-sm);
    }
    .row {
      gap: var(--sp-xs, 4px);
    }
    /* The per-file counts are the first thing to go: the totals line already carries them. */
    .row .adds,
    .row .dels {
      display: none;
    }
    /* Directory-relative names read better than full paths at this width, but the TITLE attribute
       still has the full path for a long press. */
    .shac {
      display: none;
    }
  }
</style>
