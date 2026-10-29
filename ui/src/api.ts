// Client for this extension's own JSON API.
//
// Every call reads the LOCAL workspace. Nothing here reaches api.github.com: the checkout is
// already on disk, and it is the only source that includes this session's own edits.

export interface Workspace {
  id: string
  repo: string
  branch: string
  /**
   * Which conversation this checkout belongs to.
   *
   * Present because the list can span sessions: an extension opened from its own tab has no session
   * to narrow by, so it shows every checkout the tenant owns and the reader needs to see where each
   * one came from.
   */
  session?: string
}

export interface TreeEntry {
  path: string
  dir?: boolean
}

/** One issue on GitHub. `isPullRequest` is reported because GitHub numbers both in one sequence. */
export interface IssueView {
  number: number
  title: string
  state: string
  body: string
  author: string
  url: string
  labels: string[] | null
  isPullRequest: boolean
}

export interface CommentView {
  id: number
  author: string
  body: string
  when: string
}

/** One workflow run. `conclusion` is empty while it is still going. */
export interface RunView {
  id: number
  name: string
  status: string
  conclusion: string
  branch: string
  event: string
  headSha: string
  url: string
  createdAt: string
  attempt: number
  done: boolean
}

/** One step of a job, as GitHub reports it while the job is running. */
export interface StepView {
  name: string
  status: string
  conclusion: string
  number: number
}

export interface JobView {
  id: number
  name: string
  status: string
  conclusion: string
  startedAt: string
  url: string
  steps: StepView[] | null
  /** False while the job runs: GitHub publishes a job's log only once it has finished. */
  logAvailable: boolean
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

// Requests carry the host's bearer token, read from localStorage.
//
// WHY THIS AND NOT A postMessage BRIDGE: this UI is served from /ext/<id>/ behind the host's own
// reverse proxy, so it is SAME-ORIGIN with the host — and same-origin documents share localStorage.
// The host already keeps its token there (`agent.token`, see easyagent-webui/src/lib/prefs.ts), so
// this UI can read it directly.
//
// The bridge was the first attempt and it never worked: it needed the host to be listening, to
// recognise the frame's window, and to answer before a five-second timeout — three things that can
// each fail silently, and did. A direct read has none of them.
//
// The trust boundary is unchanged in the way that matters: the token is never put in a URL, and the
// server still resolves the tenant from the token, so a client cannot name its own tenant.
const TOKEN_KEY = 'agent.token'

// Absolute, and derived from where this document is MOUNTED.
//
// A relative `api/<path>` resolves against the current URL, and this UI is opened at
// `/ext/gitdev/#/ws/...` — the directory part is `/ext/gitdev/`, which happens to be right, but the
// same code served from `/ext/gitdev` (no trailing slash) would resolve to `/ext/api/...` and 404.
// Taking the mount point from the document URL and appending one fixed segment removes the guess.
const API_BASE = (() => {
  const p = location.pathname.replace(/\/+$/, '') // /ext/gitdev
  return `${p}/api/`
})()

function bearer(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    // A sandboxed frame without allow-same-origin throws on access. Report it as "no token" so the
    // API answers 401 with a message, rather than crashing the whole panel on load.
    return ''
  }
}

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

/**
 * Whether this document is running inside a host frame.
 *
 * When it is, requests go through the host's bridge because only the host holds the bearer.
 * When it is NOT — the UI opened directly, which is how it is debugged — there is no host to ask,
 * so requests go straight to the same origin. That keeps the UI usable standalone instead of
 * hanging for five seconds on every call with no way to see why.
 */
