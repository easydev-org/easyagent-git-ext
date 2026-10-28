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

  const s = (k: string): string =>
    typeof payload.data[k] === 'string' ? (payload.data[k] as string) : ''
  const inputStr = (k: string): string =>
    typeof payload.input[k] === 'string' ? (payload.input[k] as string) : ''

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
  {:else if payload.tool === 'git-ws-clone'}
    <section class="sec">
      <h3 class="sec-title">Input</h3>
      <dl class="kv">
        <dt>repo</dt>
        <dd class="mono">{inputStr('repo') || s('repo') || '—'}</dd>
        {#if inputStr('ref')}<dt>ref</dt><dd class="mono">{inputStr('ref')}</dd>{/if}
      </dl>
    </section>
    <section class="sec">
      <h3 class="sec-title">Result</h3>
      <dl class="kv">
        <dt>workspace</dt>
        <dd class="mono">{s('workspace') || '—'}</dd>
        <dt>files</dt>
        <dd>{s('files') || (typeof payload.data['files'] === 'number' ? String(payload.data['files']) : '0')}</dd>
      </dl>
    </section>

  {:else if payload.tool === 'git-ws-read'}
    <section class="sec">
      <h3 class="sec-title">File</h3>
      <dl class="kv">
        <dt>path</dt>
        <dd class="mono">{s('path') || inputStr('path') || '—'}</dd>
        {#if typeof payload.data['bytes'] === 'number'}
          <dt>size</dt>
          <dd>{payload.data['bytes']} B</dd>
        {/if}
      </dl>
    </section>
    <!-- The RESULT text is the file, line-numbered. It is what the tool returned, so it is what the
         card shows; truncating it here would hide the answer the caller was given. -->
    <pre class="pre scroll">{payload.output}</pre>

  {:else if payload.tool === 'git-ws-status' || payload.tool === 'gh-pr-files'}
    <section class="sec">
      <h3 class="sec-title">
        {payload.tool === 'gh-pr-files' ? 'Changed files' : 'Status'}
      </h3>
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
        <div class="muted totals">
          {fileRows.length} file(s){#if payload.tool === 'gh-pr-files'}, +{totals.adds} -{totals.dels}{/if}
        </div>
      {/if}
    </section>

  {:else if payload.tool === 'git-ws-diff' || payload.tool === 'gh-pr-diff'}
    <section class="sec">
      <h3 class="sec-title">Diff</h3>
      {#if payload.data['truncated'] === true}
        <div class="muted">(truncated — see the tool output for the rest)</div>
      {/if}
      <pre class="pre scroll">{payload.output}</pre>
    </section>

  {:else if payload.tool === 'git-ws-log'}
    <section class="sec">
      <h3 class="sec-title">History</h3>
      {#if logRows.length === 0}
        <div class="muted">no commits</div>
      {:else}
        <ul class="rows">
          {#each logRows as c, i (c.sha + i)}
            <li class="row">
              <span class="mono shac">{c.sha}</span>
              <span class="grow ellipsis">{c.message}</span>
              <span class="muted nowrap">{c.author}</span>
            </li>
          {/each}
        </ul>
      {/if}
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
</style>
