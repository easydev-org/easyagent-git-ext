// The UI talks ONLY to this extension's own HTTP surface, and every read comes from the LOCAL
// workspace. Nothing here calls api.github.com: the checkout is already on disk, so the tree, the
// file contents and the diff are free — and they include the session's own edits, which the remote
// does not have yet.
//
// Routing is hash-based so a host can deep-link (`/ext/github/#/ws/<id>/blob/<path>`) without the
// server needing a route for it.

const app = document.getElementById('app')

/** Base of the JSON API, derived from where this page is mounted. */
const API = new URL('../api/v1/ui/', location.href).pathname.replace(/\/ui\/\.\.\//, '/')

/** Scope parameters the host appends, so the extension can find the right workspace. */
const scope = new URLSearchParams(location.search)

const state = {
  ws: null,            // selected workspace id
  tab: 'code',         // code | changes | log
  dir: '',             // current directory in the tree
  file: null,          // { path, content, binary, size }
  diff: null,
  log: null,
  error: null,
  loading: false,
}

function qs(extra = {}) {
  const p = new URLSearchParams(scope)
  if (state.ws) p.set('ws', state.ws)
  for (const [k, v] of Object.entries(extra)) {
    if (v !== '' && v != null) p.set(k, String(v))
  }
  return p.toString()
}

async function api(path, params) {
  const r = await fetch(`${API}${path}?${qs(params)}`, { headers: { accept: 'application/json' } })
  const body = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(body.error || `${r.status} ${r.statusText}`)
  return body
}

// ---- routing ---------------------------------------------------------------

function route() {
  const h = location.hash.replace(/^#/, '') || '/'
  const parts = h.split('/').filter(Boolean)
  if (parts[0] !== 'ws' || !parts[1]) return { kind: 'root' }
  const ws = parts[1]
  if (parts[2] === 'blob' && parts[3]) return { kind: 'blob', ws, path: parts.slice(3).join('/') }
  if (parts[2] === 'tree') return { kind: 'tree', ws, dir: parts.slice(3).join('/') }
  if (parts[2] === 'changes') return { kind: 'changes', ws }
  if (parts[2] === 'log') return { kind: 'log', ws }
  return { kind: 'tree', ws, dir: '' }
}

function go(hash) {
  location.hash = hash
}

// ---- data ------------------------------------------------------------------

async function load() {
  state.error = null
  try {
    state.loading = true
    const r = route()
    if (r.kind === 'root') {
      const { workspaces } = await api('workspaces')
      if (workspaces.length === 1) {
        go(`#/ws/${workspaces[0].id}/tree/`)
        return
      }
      state.workspaces = workspaces
      return
    }
    if (state.ws !== r.ws) {
      state.ws = r.ws
      state.file = null
      state.diff = null
      state.log = null
    }
    if (r.kind === 'tree') {
      state.tab = 'code'
      state.dir = r.dir || ''
      const { entries } = await api('tree', { dir: state.dir })
      state.entries = entries
    } else if (r.kind === 'blob') {
      state.tab = 'code'
      state.file = await api('file', { path: r.path })
    } else if (r.kind === 'changes') {
      state.tab = 'changes'
      const { changes, branch } = await api('status')
      state.changes = changes
      state.branch = branch
      const { diff } = await api('diff')
      state.diff = diff
    } else if (r.kind === 'log') {
      state.tab = 'log'
      const { entries } = await api('log', { limit: 80 })
      state.log = entries
    }
  } catch (e) {
    state.error = e.message
  } finally {
    state.loading = false
    render()
  }
}

// ---- rendering -------------------------------------------------------------

const el = (tag, props = {}, ...kids) => {
  const n = document.createElement(tag)
  for (const [k, v] of Object.entries(props)) {
    if (k === 'class') n.className = v
    else if (k === 'text') n.textContent = v
    else if (k.startsWith('on')) n.addEventListener(k.slice(2), v)
    else n.setAttribute(k, v)
  }
  for (const kid of kids) if (kid) n.append(kid)
  return n
}

function render() {
  app.replaceChildren()
  if (state.error) {
    app.append(el('div', { class: 'empty err', text: state.error }))
    return
  }
  const r = route()
  if (r.kind === 'root') return renderWorkspaces()
  if (!state.ws) return
  const layout = el('div', { class: 'layout' })
  layout.append(renderTree(), renderMain())
  app.append(layout)
}

function renderWorkspaces() {
  const list = state.workspaces || []
  if (list.length === 0) {
    app.append(el('div', { class: 'empty', text: 'No workspace in this session. Clone one with git-ws-clone.' }))
    return
  }
  const box = el('div', { class: 'side', style: 'width:100%;border:0' })
  for (const w of list) {
    box.append(el('button', {
      class: 'row',
      onclick: () => go(`#/ws/${w.id}/tree/`),
      title: w.repo,
    }, el('span', { class: 'kind', text: 'repo' }), el('span', { class: 'name', text: w.repo || w.id })))
  }
  app.append(el('div', { class: 'tabs', text: 'Workspaces' }), box)
}

function renderTree() {
  const side = el('div', { class: 'side' })
  const crumbs = el('div', { class: 'crumb' })
  const root = el('button', { text: 'repo', onclick: () => go(`#/ws/${state.ws}/tree/`) })
  crumbs.append(root)
  for (const seg of (state.dir || '').split('/').filter(Boolean)) {
    crumbs.append(el('span', { class: 'muted', text: '/' }))
    const upto = (state.dir || '').split('/').slice(0, (state.dir || '').split('/').indexOf(seg) + 1).join('/')
    crumbs.append(el('button', { text: seg, onclick: () => go(`#/ws/${state.ws}/tree/${upto}`) }))
  }
  side.append(crumbs)

  if (state.dir) {
    const parent = state.dir.split('/').slice(0, -1).join('/')
    side.append(el('button', { class: 'row', onclick: () => go(`#/ws/${state.ws}/tree/${parent}`) },
      el('span', { class: 'kind', text: '..' }), el('span', { class: 'name', text: 'up' })))
  }
  for (const e of state.entries || []) {
    const name = e.path.split('/').pop()
    side.append(el('button', {
      class: 'row',
      title: e.path,
      onclick: () => go(e.dir ? `#/ws/${state.ws}/tree/${e.path}` : `#/ws/${state.ws}/blob/${e.path}`),
    }, el('span', { class: 'kind', text: e.dir ? 'dir' : 'file' }), el('span', { class: 'name', text: name })))
  }
  return side
}

function renderMain() {
  const main = el('div', { class: 'main' })
  const tabs = el('div', { class: 'tabs' })
  for (const [id, label] of [['code', 'Code'], ['changes', 'Changes'], ['log', 'History']]) {
    tabs.append(el('button', {
      'aria-selected': state.tab === id ? 'true' : 'false',
      text: label,
      onclick: () => go(id === 'code' ? `#/ws/${state.ws}/tree/${state.dir}` : `#/ws/${state.ws}/${id}`),
    }))
  }
  main.append(tabs)
  const body = el('div', { class: 'body' })
  if (state.loading) body.append(el('div', { class: 'empty', text: 'Loading…' }))
  else if (state.tab === 'code') body.append(renderCode())
  else if (state.tab === 'changes') body.append(renderChanges())
  else body.append(renderLog())
  main.append(body)
  return main
}

function renderCode() {
  if (!state.file) {
    return el('div', { class: 'empty', text: 'Select a file.' })
  }
  const f = state.file
  const wrap = el('div')
  wrap.append(el('div', { class: 'muted', text: `${f.path} · ${f.size} bytes` }))
  if (f.binary) {
    wrap.append(el('div', { class: 'empty', text: 'Binary file — not shown.' }))
    return wrap
  }
  const pre = el('pre', { class: 'code' })
  pre.textContent = f.content ?? ''
  wrap.append(pre)
  return wrap
}

function renderChanges() {
  const wrap = el('div')
  if (state.branch) wrap.append(el('div', { class: 'muted', text: `branch ${state.branch}` }))
  const changes = state.changes || []
  if (changes.length === 0) {
    wrap.append(el('div', { class: 'empty', text: 'No local changes.' }))
    return wrap
  }
  const list = el('div')
  for (const c of changes) {
    list.append(el('button', {
      class: 'row',
      onclick: () => go(`#/ws/${state.ws}/blob/${c.Path}`),
    }, el('span', { class: 'kind', text: c.Status }), el('span', { class: 'name', text: c.Path })))
  }
  wrap.append(list)
  wrap.append(el('div', { class: 'crumb', style: 'padding-top:8px', text: 'diff' }))
  wrap.append(diffNode(state.diff || ''))
  return wrap
}

function diffNode(text) {
  const pre = el('pre', { class: 'diff' })
  for (const line of text.split('\n')) {
    let cls = 'meta'
    if (line.startsWith('+++') || line.startsWith('---')) cls = 'meta'
    else if (line.startsWith('@@')) cls = 'hunk'
    else if (line.startsWith('+')) cls = 'add'
    else if (line.startsWith('-')) cls = 'del'
    pre.append(el('div', { class: cls, text: line || ' ' }))
  }
  return pre
}

function renderLog() {
  const wrap = el('div')
  const entries = state.log || []
  if (entries.length === 0) {
    wrap.append(el('div', { class: 'empty', text: 'No commits.' }))
    return wrap
  }
  for (const e of entries) {
    wrap.append(el('div', { class: 'row' },
      el('span', { class: 'kind', text: (e.Hash || '').slice(0, 7) }),
      el('span', { class: 'name', text: e.Message || '' })))
  }
  return wrap
}

// ---- boot ------------------------------------------------------------------

window.addEventListener('hashchange', load)
load()