function ask(path: string): Promise<{ ok: boolean; status: number; body?: unknown; error?: string }> {
  // A token in storage means we can just fetch: no host involvement, no timeout to wait out.
  if (bearer() !== '') return direct(path)
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

/** Same-origin fetch, with the bearer from localStorage when there is one. */
async function direct(path: string): Promise<{ ok: boolean; status: number; body?: unknown; error?: string }> {
  try {
    const tok = bearer()
    const r = await fetch(`${API_BASE}${path}`, {
      headers: {
        accept: 'application/json',
        ...(tok ? { authorization: `Bearer ${tok}` } : {}),
      },
    })
    const text = await r.text()
    let body: unknown = text
    try {
      body = JSON.parse(text)
    } catch {
      // keep the raw text so the panel can show what actually came back
    }
    return { ok: r.ok, status: r.status, body }
  } catch (e) {
    return { ok: false, status: 0, error: e instanceof Error ? e.message : String(e) }
  }
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
  // THE MIRROR. These read the forge, not a local checkout: there is no checkout any more, and the
  // mirror is where a change is made, so it is what a reader wants to see while making one.
  tree: (repo: string, branch: string, dir: string) =>
    get<{ entries: TreeEntry[] }>('tree', { repo, branch, dir }).then(r => r.entries),
  file: (repo: string, branch: string, path: string) =>
    get<FileBody>('file', { repo, branch, path }),
  repoInfo: (repo: string) =>
    get<{ repo: string; branches: string[]; defaultBranch: string; tags: string[] }>('repo', { repo }),
  // Pull requests. These read GitHub rather than the local checkout: a pull request is not on disk,
  // and a PAGE shows the current state where a card replays what a tool already returned.
  pull: (repo: string, number: number) => get<PullView>('pull', { repo, number: String(number) }),
  pullFiles: (repo: string, number: number) =>
    get<{ files: PullFile[]; count: number }>('pull/files', { repo, number: String(number) }).then(
      r => r.files,
    ),
  pullDiff: (repo: string, number: number) =>
    get<{ diff: string }>('pull/diff', { repo, number: String(number) }).then(r => r.diff),
  pullChecks: (repo: string, number: number) =>
    get<PullChecks>('pull/checks', { repo, number: String(number) }),
  // Issues. GitHub numbers issues and pull requests in one sequence, so the same route answers for
  // both and says which it is.
  issue: (repo: string, number: number) => get<IssueView>('issue', { repo, number: String(number) }),
  issueComments: (repo: string, number: number) =>
    get<{ comments: CommentView[]; count: number }>('issue/comments', {
      repo,
      number: String(number),
    }).then(r => r.comments ?? []),
  // Issues OR pull requests. GitHub numbers both in one sequence, so it is one query with a filter —
  // and the filter is applied on the server so a caller cannot forget it.
  issues: (repo: string, kind: 'issue' | 'pr', state = 'open', limit = 50) =>
    get<{ items: IssueView[]; count: number }>('issues', {
      repo,
      kind,
      state,
      limit: String(limit),
    }).then(r => r.items ?? []),
  // CI runs. The log is fetched PER JOB and only when asked: a run's logs are megabytes, and a page
  // that fetched them all would be slow for a reader who wanted one job.
  runs: (repo: string, limit = 20) =>
    get<{ runs: RunView[]; count: number }>('runs', { repo, limit: String(limit) }).then(
      r => r.runs ?? [],
    ),
  run: (repo: string, id: number) => get<RunView>('run', { repo, run: String(id) }),
  runJobs: (repo: string, id: number) =>
    get<{ jobs: JobView[]; count: number }>('run/jobs', { repo, run: String(id) }).then(
      r => r.jobs ?? [],
    ),
  jobLog: (repo: string, job: number) =>
    get<{ log: string; bytes: number }>('run/job/log', { repo, job: String(job) }).then(
      r => r.log ?? '',
    ),
}

/** A pull request as the page shows it. */
export interface PullView {
  number: number
  title: string
  state: string
  author: string
  body: string
  head: string
  base: string
  url: string
  draft: boolean
  merged: boolean
  created: string
}

/** One changed file. `patch_truncated` distinguishes "no diff given" from "no change". */
export interface PullFile {
  path: string
  status: string
  additions: number
  deletions: number
  patch: string
  patch_truncated: boolean
}

export interface PullChecks {
  state: string
  sha: string
  checks: Array<{ name: string; status: string }> | null
}
