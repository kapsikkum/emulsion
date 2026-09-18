'use strict';
// Views: login, library, roll.

views.login = app => {
  app.innerHTML = html`<div class="login"><form class="card">
    <div class="brand">${LOGO}<div><b>Emulsion</b><small>Film library</small></div></div>
    <label class="field"><span>Password</span><input class="input" type="password" name="pw" autocomplete="current-password" required autofocus></label>
    <button class="btn primary">${icon('lock')}Sign in</button>
    <p class="err" role="alert" style="margin:0;min-height:1.5em"></p>
  </form></div>`.s;
  const form = $('form', app);
  $('input', form).focus();
  form.onsubmit = async e => {
    e.preventDefault();
    const btn = $('button', form);
    btn.disabled = true;
    try {
      await api('/api/login', { Password: form.pw.value });
      await refreshState();
      render();
    } catch (err) {
      $('.err', form).textContent = err.message;
      form.pw.select();
    } finally {
      btn.disabled = false;
    }
  };
};

// ---------- library ----------
const libFilter = { q: '', film: '', camera: '' };

const frameCount = r => r.Count || r.ExportCount || 0;
const rollCard = r => html`<a class="roll" href="#/roll/${enc(r.Dir)}">
  <div class="rebate"><span>${r.Films[0] || 'Unknown stock'}</span><span>${r.ISO[0] ? `ISO ${r.ISO[0]}` : ''}</span></div>
  <div class="cover">${img(thumb(r.Cover, false, r.CoverOrientation), r.Name)}${r.ExportCount ? html`<span class="badge-pos">Positives</span>` : ''}</div>
  <div class="rebate bottom"><span>▸ ${String(frameCount(r)).padStart(2, '0')}</span><span>${r.Date ? fmtDate(r.Date) : 'Undated'}</span></div>
  <div class="meta"><b title="${r.Name}">${r.Name}</b><div class="line"><span>${r.Cameras.join(', ') || 'Unknown camera'}</span><span class="nowrap">${plural(frameCount(r), 'frame')}</span></div></div>
</a>`;

views.library = async (main, _, __, alive) => {
  const settings = S.state.settings;
  main.innerHTML = html`${pageHead('Library')}<div class="rolls">${Array.from({ length: 8 }, () => html`<div class="skeleton" style="aspect-ratio:4/4.2"></div>`)}</div>`.s;
  const all = await rolls(true);
  if (!alive()) return;
  S.onState = ({ scanDone, jobDone }) => { if (scanDone || jobDone) route(); };

  if (!settings.libraries.length) return welcome(main, alive);
  if (!all.length) {
    const scanning = S.state.library.status.startsWith('Scanning');
    main.innerHTML = html`${pageHead('Library')}<div class="empty card">
      ${icon(scanning ? 'refresh' : 'frames', 'big')}<h2>${scanning ? 'Scanning your photos…' : 'No photos yet'}</h2>
      <p>${scanning ? 'This can take a minute for big libraries.' : html`Nothing found in ${settings.libraries.join(', ')}. Add scans to a sub-folder, or import a roll.`}</p>
      ${scanning ? '' : html`<div class="row" style="justify-content:center"><a class="btn primary" href="#/import">${icon('import')}Import a roll</a><button class="btn" data-rescan>${icon('refresh')}Rescan</button></div>`}
    </div>`.s;
    $('[data-rescan]', main)?.addEventListener('click', guard(async () => { await api('/api/rescan', {}); toast('Rescanning…'); }));
    return;
  }

  const films = uniq(all.flatMap(r => r.Films)).sort();
  const cameras = uniq(all.flatMap(r => r.Cameras)).sort();
  const frames = all.reduce((n, r) => n + frameCount(r), 0);
  main.innerHTML = html`${pageHead('Library', null, html`
      <div class="search">${icon('search')}<input class="input" type="search" placeholder="Search rolls, films, cameras" aria-label="Search rolls" value="${libFilter.q}" data-q></div>
      <button class="btn icon" title="Rescan library" aria-label="Rescan library" data-rescan>${icon('refresh')}</button>
      <a class="btn primary" href="#/import">${icon('import')}Import</a>`)}
    <div class="stats">
      <div class="stat"><b>${all.length.toLocaleString()}</b><span>Rolls</span></div>
      <div class="stat"><b>${frames.toLocaleString()}</b><span>Frames</span></div>
      <div class="stat"><b>${films.length}</b><span>Film stocks</span></div>
      <div class="stat"><b>${cameras.length}</b><span>Cameras</span></div>
    </div>
    <div class="toolbar">
      <select class="input" data-film aria-label="Filter by film"><option value="">All films</option>${films.map(f => html`<option ${f === libFilter.film ? 'selected' : ''}>${f}</option>`)}</select>
      <select class="input" data-camera aria-label="Filter by camera"><option value="">All cameras</option>${cameras.map(c => html`<option ${c === libFilter.camera ? 'selected' : ''}>${c}</option>`)}</select>
      <span class="muted" data-count></span>
    </div>
    <div data-list></div>`.s;

  const list = $('[data-list]', main);
  const draw = () => {
    const q = libFilter.q.toLowerCase().trim();
    const shown = all.filter(r =>
      (!libFilter.film || r.Films.includes(libFilter.film)) &&
      (!libFilter.camera || r.Cameras.includes(libFilter.camera)) &&
      (!q || [r.Name, r.Date, ...r.Films, ...r.Cameras, ...r.Lenses].join(' ').toLowerCase().includes(q)));
    const years = new Map();
    shown.forEach(r => {
      const y = r.Date ? r.Date.slice(0, 4) : 'Undated';
      years.set(y, [...(years.get(y) || []), r]);
    });
    $('[data-count]', main).textContent = shown.length === all.length ? '' : `${plural(shown.length, 'roll')} match`;
    list.innerHTML = shown.length
      ? html`${[...years].map(([y, rs]) => html`<section><div class="year"><h2>${y}</h2><span>${plural(rs.length, 'roll')} · ${plural(rs.reduce((n, r) => n + frameCount(r), 0), 'frame')}</span></div>
          <div class="rolls">${rs.map(rollCard)}</div></section>`)}`.s
      : html`<div class="empty">${icon('search', 'big')}<h2>No rolls match</h2><button class="btn" data-clear>Clear filters</button></div>`.s;
    $('[data-clear]', list)?.addEventListener('click', () => {
      Object.assign(libFilter, { q: '', film: '', camera: '' });
      route();
    });
  };
  $('[data-q]', main).oninput = debounce(e => { libFilter.q = e.target.value; draw(); }, 120);
  $('[data-film]', main).onchange = e => { libFilter.film = e.target.value; draw(); };
  $('[data-camera]', main).onchange = e => { libFilter.camera = e.target.value; draw(); };
  $('[data-rescan]', main).onclick = guard(async () => { await api('/api/rescan', {}); toast('Rescanning library…'); });
  draw();
};

