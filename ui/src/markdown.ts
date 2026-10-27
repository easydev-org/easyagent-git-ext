// Markdown -> HTML for the file viewer.
//
// SANITIZED, not trusted. The source is a file in somebody's repository: a README can contain
// `<img onerror=...>`, and this document shares an origin with the host app (the host proxies
// /ext/<id>). Unsanitized markdown here would be a cross-site scripting hole in the HOST, not just
// in this panel — the iframe is same-origin, so its scripts can reach the host's storage and its
// bearer token.
//
// marked is configured to keep the raw HTML it parses (many READMEs use <details>/<summary>), and
// DOMPurify then removes everything that can execute.
import DOMPurify from 'dompurify'
import { marked } from 'marked'

marked.setOptions({
  gfm: true,
  breaks: false,
})

export function renderMarkdown(src: string): string {
  const html = marked.parse(src, { async: false })
  return DOMPurify.sanitize(html, {
    // Links may point at another origin — that is normal for a README — but they must not be able
    // to run script or navigate the top frame.
    ADD_ATTR: ['target', 'rel'],
    // `target=_blank` is added below rather than allowed from the source, so the source cannot pick
    // a window name.
    FORBID_TAGS: ['style', 'form', 'input', 'button'],
    FORBID_ATTR: ['srcset'],
  })
}

/**
 * Open links in a new tab instead of INSIDE the iframe.
 *
 * Without this a README's link navigates the panel to the target page, and the user loses the file
 * viewer with no way back — the panel's own back button goes to the previous ROUTE, not the previous
 * document. `noopener` matters because the target is often a third-party page.
 */
export function decorateLinks(root: HTMLElement): void {
  for (const a of root.querySelectorAll('a[href]')) {
    a.setAttribute('target', '_blank')
    a.setAttribute('rel', 'noopener noreferrer')
  }
}
