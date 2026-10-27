// The host's theme, mirrored into this iframe.
//
// An iframe has its own <html>, so `data-theme` on the host's document does NOT apply here. Two
// channels cover it, and both are needed:
//
//   * `?theme=` in the URL — set by the host when it builds the iframe src. index.html applies it
//     before first paint, so dark mode never flashes light.
//   * `postMessage` — for a switch that happens while the panel is open. Without it the pane would
//     keep the theme it was loaded with.

const MSG_KEY = 'easyagent:theme'

export const theme = $state<{ mode: 'light' | 'dark' }>({
  mode: (document.documentElement.dataset.theme as 'light' | 'dark') ?? 'light',
})

function apply(mode: string): void {
  if (mode !== 'light' && mode !== 'dark') return
  document.documentElement.dataset.theme = mode
  theme.mode = mode
}

window.addEventListener('message', (ev: MessageEvent) => {
  // No origin allowlist on purpose: the host is same-origin (it proxies this UI), and an unrelated
  // origin has nothing to gain from setting a colour scheme. Validating `data` is what matters —
  // anything that is not one of the two values is ignored.
  const d = ev.data as { type?: string; theme?: string } | null
  if (d?.type === MSG_KEY) apply(String(d.theme))
})

/** Tell the host we are ready, so it can push the current theme without waiting for a change. */
export function announceReady(): void {
  window.parent?.postMessage({ type: `${MSG_KEY}:ready` }, '*')
}
