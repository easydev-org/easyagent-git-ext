<script lang="ts">
  let {
    file,
  }: {
    file: { path: string; size: number; binary?: boolean; content?: string } | null
  } = $props()
</script>

{#if !file}
  <div class="state muted">Select a file.</div>
{:else}
  <div class="head t-meta muted">
    <span class="mono">{file.path}</span>
    <span>· {file.size} bytes</span>
  </div>
  {#if file.binary}
    <!-- A repository holds images and archives; rendering their bytes as text would be megabytes of
         replacement characters and no information. -->
    <div class="state muted">Binary file — not shown.</div>
  {:else}
    <pre class="mono code">{file.content ?? ''}</pre>
  {/if}
{/if}

<style>
  .head {
    display: flex;
    gap: var(--sp-sm);
    padding-bottom: var(--sp-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  .state {
    padding: var(--sp-xl);
    text-align: center;
  }
</style>
