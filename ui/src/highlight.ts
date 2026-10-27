// Syntax highlighting, lazily.
//
// Shiki ships a grammar per language and they are not small (some are hundreds of KB), so the
// entry bundle must not contain them: the History/Changes panes rarely need highlighting, and a
// panel that takes 3 seconds to open because it preloaded 40 grammars is worse than one that
// highlights a moment later.
//
// So: the engine is loaded on first use, grammars are loaded per language, and an UNKNOWN language
// falls back to plain text instead of failing. A file in a language we do not have must still be
// readable — that is the whole point of a file viewer.

/** Extension -> Shiki language id. Shiki's own resolver handles most, this maps the odd ones. */
const LANG_ALIASES: Record<string, string> = {
  ts: 'typescript',
  tsx: 'tsx',
  js: 'javascript',
  jsx: 'jsx',
  mjs: 'javascript',
  cjs: 'javascript',
  yml: 'yaml',
  sh: 'bash',
  zsh: 'bash',
  kt: 'kotlin',
  rs: 'rust',
  py: 'python',
  md: 'markdown',
  dockerfile: 'docker',
  makefile: 'make',
  hcl: 'hcl',
  toml: 'toml',
  proto: 'proto',
}

/** Languages we are willing to load. Anything else renders as plain text. */
const KNOWN = new Set([
  'bash', 'c', 'cpp', 'csharp', 'css', 'diff', 'docker', 'go', 'graphql', 'hcl', 'html', 'ini',
  'java', 'javascript', 'json', 'jsx', 'kotlin', 'less', 'lua', 'make', 'markdown', 'nginx',
  'php', 'proto', 'python', 'ruby', 'rust', 'scss', 'shellscript', 'sql', 'svelte', 'swift',
  'toml', 'tsx', 'typescript', 'vue', 'xml', 'yaml',
])

let engine: Promise<import('shiki').Highlighter> | null = null

async function highlighter(): Promise<import('shiki').Highlighter> {
  if (!engine) {
    // github-light / github-dark match the host's two palettes more closely than any other pair,
    // and using the SAME pair for every file keeps the panel visually coherent.
    engine = import('shiki').then(m =>
      m.createHighlighter({ themes: ['github-light', 'github-dark'], langs: [] }),
    )
  }
  return engine
}

/** Detect a Shiki language from a path, or '' when there is no confident answer. */
export function langOf(path: string): string {
  const base = path.split('/').pop() ?? ''
  const dot = base.lastIndexOf('.')
  const ext = dot > 0 ? base.slice(dot + 1).toLowerCase() : base.toLowerCase()
  const lang = LANG_ALIASES[ext] ?? ext
  return KNOWN.has(lang) ? lang : ''
}

/**
 * Render `code` as themed HTML, or return null when highlighting is unavailable.
 *
 * Null (not an exception, not escaped plain text) so the caller can fall back to its own <pre>:
 * a viewer that degrades to plain text is fine, one that shows a stack trace is not.
 */
export async function highlight(
  code: string,
  lang: string,
  dark: boolean,
): Promise<string | null> {
  if (lang === '') return null
  try {
    const h = await highlighter()
    if (!h.getLoadedLanguages().includes(lang)) {
      await h.loadLanguage(lang as Parameters<typeof h.loadLanguage>[0])
    }
    return h.codeToHtml(code, { lang, theme: dark ? 'github-dark' : 'github-light' })
  } catch {
    // A language id Shiki does not know, or a grammar that fails to fetch: plain text is the right
    // answer, and it is what the caller already renders.
    return null
  }
}