// ---------- first launch ----------
async function welcome(main, alive) {
  let films = 0;
  const draw = () => {
    const st = S.state, db = st.filmdb, deps = st.deps;
    const dbReady = db.status === 'Up to date' && films > 0;
    const step = (n, done, title, body) => html`<li class="card welcome-step ${done ? 'done' : ''}">
      <div class="num">${done ? icon('check') : n}</div><div><h2>${title}</h2>${body}</div></li>`;
    const tool = (ok, name, cmd) => html`<div class="tool">${ok ? html`<span class="ok">${icon('check')}</span>` : html`<span class="err">${icon('x')}</span>`}
      <b>${name}</b>${ok ? html`<span class="muted">found</span>` : html`<code>${cmd}</code>`}</div>`;
    main.innerHTML = html`<div class="welcome">
      <header class="welcome-hero">${LOGO}<h1>Welcome to Emulsion</h1>
        <p>Your film rolls, tagged with the stock and camera they were shot on, alongside a library of more than 8,000 films.</p></header>
      <ol class="welcome-steps">
        ${step(1, false, 'Choose where your scans live', html`<p>Pick the folder that holds your scans, where each sub-folder is one roll. Or create an empty folder to import lab zips and cards into. Nothing gets moved.</p>
          <div class="row"><button class="btn primary" data-add>${icon('folder')}Choose folder</button></div>`)}
        ${step(2, dbReady, 'Film database', html`<p>${dbReady ? `${films.toLocaleString()} films downloaded from the Open Source Film Database. Emulsion checks for updates daily.`
          : /fail/i.test(db.status) ? html`<span class="err">${db.status}</span>` : html`<span class="spinner"></span>${db.status || 'Starting…'}`}</p>`)}
        ${step(3, deps.git && deps.exiftool, 'Helper tools', html`<p>Git keeps the film database up to date and ExifTool writes metadata into your photos.</p>
          <div class="tools">${tool(deps.git, 'Git', st.os === 'windows' ? 'winget install Git.Git' : 'install git')}${tool(deps.exiftool, 'ExifTool', st.os === 'windows' ? 'winget install OliverBetz.ExifTool' : 'install exiftool')}</div>
          ${deps.git && deps.exiftool ? '' : html`<p class="faint" style="margin:10px 0 0">Install what's missing, then restart Emulsion.</p>`}`)}
      </ol>
      <footer class="welcome-foot"><span class="muted">Settings, the film database and thumbnails are kept in</span> <code>${st.dataDir}</code>
        ${st.desktop ? html`<button class="btn sm ghost" data-datadir>${icon('external')}Open folder</button>` : ''}</footer>
    </div>`.s;
    $('[data-add]', main).onclick = guard(async () => {
      const p = await pickFolder('Choose your scans folder');
      if (!p) return;
      await api('/api/settings', { Libraries: [p], ImportTo: p, UpdateHours: st.settings.updateHours });
      await refreshState();
      toast('Folder added. Scanning your photos…');
      route();
    });
    $('[data-datadir]', main)?.addEventListener('click', guard(() => api('/api/open', { App: 'data' })));
  };
  const countFilms = async () => { films = (await api('/api/films?limit=1').catch(() => ({ total: 0 }))).total || 0; };
  await countFilms();
  if (!alive()) return;
  let lastStatus = S.state.filmdb.status;
  S.onState = async () => {
    if (S.state.filmdb.status === lastStatus) return;
    lastStatus = S.state.filmdb.status;
    await countFilms();
    if (alive() && !$('.overlay')) draw();
  };
  draw();
}

