<script lang="ts">
  // One file as SOURCE, in a box.
  //
  // NOTHING IS RENDERED, Markdown included. This is a VIEWER of what is in the repository: a README
  // shows its `#` and its pipes because those are the bytes, and a reader looking at a file wants to
  // see the file. Rendering it turns a check of "what does the commit contain" into a display of
  // "what would this look like", which is a different question and answers none of the edits.
  import { highlight, langOf } from './highlight'
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
  let rendering = $state(false)

  const lang = $derived(file ? langOf(file.path) : '')

  // Re-render on every change of file, theme or kind. `theme.mode` is a dependency on purpose:
  // Shiki bakes colours into the HTML, so a theme switch has to re-render rather than restyle.
  $effect(() => {
    const f = file
    const dark = theme.mode === 'dark'
    html = null
    if (!f || f.binary || f.content == null) return

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
  {:else}
    <!-- THE BOX. One container for every file, highlighted or not, because it is the thing that says
         "this is a file and not the page": it sets the content apart, gives it an edge to scroll
         against, and gives the reader somewhere to select from without dragging across the layout. -->
    <div class="codebox">
      {#if html}
        <!-- Shiki's output, with its own line numbers. -->
        <div class="shiki">{@html html}</div>
      {:else}
        <!-- While the grammar loads, and for languages Shiki does not have: the plain file, numbered
             here so the content does not jump when the highlight arrives. -->
        <div class="plain">
          {#each file.content.split('\n') as line, i}
            <div class="plain-line">
              <span class="plain-n">{i + 1}</span><span class="plain-t">{line}</span>
            </div>
          {/each}
        </div>
        {#if rendering}<div class="t-micro muted hint">highlighting…</div>{/if}
      {/if}
    </div>
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
  /* THE BOX. One frame for every file, highlighted or not: it is what says "this is a file, not the
     page", gives the content an edge to scroll against, and gives a reader somewhere to select from
     without dragging across the layout. */
  .codebox {
    position: relative;
    flex: 1 1 auto;
    min-height: 0;
    overflow: auto;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .hint { padding-top: 4px; }
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }

  /* Shiki emits its own <pre class="shiki"> with inline colours and, via the line-number
     transformer, a gutter of numbered spans. These rules only fit it into the box: the box owns the
     border and the scroll, so the pre must not draw a second one. */
  .shiki :global(pre.shiki) {
    margin: 0;
    padding: var(--sp-md) var(--sp-md) var(--sp-md) 0;
    background: none;
    border: 0;
    font-size: 12px;
    line-height: 1.5;
  }
  /* The gutter. The transformer emits a real `<span class="line-number">` at the start of each line,
     so the styling targets it rather than a ::before with a data attribute. Right-aligned so the
     numbers form a column, dimmed so they are findable but not read, and unselectable so a copy of
     the file does not begin with a column of digits. */
  .shiki :global(pre.shiki .line-number) {
    display: inline-block;
    width: 3.5em;
    padding-right: 1em;
    text-align: right;
    color: var(--muted-foreground);
    opacity: 0.55;
    user-select: none;
  }

  /* The unhighlighted fallback: numbered by hand so the content does not jump when Shiki lands. */
  .plain {
    padding: var(--sp-md) 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    line-height: 1.5;
    tab-size: 4;
  }
  .plain-line { display: flex; white-space: pre-wrap; overflow-wrap: anywhere; }
  .plain-n {
    flex: 0 0 auto;
    width: 3.5em;
    padding-right: 1em;
    text-align: right;
    color: var(--muted-foreground);
    opacity: 0.6;
    user-select: none;
  }
  .plain-t { flex: 1 1 auto; min-width: 0; }
</style>
