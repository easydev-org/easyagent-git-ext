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

const BASE = '/api/v1/ui/'

function qs(extra: Record<string, string | undefined>): string {
  const p = new URLSearchParams(scope)
  for (const [k, v] of Object.entries(extra)) {
    if (v !== undefined && v !== '') p.set(k, v)
  }
  return p.toString()
}

async function get<T>(path: string, params: Record<string, string | undefined> = {}): Promise<T> {
  const r = await fetch(`${BASE}${path}?${qs(params)}`, { headers: { accept: 'application/json' } })
  // The API always answers JSON, including errors, so a parse failure means the request never
  // reached the handler (a proxy or base-path mistake) — say so rather than showing "{}".
  const body = (await r.json().catch(() => null)) as { error?: string } | null
  if (!r.ok) throw new Error(body?.error ?? `HTTP ${r.status} ${r.statusText}`)
  return body as T
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