// ---------- roll ----------
const rollView = { tab: 'positives' };

// splitGear turns "Canon AE-1" into the make and model EXIF wants.
const splitGear = name => {
  const [make, ...rest] = name.trim().split(/\s+/);
  return [make, rest.join(' ') || make];
};

views.roll = async (main, [dir], _, alive) => {
  const [r, apps] = await Promise.all([api('/api/roll?dir=' + enc(dir)), S.state.local ? api('/api/apps').catch(() => []) : []]);
  r.Frames ||= [];
  r.Exports ||= [];
  const film = r.Films[0] ? await api('/api/film?name=' + enc(r.Films[0])).catch(() => null) : null;
  if (!alive()) return;
  const tab = r.Exports.length && (rollView.tab === 'positives' || !r.Frames.length) ? 'positives' : 'scans';
  const shown = tab === 'positives' ? r.Exports : r.Frames;
  const openers = apps.filter(a => a.Found);
  const fact = (ic, label, value, href) => {
    const inner = html`<div class="ico">${ic}</div><div><small>${label}</small><b>${value}</b></div>`;
    return href ? html`<a class="fact" href="${href}">${inner}</a>` : html`<div class="fact">${inner}</div>`;
  };
  const filmIcon = film?.fields?.[11] ? img(filmImg(film.fields[11])) : icon('film');
  main.innerHTML = html`<div style="padding-top:22px"><a class="back" href="#/">${icon('left')}Library</a></div>
    <section class="roll-hero">
      <div>
        <h1>${r.Name}</h1>
        <div class="path">${r.Dir}</div>
        <div class="facts">
          ${fact(filmIcon, 'Film', r.Films.join(', ') || 'Not set', film ? `#/film/${film.line}` : null)}
          ${fact(icon('camera'), 'Camera', r.Cameras.join(', ') || 'Not set')}
          ${r.Lenses.length ? fact(icon('lens'), 'Lens', r.Lenses.join(', ')) : ''}
          ${fact(icon('speed'), 'ISO', r.ISO.join(', ') || '—')}
          ${fact(icon('calendar'), 'Shot', r.Date ? fmtDate(r.Date) : 'Undated')}
          ${fact(icon('frames'), 'Frames', r.RawCount ? `${r.Count} · ${r.RawCount} RAW` : r.Count || r.ExportCount)}
        </div>
      </div>
      <div class="row">
        ${S.state.local ? html`<div class="menu-wrap"><button class="btn" data-openmenu aria-haspopup="menu" aria-expanded="false">${icon('external')}Open in</button>
          <div class="menu" role="menu" hidden>
            ${openers.map(a => html`<button role="menuitem" data-open="${a.ID}">${a.Name}${a.TakesFiles ? '' : html`<small>opens with the folder</small>`}</button>`)}
            <button role="menuitem" data-open="folder">${S.state.os === 'darwin' ? 'Finder' : 'File Explorer'}</button>
            ${apps.some(a => !a.Found) ? html`<a role="menuitem" href="#/settings">Set up more apps…</a>` : ''}
          </div></div>` : ''}
        <button class="btn" data-rename>${icon('edit')}Rename</button>
        <button class="btn primary" data-edit>${icon('edit')}Edit roll</button>
      </div>
    </section>
    ${r.Exports.length && r.Frames.length ? html`<div class="toolbar"><div class="seg" role="tablist">
      <button role="tab" data-tab="positives" class="${tab === 'positives' ? 'on' : ''}">Positives · ${r.Exports.length}</button>
      <button role="tab" data-tab="scans" class="${tab === 'scans' ? 'on' : ''}">Scans · ${r.Frames.length}</button></div>
      <span class="muted">${tab === 'positives' ? 'Converted exports, e.g. from NegPy' : 'Original scans'}</span></div>` : ''}
    <div class="sheet"><div class="frames">${shown.map((p, i) => html`<button class="frame" data-i="${i}" aria-label="Open frame ${i + 1}">
      <div class="img">${img(thumb(p.SourceFile, false, p.Orientation), `Frame ${i + 1}`)}${isRawFile(p.SourceFile) ? html`<span class="badge-raw">RAW</span>` : ''}</div>
      <div class="n"><span>▸ ${String(i + 1).padStart(2, '0')}</span><span>${p.SourceFile.split('/').pop()}</span></div></button>`)}</div></div>`.s;
  $$('.frame', main).forEach(b => (b.onclick = () => lightbox(shown, +b.dataset.i, { roll: r })));
  $('[data-edit]', main).onclick = () => editRoll(r);
  $('[data-rename]', main).onclick = () => renameRoll(r);
  $$('[data-tab]', main).forEach(b => (b.onclick = () => { rollView.tab = b.dataset.tab; route(); }));

  const menuBtn = $('[data-openmenu]', main);
  if (menuBtn) {
    const menu = menuBtn.nextElementSibling;
    const setOpen = open => {
      menu.hidden = !open;
      menuBtn.setAttribute('aria-expanded', String(open));
      if (open) {
        $('button, a', menu)?.focus();
        setTimeout(() => document.addEventListener('click', () => setOpen(false), { once: true }));
      }
    };
    menuBtn.onclick = e => { e.stopPropagation(); setOpen(menu.hidden); };
    menu.onkeydown = e => {
      const items = $$('button, a', menu);
      const i = items.indexOf(document.activeElement);
      if (e.key === 'Escape') { setOpen(false); menuBtn.focus(); }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); items[(i + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length].focus(); }
    };
    $$('[data-open]', menu).forEach(b => (b.onclick = guard(async () => {
      setOpen(false);
      const app = b.dataset.open;
      // Lightroom and editors get the frames on screen; NegPy wants the scans folder.
      await api('/api/open', { App: app, Dir: r.Dir, Files: app === 'folder' || app === 'negpy' ? [] : shown.map(p => p.SourceFile) });
      const info = apps.find(a => a.ID === app);
      toast(info && !info.TakesFiles ? `Opened ${info.Name} and the roll folder. Drag the folder into ${info.Name}.` : `Opened in ${info?.Name || (S.state.os === 'darwin' ? 'Finder' : 'File Explorer')}`);
    })));
  }
};

const isRawFile = f => /\.(nef|nrw|cr2|cr3|crw|arw|srf|sr2|raf|orf|rw2|pef|srw|3fr|fff|iiq|rwl|x3f|erf|mef|mos|kdc|dcr)$/i.test(f);

// Lab file names, e.g. B001738-R1-00-36A.JPG (order, roll, scan sequence, frame on the film edge).
const LAB_NAME = /^([a-z]*\d+)-r(\d+)-(\d+)-(\d+[a-z]?|[a-z]{1,2})$/i;
const dateIn = s => (s.match(/(19|20)\d{2}[-_.]?(0[1-9]|1[0-2])[-_.]?(0[1-9]|[12]\d|3[01])/) || [''])[0].replace(/[-_.]/g, '').replace(/^(\d{4})(\d{2})(\d{2})$/, '$1-$2-$3');

function renameRoll(r) {
  const film = (r.Films[0] || '').replace(/#\S+/g, '').replace(/\s+/g, ' ').trim();
  const date = r.Date || dateIn(r.Name);
  const lab = LAB_NAME.exec((r.Frames?.[0]?.SourceFile || r.Exports?.[0]?.SourceFile || '').split('/').pop()?.replace(/\.[^.]+$/, '') || '');
  const suggestions = uniq([
    date && film ? `${date} ${film}` : '',
    film,
    date && lab ? `${date} ${lab[1].toUpperCase()} roll ${+lab[2]}` : '',
    lab ? `${lab[1].toUpperCase()} roll ${+lab[2]}` : '',
  ]).filter(s => s && s !== r.Name);
  const { box, close } = layer(html`<header><h2>Rename roll</h2><button class="btn ghost icon" data-close aria-label="Close">${icon('x')}</button></header>
    <form class="body" id="renameform">
      <label class="field"><span>Folder name</span><input class="input" name="name" value="${r.Name}" required autofocus></label>
      ${suggestions.length ? html`<div><div class="subhead" style="margin:0 0 8px">Suggestions</div>
        <div class="chips">${suggestions.map(s => html`<button type="button" class="btn sm" data-suggest="${s}">${s}</button>`)}</div></div>` : ''}
      <div class="note">Renames the folder on disk. The photos and their metadata don't change.</div>
      <p class="mono faint" style="margin:0;overflow-wrap:anywhere">${r.Dir}</p>
    </form>
    <footer><button class="btn" data-close>Cancel</button><button class="btn primary" form="renameform">${icon('check')}Rename</button></footer>`, 'drawer');
  const form = $('form', box);
  $$('[data-suggest]', box).forEach(b => (b.onclick = () => { form.name.value = b.dataset.suggest; form.name.focus(); }));
  form.onsubmit = guard(async e => {
    e.preventDefault();
    const res = await api('/api/roll/rename', { Dir: r.Dir, Name: form.name.value });
    S.rolls = null;
    close();
    toast('Roll renamed');
    location.hash = `#/roll/${enc(res.Dir)}`;
  });
}

