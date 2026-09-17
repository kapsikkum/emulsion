'use strict';
// Emulsion UI core: rendering helpers, API, shell, shared components. Views live in views.js.

// ---------- html ----------
class Raw { constructor(s) { this.s = s; } }
const raw = s => new Raw(s);
const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
function html(strings, ...vals) {
  let out = '';
  strings.forEach((s, i) => {
    out += s;
    if (i < vals.length) {
      const v = vals[i];
      if (v === false || v === null || v === undefined) return;
      out += Array.isArray(v) ? v.map(x => (x instanceof Raw ? x.s : esc(x))).join('') : v instanceof Raw ? v.s : esc(v);
    }
  });
  return raw(out);
}
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const enc = encodeURIComponent;
// size: falsy = grid, true = large preview, 'f' = full resolution. o (EXIF orientation) keeps rotated thumbnails from coming out of the browser cache.
const thumb = (f, size, o) => `/api/thumb?${size === 'f' ? 's=f&' : size ? 's=l&' : ''}${o > 1 ? `o=${o}&` : ''}f=${enc(f)}`;
const filmImg = pic => `/filmimg/${enc(pic)}`;
const img = (src, alt = '') => html`<img class="lazy" loading="lazy" decoding="async" alt="${alt}" src="${src}" onload="this.classList.add('loaded')" onerror="this.classList.add('loaded');this.style.visibility='hidden'">`;
const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };
const plural = (n, one, many = one + 's') => `${n.toLocaleString()} ${n === 1 ? one : many}`;
const uniq = a => [...new Set(a.filter(Boolean))];

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
function fmtDate(d) {
  if (!d) return 'Undated';
  const [y, m, day] = d.split('-').map(Number);
  return `${day} ${MONTHS[m - 1]} ${y}`;
}
function ago(t) {
  const ms = Date.now() - new Date(t).getTime();
  if (!t || new Date(t).getFullYear() < 2000) return 'never';
  const m = Math.round(ms / 60000);
  if (m < 1) return 'just now';
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  return `${Math.round(h / 24)} d ago`;
}
const today = () => { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`; };

// ---------- icons ----------
const ICONS = {
  grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  film: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M7 3v18M17 3v18M3 7.5h4M3 12h4M3 16.5h4M17 7.5h4M17 12h4M17 16.5h4"/>',
  import: '<path d="M12 3v12M7 10l5 5 5-5"/><path d="M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4"/>',
  settings: '<path d="M4 6h9M19 6h1M4 12h3M13 12h7M4 18h11M21 18h-1"/><circle cx="16" cy="6" r="2.5"/><circle cx="10" cy="12" r="2.5"/><circle cx="18" cy="18" r="2.5"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  x: '<path d="M18 6 6 18M6 6l12 12"/>',
  left: '<path d="m15 18-6-6 6-6"/>',
  right: '<path d="m9 18 6-6-6-6"/>',
  up: '<path d="m18 15-6-6-6 6"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  camera: '<path d="M3 8a2 2 0 0 1 2-2h2l2-2h6l2 2h2a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><circle cx="12" cy="13" r="4"/>',
  lens: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="4"/><path d="M12 3v5M20.5 9.5 16 11M17 19.5 13.5 16M7 19.5 10.5 16M3.5 9.5 8 11"/>',
  calendar: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>',
  edit: '<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z"/><path d="m13.5 6.5 4 4"/>',
  refresh: '<path d="M20 11a8 8 0 0 0-14.6-4.5L4 8M4 4v4h4M4 13a8 8 0 0 0 14.6 4.5L20 16M20 20v-4h-4"/>',
  check: '<path d="m5 12 5 5 9-10"/>',
  external: '<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  trash: '<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>',
  lock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  speed: '<path d="M4 17a8 8 0 1 1 16 0"/><path d="m12 17 4-6"/>',
  frames: '<rect x="3" y="6" width="14" height="12" rx="1.5"/><path d="M21 8v10a2 2 0 0 1-2 2H7"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
  alert: '<path d="M12 3 2 20h20z"/><path d="M12 10v4M12 17v.01"/>',
  undo: '<path d="M9 14 4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/>',
  database: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
  box: '<path d="M3 7.5 12 3l9 4.5v9L12 21l-9-4.5z"/><path d="m3 7.5 9 4.5 9-4.5M12 12v9"/>',
  minus: '<path d="M5 12h14"/>',
  rotl: '<path d="M4 12a8 8 0 1 0 2.4-5.7L4 8.5"/><path d="M4 4v4.5h4.5"/>',
  rotr: '<path d="M20 12a8 8 0 1 1-2.4-5.7L20 8.5"/><path d="M20 4v4.5h-4.5"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8v.01"/>',
};
const icon = (name, cls = '') => raw(`<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name]}</svg>`);
const LOGO = raw('<svg viewBox="0 0 64 64" aria-hidden="true"><defs><linearGradient id="lg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#ffb35c"/><stop offset="1" stop-color="#ff6a13"/></linearGradient></defs><circle cx="32" cy="34" r="24" fill="url(#lg)"/><circle cx="32" cy="34" r="15" fill="#0f0d0c"/><circle cx="32" cy="34" r="6" fill="url(#lg)"/><rect x="28" y="2" width="8" height="10" rx="2.5" fill="url(#lg)"/></svg>');

// ---------- state & api ----------
const S = { state: null, rolls: null, gear: null, route: '', scroll: {}, onState: null, token: 0 };
const views = {};

async function api(path, body) {
  const opt = body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
  const r = await fetch(path, opt);
  const data = await r.json().catch(() => ({}));
  if (r.status === 401) {
    S.state = { authed: false };
    render();
    throw new Error('Please sign in');
  }
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

function toast(msg, isError) {
  const t = document.createElement('div');
  t.className = 'toast' + (isError ? ' error' : '');
  t.setAttribute('role', isError ? 'alert' : 'status');
  t.innerHTML = html`${icon(isError ? 'alert' : 'check')}<span>${msg}</span>`.s;
  $('#toasts').append(t);
  setTimeout(() => t.remove(), isError ? 7000 : 3800);
}
const guard = fn => async (...a) => { try { return await fn(...a); } catch (e) { toast(e.message, true); } };

async function refreshState() {
  const prevLib = S.state?.library?.status;
  const prevJob = S.state?.library?.job;
  S.state = await fetch('/api/state').then(r => r.json());
  if (!S.state.authed) { if (!$('.login')) render(); return; }
  renderStatus();
  const lib = S.state.library;
  const scanDone = prevLib && prevLib !== lib.status && !lib.status.startsWith('Scanning');
  const jobDone = prevJob?.Running && !lib.job.Running;
  if (scanDone || jobDone) {
    S.rolls = null;
    S.gear = null;
  }
  if (jobDone) toast(lib.job.Error ? `${lib.job.Message}: ${lib.job.Error}` : lib.job.Message, !!lib.job.Error);
  S.onState?.({ scanDone, jobDone });
}

async function rolls(force) {
  if (!S.rolls || force) S.rolls = await api('/api/rolls');
  return S.rolls;
}
async function gear() {
  if (!S.gear) S.gear = await api('/api/gear');
  return S.gear;
}

// ---------- shell ----------
const NAV = [
  ['library', '#/', 'Library', 'grid'],
  ['films', '#/films', 'Films', 'film'],
  ['import', '#/import', 'Import', 'import'],
  ['settings', '#/settings', 'Settings', 'settings'],
];

function render() {
  const app = $('#app');
  if (!S.state) return;
  if (!S.state.authed) {
    app.innerHTML = '';
    views.login(app);
    return;
  }
  app.innerHTML = html`<div class="shell">
    <aside class="sidebar">
      <div class="brand">${LOGO}<div><b>Emulsion</b><small>Film library</small></div></div>
      <nav class="nav">${NAV.map(([k, href, label, ic]) => html`<a data-nav="${k}" href="${href}">${icon(ic)}<span>${label}</span></a>`)}</nav>
      <div class="grow"></div>
      <div class="status" id="status"></div>
    </aside>
    <main class="main" id="main"></main>
  </div>`.s;
  renderStatus();
  route();
}

function renderStatus() {
  const box = $('#status');
  if (!box || !S.state.library) return;
  const { library: lib, filmdb: db } = S.state;
  const dot = s => /fail|not installed|error/i.test(s) ? 'err' : /…/.test(s) ? 'busy' : 'ok';
  const job = lib.job;
  const up = S.state.update || {};
  box.innerHTML = html`
    ${up.available ? html`<a class="row status-link" href="#/settings/updates"><i class="dot busy"></i><div><div class="label">Update</div><div class="value">${up.installing ? `Installing ${up.latest}…` : `Emulsion ${up.latest} is available`}</div></div></a>` : ''}
    ${job.Running ? html`<div class="row"><i class="dot busy"></i><div><div class="label">Import</div><div class="value">${job.Message} · ${job.Done}/${job.Total}</div><div class="progress" style="margin-top:6px"><i style="width:${Math.round(100 * job.Done / Math.max(1, job.Total))}%"></i></div></div></div>` : ''}
    <div class="row"><i class="dot ${dot(lib.status)}"></i><div><div class="label">Library</div><div class="value" title="${lib.status}">${lib.status || 'Idle'}</div></div></div>
    <div class="row"><i class="dot ${dot(db.status)}"></i><div><div class="label">Film database</div><div class="value" title="${db.status}">${db.status || 'Waiting'}${db.status === 'Up to date' ? ` · ${ago(db.updated)}` : ''}</div></div></div>`.s;
}

function route() {
  const main = $('#main');
  if (!main) return;
  const hash = location.hash || '#/';
  const [pathPart, query] = hash.slice(2).split('?');
  const [name, ...rest] = pathPart.split('/');
  const view = views[name || 'library'] ? name || 'library' : 'library';
  $$('.nav a').forEach(a => a.classList.toggle('on', a.dataset.nav === (view === 'roll' ? 'library' : view === 'film' ? 'films' : view)));
  S.route = hash;
  S.onState = null;
  const token = ++S.token;
  const alive = () => token === S.token;
  main.innerHTML = '';
  Promise.resolve(views[view](main, rest.map(decodeURIComponent), new URLSearchParams(query || ''), alive))
    .then(() => { if (alive()) requestAnimationFrame(() => scrollTo(0, S.scroll[hash] || 0)); })
    .catch(e => { if (alive()) main.innerHTML = html`<div class="empty">${icon('alert', 'big')}<h2>Something went wrong</h2><p>${e.message}</p><a class="btn" href="#/">Back to library</a></div>`.s; });
}

addEventListener('scroll', () => { S.scroll[S.route] = scrollY; }, { passive: true });
addEventListener('hashchange', route);
addEventListener('DOMContentLoaded', async () => {
  await refreshState().catch(() => {});
  render();
  // Say so once after an update restart.
  try {
    if (S.state?.updatedFrom && localStorage.getItem('emulsion.updatedTo') !== S.state.version) {
      localStorage.setItem('emulsion.updatedTo', S.state.version);
      toast(`Updated from ${S.state.updatedFrom} to ${S.state.version}`);
    }
  } catch { /* storage unavailable */ }
  const poll = async () => {
    try { await refreshState(); } catch { /* offline: keep trying */ }
    setTimeout(poll, S.state?.library?.job?.Running || /…/.test(S.state?.library?.status + S.state?.filmdb?.status) ? 1200 : 4000);
  };
  setTimeout(poll, 1500);
});

const pageHead = (title, sub, actions = '') => html`<header class="page-head"><div><h1>${title}</h1>${sub ? html`<div class="sub">${sub}</div>` : ''}</div><div class="spacer"></div>${actions}</header>`;

// ---------- layers ----------
function layer(content, cls, onClose) {
  const prevFocus = document.activeElement;
  const ov = document.createElement('div');
  ov.className = 'overlay';
  const box = document.createElement('div');
  box.className = cls;
  box.setAttribute('role', 'dialog');
  box.setAttribute('aria-modal', 'true');
  box.innerHTML = content.s;
  document.body.append(ov, box);
  const onKey = e => { if (e.key === 'Escape' && !e.defaultPrevented) close(); };
  function close() {
    ov.remove();
    box.remove();
    removeEventListener('keydown', onKey);
    prevFocus?.focus?.();
    onClose?.();
  }
  ov.onclick = close;
  addEventListener('keydown', onKey);
  $$('[data-close]', box).forEach(b => (b.onclick = close));
  setTimeout(() => ($('[autofocus]', box) || $('input,button', box))?.focus(), 30);
  return { box, close };
}

// Folder picker over the machine running Emulsion (works the same remotely).
// With { zips: true } lab .zip files are listed too and picking one resolves to its path.
function pickFolder(title = 'Choose a folder', start = '', { zips = false } = {}) {
  return new Promise(resolve => {
    let picked = null;
    const { box, close } = layer(html`<header><h2>${title}</h2><button class="btn ghost icon" data-close aria-label="Close">${icon('x')}</button></header>
      <form class="goto"><input class="input mono sm" name="p" placeholder="Paste a path and press Enter" aria-label="Go to path" spellcheck="false"></form>
      <div class="places"></div><div class="crumbs"></div><div class="list"></div>
      <footer><span class="muted count" style="flex:1"></span>
        <form class="newfolder" hidden><input class="input sm" name="n" placeholder="New folder name" aria-label="New folder name"><button class="btn sm">Create</button></form>
        <button class="btn ghost" data-new>${icon('plus')}New folder</button><button class="btn" data-close>Cancel</button><button class="btn primary" data-pick>Select this folder</button></footer>`, 'modal picker', () => resolve(picked));
    const finish = v => { picked = v; close(); };
    let current = '';
    const load = guard(async p => {
      const d = await api('/api/fs?path=' + enc(p));
      current = d.Path;
      const win = S.state.os === 'windows';
      const crumbs = [{ name: win ? 'This PC' : '/', path: win ? '' : '/' }];
      let acc = '';
      current.split('/').filter(Boolean).forEach((part, i) => {
        acc = i === 0 && /^[A-Za-z]:$/.test(part) ? part + '/' : (acc.endsWith('/') ? acc : acc + '/') + part;
        crumbs.push({ name: part, path: acc });
      });
      $('.places', box).innerHTML = html`${(d.Places || []).map(p => html`<button class="btn sm" data-go="${p.Path}">${icon('folder')}${p.Name}</button>`)}`.s;
      $('.crumbs', box).innerHTML = html`${crumbs.map((c, i) => html`${i ? html`<i>/</i>` : ''}<button data-go="${c.path}">${c.name}</button>`)}`.s;
      const zipList = zips ? d.Zips || [] : [];
      $('.list', box).innerHTML = html`${d.Path ? html`<button class="dir" data-go="${d.Parent}">${icon('up')}<span class="muted">Up one level</span></button>` : ''}
        ${d.Dirs.map(x => html`<button class="dir" data-go="${x.Path}">${icon('folder')}<span>${x.Name}</span></button>`)}
        ${zipList.map(x => html`<button class="dir" data-zip="${x.Path}">${icon('box')}<span>${x.Name}</span><span class="chip accent" style="margin-left:auto">Import zip</span></button>`)}
        ${!d.Dirs.length && !zipList.length ? html`<p class="muted" style="padding:10px">No sub-folders${zips ? ' or zips' : ''} here.</p>` : ''}`.s;
      $$('[data-zip]', box).forEach(b => (b.onclick = () => finish(b.dataset.zip)));
      $('.count', box).textContent = d.Path ? (d.Images ? `${plural(d.Images, 'image')} in this folder` : 'No images directly in this folder') : '';
      $('[data-pick]', box).disabled = $('[data-new]', box).disabled = !d.Path;
      $$('[data-go]', box).forEach(b => (b.onclick = () => load(b.dataset.go)));
      $('.list', box).scrollTop = 0;
    });
    $('[data-pick]', box).onclick = () => finish(current);
    const nf = $('.newfolder', box);
    $('[data-new]', box).onclick = () => {
      nf.hidden = false;
      $('.count', box).hidden = true;
      nf.n.focus();
    };
    const create = guard(async () => {
      const name = nf.n.value.trim();
      if (!name) return;
      const res = await api('/api/fs/mkdir', { Parent: current, Name: name });
      nf.hidden = true;
      nf.n.value = '';
      $('.count', box).hidden = false;
      await load(res.Path);
    });
    nf.onsubmit = e => { e.preventDefault(); create(); };
    nf.n.onkeydown = e => { if (e.key === 'Enter') { e.preventDefault(); create(); } else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); nf.hidden = true; $('.count', box).hidden = false; } };
    const goto = $('.goto input', box);
    $('.goto', box).onsubmit = e => e.preventDefault();
    goto.onkeydown = e => {
      if (e.key !== 'Enter') return;
      e.preventDefault();
      const p = goto.value.trim().replace(/^"|"$/g, '').replace(/\\/g, '/');
      if (zips && /\.zip$/i.test(p)) return finish(p);
      if (p) load(p);
    };
    load(start);
  });
}

// Film autocomplete against the film database.
function filmPicker(input, onPick) {
  const wrap = input.parentElement;
  wrap.classList.add('ac');
  input.setAttribute('autocomplete', 'off');
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-expanded', 'false');
  const list = document.createElement('div');
  list.className = 'ac-list';
  list.hidden = true;
  list.setAttribute('role', 'listbox');
  wrap.append(list);
  let items = [], sel = -1;
  const show = () => {
    list.hidden = !items.length;
    input.setAttribute('aria-expanded', String(!list.hidden));
    list.innerHTML = html`${items.map((f, i) => html`<div class="ac-item ${i === sel ? 'on' : ''}" role="option" data-i="${i}">
      <div class="thumb">${f.Pic ? img(filmImg(f.Pic)) : icon('film')}</div>
      <div><b>${f.Name}</b><small>${f.Avail === 2 ? html`<span class="accent">In production</span> · ` : ''}${[f.Maker, f.Begin && (f.End ? `${f.Begin}–${f.End}` : f.Begin)].filter(Boolean).join(' · ')}${f.Rolls ? ` · shot ${plural(f.Rolls, 'roll')}` : ''}</small></div></div>`)}`.s;
    $$('.ac-item', list).forEach(el => el.onmousedown = e => { e.preventDefault(); pick(items[+el.dataset.i]); });
  };
  const pick = f => { input.value = f.Name; items = []; show(); onPick?.(f); };
  const search = debounce(async () => {
    const q = input.value.trim();
    if (q.length < 2) { items = []; return show(); }
    try { items = (await api(`/api/films?limit=8&q=${enc(q)}`)).items; } catch { items = []; }
    sel = -1;
    show();
  }, 140);
  input.addEventListener('input', search);
  input.addEventListener('blur', () => setTimeout(() => { items = []; show(); }, 120));
  input.addEventListener('keydown', e => {
    if (list.hidden) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      sel = (sel + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
      show();
    } else if (e.key === 'Enter' && sel >= 0) {
      e.preventDefault();
      pick(items[sel]);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      items = [];
      show();
    }
  });
}
