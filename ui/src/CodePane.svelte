<script lang="ts">
  // One file, rendered the way its TYPE wants: Markdown as a document, everything else as
  // highlighted source. A README opened as raw text with `#` and `|` pipes is technically the file,
  // and useless.
  import { highlight, langOf } from './highlight'
  import { decorateLinks, renderMarkdown } from './markdown'
  import { theme } from './theme.svelte'

  let {
    file,
    onback = null,
  }: {
    file: { path: string; size: number; binary?: boolean; content?: string } | null
    // Present on a narrow screen, where the file list and the file are separate pages.
    onback?: (() => void) | null
  } = $props()

  let html = $state<string | null>(null)
  let mdRoot = $state<HTMLElement | null>(null)
  let rendering = $state(false)

  const lang = $derived(file ? langOf(file.path) : '')
  const isMarkdown = $derived(lang === 'markdown')

  // Re-render on every change of file, theme or kind. `theme.mode` is a dependency on purpose:
  // Shiki bakes colours into the HTML, so a theme switch has to re-render rather than restyle.
  $effect(() => {
    const f = file
    const dark = theme.mode === 'dark'
    html = null
    if (!f || f.binary || f.content == null) return

    if (isMarkdown) return // handled by the markup below, not by Shiki

    let stale = false
    rendering = true
    void highlight(f.content, lang, dark).then(out => {
      // Guard against a slow render for a file the reader has already left: without this the
      // PREVIOUS file's highlight can land after the new one and overwrite it.
      if (!stale) {
        html = out
        rendering = false
      }
    })
    return () => {
      stale = true
    }
  })

  // Markdown links must open outside the panel, and only after the HTML is in the DOM.
  $effect(() => {
    if (mdRoot && file && isMarkdown && file.content != null) decorateLinks(mdRoot)
  })
</script>

{#if !file}
  <div class="state muted">Select a file.</div>
{:else}
  <div class="head">
    {#if onback}
      <button class="back" onclick={onback} aria-label="back to files">←</button>
    {/if}
    <span class="mono path">{file.path}</span>
    <span class="muted t-meta">{file.size} B</span>
  </div>

  {#if file.binary}
    <!-- A repository holds images and archives; rendering their bytes as text would be megabytes of
         replacement characters and no information. -->
    <div class="state muted">Binary file — not shown.</div>
  {:else if file.content == null}
    <div class="state muted">No content.</div>
  {:else if isMarkdown}
    <!-- `{@html}` of SANITIZED html: see markdown.ts. The sanitizer is what makes this safe, not the
         source being a repository file. -->
    <div class="md" bind:this={mdRoot}>{@html renderMarkdown(file.content)}</div>
  {:else if html}
    <div class="shiki">{@html html}</div>
  {:else}
    <!-- While highlighting loads, and for languages Shiki does not have: the plain file. Showing it
         immediately means the content is readable before the grammar arrives. -->
    <pre class="mono code">{file.content}</pre>
    {#if rendering}<div class="t-micro muted hint">highlighting…</div>{/if}
  {/if}
{/if}

<style>
  .head {
    display: flex;
    gap: var(--sp-sm);
    align-items: center;
    padding-bottom: var(--sp-sm);
    min-width: 0;
  }
  .path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }
  .back {
    font: inherit;
    color: var(--primary);
    background: none;
    border: 0;
    cursor: pointer;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
  }
  .back:hover {
    background: var(--secondary);
  }
  .code {
    margin: 0;
    padding: var(--sp-md);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: auto;
    font-size: 12px;
    line-height: 1.45;
    tab-size: 4;
  }
  .hint {
    padding-top: 4px;
  }
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }

  /* Shiki emits its own <pre class="shiki"> with inline colours; these rules only fit it into the
     panel's own frame. */
  .shiki :global(pre.shiki) {
    margin: 0;
    padding: var(--sp-md);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: auto;
    font-size: 12px;
    line-height: 1.45;
  }

  /* Markdown prose, sized for a panel rather than a page. */
  .md {
    line-height: 1.6;
    word-wrap: break-word;
  }
  .md :global(h1),
  .md :global(h2),
  .md :global(h3) {
    margin: 1.2em 0 0.5em;
    line-height: 1.25;
    font-weight: 600;
  }
  .md :global(h1) { font-size: 1.5em; }
  .md :global(h2) { font-size: 1.25em; }
  .md :global(h3) { font-size: 1.1em; }
  .md :global(> :first-child) { margin-top: 0; }
  .md :global(p),
  .md :global(ul),
  .md :global(ol),
  .md :global(blockquote),
  .md :global(table) {
    margin: 0 0 0.8em;
  }
  .md :global(ul),
  .md :global(ol) {
    padding-left: 1.4em;
  }
  .md :global(a) {
    color: var(--primary);
  }
  .md :global(code) {
    font-family: 'NotoSansMono', ui-monospace, monospace;
    font-size: 0.92em;
    background: var(--secondary);
    padding: 0.1em 0.35em;
    border-radius: 4px;
  }
  .md :global(pre) {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-md);
    overflow: auto;
  }
  .md :global(pre code) {
    background: none;
    padding: 0;
  }
  .md :global(blockquote) {
    border-left: 3px solid var(--border);
    padding-left: var(--sp-md);
    color: var(--muted-foreground);
  }
  .md :global(table) {
    border-collapse: collapse;
    display: block;
    overflow: auto;
  }
  .md :global(th),
  .md :global(td) {
    border: 1px solid var(--border);
    padding: 4px 10px;
  }
  .md :global(img) {
    max-width: 100%;
  }
  .md :global(hr) {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 1.2em 0;
  }
</style>
