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

const HEIGHT_KEY = 'easyagent:card-height'

/**
 * Report this document's height to the host, so a card frame is exactly as tall as its content.
 *
 * Only the HOST can size a frame it owns, and only this document knows how tall the content is — so
 * the number has to travel. A ResizeObserver rather than a one-off measurement because a card
 * renders in stages (data arrives, then an image decodes), and a height reported once would clip
 * whatever came after.
 *
 * Called only on the `card` route: the same UI is also mounted as a full page, where the host is not
 * sizing anything and a stray message would be noise.
 */
export function reportCardHeight(): void {
  if (typeof ResizeObserver === 'undefined') return
  let last = 0
  const send = (): void => {
    const h = Math.ceil(document.body.getBoundingClientRect().height)
    // Only on change: the observer fires for width too, and re-sending an identical height would
    // make the host re-render for nothing.
    if (h === last || h <= 0) return
    last = h
    window.parent?.postMessage({ type: HEIGHT_KEY, height: h }, '*')
  }
  new ResizeObserver(send).observe(document.body)
  send()
}

const OPEN_VIEW_KEY = 'easyagent:open-view'

const CARD_READY_KEY = 'easyagent:card-ready'
const CARD_DATA_KEY = 'easyagent:card-data'

/** What the host hands a card: the call's three fields, and nothing else. */
export interface CardPayload {
  tool: string
  input: Record<string, unknown>
  data: Record<string, unknown>
  output: string
  error: string
}

/**
 * Ask the host for this card's data, and call `apply` when it arrives.
 *
 * A card is a pure render, so it has nothing to fetch and no scope to resolve: the host already holds
 * the tool result and hands it over. This is why a card still works after the workspace it describes
 * has expired — it was never reading the workspace.
 *
 * Returns a function that stops listening.
 */
export function receiveCardData(apply: (p: CardPayload) => void): () => void {
  const onMessage = (ev: MessageEvent): void => {
    const d = ev.data as { type?: string } & Partial<CardPayload> | null
    if (d?.type !== CARD_DATA_KEY) return
    apply({
      tool: typeof d.tool === 'string' ? d.tool : '',
      input: (d.input as Record<string, unknown>) ?? {},
      data: (d.data as Record<string, unknown>) ?? {},
      output: typeof d.output === 'string' ? d.output : '',
      error: typeof d.error === 'string' ? d.error : '',
    })
  }
  window.addEventListener('message', onMessage)
  // Announce AFTER the listener is registered. The other order races: the host can answer before
  // this side is listening, and the card would sit empty forever.
  window.parent?.postMessage({ type: CARD_READY_KEY }, '*')
  return () => window.removeEventListener('message', onMessage)
}

/**
 * Ask the HOST to open one of this extension's views.
 *
 * A card cannot navigate by itself: it is rendered inside a frame a few hundred pixels tall, so an
 * `<a href="#/ws/…">` would open the full browser INSIDE that frame — the reader would be looking at
 * a squeezed file tree and would have to collapse the card to escape. Only the host knows where a
 * view belongs (the side panel on a wide screen, a full page on a phone).
 *
 * This is why the tool result declares `card` and `view` separately: the card knows which route is
 * worth opening, the host decides how to present it.
 */
export function requestOpenView(route: string, title: string): void {
  const parent = window.parent
  if (!parent || parent === window) return
  parent.postMessage(
    { type: OPEN_VIEW_KEY, ext: extID(), route, title },
    '*',
  )
}

/**
 * This extension's UI id, taken from the mount path.
 *
 * Derived rather than hardcoded so a rename of the Service or the mount cannot leave the card asking
 * the host to open `/ext/<old-name>`. `/ext/gitdev/?…` -> `github`.
 */
function extID(): string {
  const m = /^\/ext\/([a-z0-9-]+)/.exec(location.pathname)
  return m ? m[1]! : ''
}