async function editRoll(r) {
  const one = a => (a.length === 1 ? a[0] : '');
  const all = [...(r.Frames || []), ...(r.Exports || [])];
  const dates = uniq(all.map(f => (f.DateTimeOriginal || '').slice(0, 10)));
  const { box, close } = layer(html`<header><h2>Edit roll</h2><button class="btn ghost icon" data-close aria-label="Close">${icon('x')}</button></header>
    <form class="body" id="rollform">
      <label class="field"><span>Film stock</span><input class="input" name="Film" value="${one(r.Films)}" placeholder="${r.Films.length > 1 ? 'Mixed' : 'Search the film database'}"></label>
      <div class="fields two">
        <label class="field"><span>ISO</span><input class="input" name="ISO" inputmode="numeric" value="${one(r.ISO)}" placeholder="${r.ISO.length > 1 ? 'Mixed' : ''}"></label>
        <label class="field"><span>Date shot</span><input class="input" type="date" name="Date" value="${one(dates)}"></label>
      </div>
      <label class="field"><span>Camera</span><input class="input" name="Camera" value="${one(r.Cameras)}" placeholder="${r.Cameras.length > 1 ? 'Mixed' : 'Search your gear, e.g. Canon AE-1'}"></label>
      <label class="field"><span>Lens</span><input class="input" name="Lens" value="${one(r.Lenses)}" placeholder="${r.Lenses.length > 1 ? 'Mixed' : 'Search your gear, e.g. Canon FD 50mm f/1.4'}"></label>
      <div class="note">Written into all ${plural(all.length, 'file')} as EXIF/XMP, so other photo apps see it too. Unchanged fields are left alone.</div>
    </form>
    <footer><button class="btn" data-close>Cancel</button><button class="btn primary" form="rollform">${icon('check')}Write metadata</button></footer>`, 'drawer');
  const form = $('form', box);
  const initial = Object.fromEntries(new FormData(form));
  filmPicker(form.Film, f => { if (f.ISO) form.ISO.value = f.ISO; });
  gearPicker(form.Camera, 'body');
  gearPicker(form.Lens, 'lens');
  form.onsubmit = guard(async e => {
    e.preventDefault();
    const now = Object.fromEntries(new FormData(form));
    const changed = Object.fromEntries(Object.entries(now).filter(([k, v]) => v.trim() !== (initial[k] || '').trim() && v.trim() !== ''));
    if (changed.Camera) { // photos store the make and model separately
      [changed.Make, changed.Model] = splitGear(changed.Camera);
      delete changed.Camera;
    }
    if (!Object.keys(changed).length) return close();
    const btn = $('footer .primary', box);
    btn.disabled = true;
    btn.textContent = 'Writing…';
    try {
      await api('/api/roll', { Dir: r.Dir, ...changed });
      S.rolls = null;
      close();
      toast(`Updated ${plural(all.length, 'file')}`);
      route();
    } finally {
      btn.disabled = false;
    }
  });
}
