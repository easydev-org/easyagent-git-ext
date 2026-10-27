// Client for this extension's own JSON API.
//
// Every call reads the LOCAL workspace. Nothing here reaches api.github.com: the checkout is
// already on disk, and it is the only source that includes this session's own edits.

export interface Workspace {
  id: string
  repo: string
  branch: string
}

export interface TreeEntry {
  path: string
  dir?: boolean
}

export interface FileBody {
  path: string
  size: number
  binary?: boolean
  content?: string
}

export interface Change {
  Path: string
  Status: string
  Staged?: boolean
}

export interface LogEntry {
  Hash?: string
  Message?: string
  Author?: string
  When?: string
}

/** The (tenant, session) scope that owns the workspace, taken from the host's query string. */
const scope = new URLSearchParams(location.search)

function qs(extra: Record<string, string | undefined>): string {
  const p = new URLSearchParams(scope)
  for (const [k, v] of Object.entries(extra)) {
    if (v !== undefined && v !== '') p.set(k, v)
  }
  return p.toString()
}

// Requests go through the HOST, not directly.
//
// The credential for this data is the host's bearer token, which lives in the host's JavaScript and
// must not be handed to an iframe: anything in this document would hold it, and any XSS here would
// then hold a long-lived credential. So this side asks and the host performs the fetch —
// see easyagent-webui/src/lib/ext-bridge.ts.
//
// It also means the tenant does not have to be trusted from a query parameter: the host's token
// carries it and the server reads it from there.
const HOST = window.parent ?? window
const BRIDGE_REQUEST = 'easyagent:ext-fetch'
const BRIDGE_RESPONSE = 'easyagent:ext-fetch:result'

let seq = 0
const pending = new Map<string, (r: { ok: boolean; status: number; body?: unknown; error?: string }) => void>()

window.addEventListener('message', (ev: MessageEvent) => {
  const d = ev.data as { type?: string; id?: string; ok?: boolean; status?: number; body?: unknown; error?: string } | null
  if (d?.type !== BRIDGE_RESPONSE || typeof d.id !== 'string') return
  const done = pending.get(d.id)
  if (!done) return
  pending.delete(d.id)
  done({ ok: Boolean(d.ok), status: d.status ?? 0, body: d.body, error: d.error })
})

function ask(path: string): Promise<{ ok: boolean; status: number; body?: unknown; error?: string }> {
  return new Promise(resolve => {
    const id = `${Date.now()}-${++seq}`
    // A host that never answers would hang the panel forever with no clue; five seconds is far more
    // than a local file read needs.
    const timer = setTimeout(() => {
      pending.delete(id)
      resolve({ ok: false, status: 0, error: 'the host did not answer' })
    }, 5000)
    pending.set(id, r => {
      clearTimeout(timer)
      resolve(r)
    })
    HOST.postMessage({ type: BRIDGE_REQUEST, id, path }, '*')
  })
}

async function get<T>(path: string, params: Record<string, string | undefined> = {}): Promise<T> {
  const r = await ask(`${path}?${qs(params)}`)
  if (!r.ok) {
    const body = r.body as { error?: string } | string | undefined
    const msg = typeof body === 'string' ? body : body?.error
    throw new Error(msg ?? r.error ?? `HTTP ${r.status}`)
  }
  return r.body as T
}

export const api = {
  workspaces: () => get<{ workspaces: Workspace[] }>('workspaces').then(r => r.workspaces),
  tree: (ws: string, dir: string) =>
    get<{ entries: TreeEntry[] }>('tree', { ws, dir }).then(r => r.entries),
  file: (ws: string, path: string) => get<FileBody>('file', { ws, path }),
  status: (ws: string) =>
    get<{ branch: string; changes: Change[] }>('status', { ws }),
  diff: (ws: string, path?: string) =>
    get<{ diff: string }>('diff', { ws, path }).then(r => r.diff),
  log: (ws: string) => get<{ entries: LogEntry[] }>('log', { ws, limit: '80' }).then(r => r.entries),
}
