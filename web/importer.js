'use strict';
// View: Lightroom-style import. Source (folder, card, lab zip or upload) → pick frames → file them with a folder template.

const STRUCTURES = [
  ['{name}', 'Roll name'],
  ['{yyyy}/{name}', 'Year / Roll name'],
  ['{yyyy}/{date} {name}', 'Year / Date + roll name'],
  ['{yyyy}/{mm}/{name}', 'Year / Month / Roll name'],
  ['{film}/{name}', 'Film / Roll name'],
  ['{camera}/{yyyy}/{name}', 'Camera / Year / Roll name'],
];
const RENAMES = [
  ['', 'Keep original names'],
  ['{name}_{seq}', 'Roll name_01'],
  ['{date}_{seq}', 'Date_01'],
  ['{name}_{original}', 'Roll name_original'],
];

const ROLL_STYLES = [
  ['tidy', 'Tidied folder name', g => g.Name],
  // Skips the date when the folder structure already adds one, so folders don't read "2026-05-03 2026-05-03 Portra".
  ['dated', 'Date + tidied name', (g, r) => (/\{date\}/.test(imp.structure) ? g.Name : `${r.Date || g.Date} ${g.Name}`)],
  ['folder', 'Folder name as-is', g => g.Folder],
];

const imp = {
  rollStyle: 'tidy', rolls: {},
  source: '', src: null, sub: true, selected: new Set(), hideDups: false, uploads: [],
  mode: 'copy', dest: '', structure: null, rename: null, split: false,
  name: '', nameTouched: false, Film: '', ISO: '', Make: '', Model: '', Lens: '', Date: '', after: '',
};

views.import = async (main, _, query, alive) => {
  const s = S.state.settings;
  const [g, apps] = await Promise.all([gear().catch(() => ({ makes: [], models: [], lenses: [] })), api('/api/apps').catch(() => [])]);
  if (!alive()) return;
  imp.structure ??= s.structure || '{name}';
  imp.rename ??= s.rename || '';
  if (!s.libraries.includes(imp.dest)) imp.dest = s.importTo || s.libraries[0] || '';
  if (query.get('film')) {
    imp.Film = query.get('film');
    imp.ISO = (await api('/api/film?name=' + enc(imp.Film)).catch(() => ({}))).iso || '';
    if (!alive()) return;
  }
  const openApps = apps.filter(a => a.Found);

  main.innerHTML = html`${pageHead('Import', 'Folders, memory cards and lab zips', html`<span class="muted" data-jobline></span>`)}
    <div data-job></div>
    <div class="importer">
      <aside class="card imp-panel" data-source></aside>
      <section class="imp-center" data-center></section>
      <aside class="card imp-panel imp-dest" data-dest></aside>
    </div>
    <div class="dropveil" hidden>${icon('import', 'big')}<b>Drop to upload</b><span>Zips from your lab, or loose scans</span></div>`.s;

  const $src = $('[data-source]', main), $center = $('[data-center]', main), $dest = $('[data-dest]', main);

  // ---------- source ----------
  const drawSource = () => {
    $src.innerHTML = html`<h2>Source</h2>
      <div class="stack" style="gap:10px">
        <button class="btn primary" data-pick-folder>${icon('folder')}Folder or card</button>
        <button class="btn" data-pick-zip>${icon('box')}Lab zip on this ${S.state.local ? 'PC' : 'server'}</button>
        <label class="dropzone" tabindex="0">${icon('import')}<b>Upload from this device</b><span>Drop a zip or scans here, or click to browse</span>
          <input type="file" multiple accept=".zip,image/*,.tif,.tiff,.dng,.nef,.cr2,.cr3,.arw,.raf,.orf,.rw2,.pef,.xmp" hidden></label>
      </div>
      ${imp.uploading ? html`<div class="note" style="margin-top:14px"><div class="row" style="justify-content:space-between"><span>Uploading ${imp.uploading.name}</span><span class="mono">${imp.uploading.pct}%</span></div>
        <div class="progress" style="margin-top:8px"><i style="width:${imp.uploading.pct}%"></i></div></div>` : ''}
      ${imp.source ? html`<div class="subhead" style="margin:22px 0 10px">Selected</div>
        <div class="picked">${icon(imp.src?.Zip ? 'box' : 'folder')}<span class="path">${imp.source}</span></div>
        ${imp.src && !imp.src.Zip ? html`<label class="switch" style="margin-top:12px"><input type="checkbox" data-sub ${imp.sub ? 'checked' : ''}><i></i><span class="muted">Include sub-folders</span></label>` : ''}` : ''}
      ${imp.uploads.length > 1 ? html`<div class="subhead" style="margin:22px 0 10px">Uploaded zips</div><div class="list">${imp.uploads.map(u => html`
        <button class="item dir-btn ${u === imp.source ? 'on' : ''}" data-load="${u}">${icon('box', 'lead')}<span class="path">${u.split('/').pop()}</span></button>`)}</div>` : ''}`.s;

    $('[data-pick-folder]', $src).onclick = guard(async () => {
      const p = await pickFolder('Import from a folder or card', imp.src && !imp.src.Zip ? imp.source : '');
      if (p) await load(p);
    });
    $('[data-pick-zip]', $src).onclick = guard(async () => {
      const p = await pickFolder('Choose a lab zip', '', { zips: true });
      if (p) await load(p);
    });
    const input = $('input[type=file]', $src);
    input.onchange = () => upload([...input.files]);
    $('.dropzone', $src).onkeydown = e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); input.click(); } };
    $('[data-sub]', $src)?.addEventListener('change', e => { imp.sub = e.target.checked; load(imp.source); });
    $$('[data-load]', $src).forEach(b => (b.onclick = () => load(b.dataset.load)));
  };

  const load = guard(async path => {
    imp.source = path;
    imp.src = null;
    drawSource();
    $center.innerHTML = html`<div class="empty">${icon('refresh', 'big spin')}<h2>Reading ${path.split('/').pop()}…</h2><p>${/\.zip$/i.test(path) ? 'Unpacking the zip. Big lab downloads take a moment.' : 'Looking for photos.'}</p></div>`.s;
    try {
      imp.src = await api(`/api/import/scan?sub=${imp.sub ? 1 : 0}&source=${enc(path)}`);
    } catch (e) {
      imp.source = '';
      drawSource();
      drawCenter();
      throw e;
    }
    if (!alive()) return;
    imp.selected = new Set(imp.src.Files.filter(f => !f.Dup).map(f => f.Rel));
    if (!imp.nameTouched) imp.name = imp.src.Name;
    if (imp.src.Zip && imp.mode === 'add') imp.mode = 'copy';
    if (path !== imp.lastSource) {
      imp.split = new Set(imp.src.Files.map(f => f.Group)).size > 1; // lab zips: one roll per folder
      // Suggested name and film per roll; the user can change any of them.
      imp.rolls = Object.fromEntries((imp.src.Groups || []).map(g => [g.Group, { name: '', nameTouched: false, Film: g.Film || '', ISO: g.ISO || '', Date: '' }]));
      nameRolls();
    }
    imp.lastSource = path;
    drawSource();
    drawCenter();
    drawDest();
  });

  const upload = guard(async files => {
    if (!files.length) return;
    const batch = Math.random().toString(36).slice(2, 14);
    const total = files.reduce((n, f) => n + f.size, 0);
    let sent = 0, res, zips = [];
    for (const f of files) {
      res = await xhrUpload(`/api/upload?batch=${batch}&name=${enc(f.name)}`, f, loaded => {
        imp.uploading = { name: files.length > 1 ? `${files.length} files` : f.name, pct: Math.round(100 * (sent + loaded) / Math.max(1, total)) };
        drawSource();
      });
      sent += f.size;
      if (/\.zip$/i.test(f.name)) zips.push(res.Path);
    }
    imp.uploading = null;
    if (zips.length) {
      imp.uploads = [...zips, ...imp.uploads.filter(u => !zips.includes(u))];
      await load(zips[0]);
      if (zips.length > 1) toast(`${zips.length} zips uploaded — import them one at a time from the list`);
    } else {
      await load(res.Dir);
    }
  });

  // ---------- center: frames ----------
  const thumbUrl = f => `/api/import/thumb?root=${enc(imp.src.Root)}&rel=${enc(f.Rel)}`;
  const drawCenter = () => {
    if (!imp.src) {
      $center.innerHTML = html`<div class="empty card">${icon('import', 'big')}<h2>Choose what to import</h2>
        <p>Pick a folder, memory card or the zip your lab sent. You can also drop files anywhere on this page.</p></div>`.s;
      return;
    }
    const files = imp.src.Files.filter(f => !(imp.hideDups && f.Dup));
    const dups = imp.src.Files.filter(f => f.Dup).length;
    const groups = new Map();
    files.forEach(f => groups.set(f.Group, [...(groups.get(f.Group) || []), f]));
    // Labs wrap rolls in an order folder; drop the shared prefix so labels read "Roll 1", not "Order 48213/Roll 1".
    const names = [...groups.keys()].filter(Boolean);
    let prefix = names.length > 1 ? names[0].split('/').slice(0, -1) : [];
    names.forEach(n => { const parts = n.split('/'); let i = 0; while (i < prefix.length && prefix[i] === parts[i]) i++; prefix = prefix.slice(0, i); });
    const label = g => g.split('/').slice(prefix.length).join('/') || g;
    if (!imp.src.Files.length) {
      $center.innerHTML = html`<div class="empty card">${icon('frames', 'big')}<h2>No photos found</h2><p>${imp.src.Zip ? 'That zip has no images in it.' : 'Try including sub-folders, or pick another folder.'}</p></div>`.s;
      return;
    }
    $center.innerHTML = html`<div class="toolbar imp-toolbar">
        <b data-count></b>
        <button class="btn sm" data-all>Select all</button><button class="btn sm ghost" data-none>None</button>
        ${dups ? html`<label class="switch"><input type="checkbox" data-hidedups ${imp.hideDups ? 'checked' : ''}><i></i><span class="muted">Hide ${plural(dups, 'duplicate')}</span></label>` : ''}
      </div>
      ${[...groups].map(([group, fs]) => html`<section class="imp-group">
        ${groups.size > 1 || group ? html`<label class="imp-group-head"><input type="checkbox" data-group="${group}"><b>${group ? label(group) : 'Top level'}</b><span class="muted">${plural(fs.length, 'photo')}</span></label>` : ''}
        <div class="cands">${fs.map(f => html`<label class="cand ${imp.selected.has(f.Rel) ? 'on' : ''} ${f.Dup ? 'dup' : ''}" title="${f.Rel}">
          <input type="checkbox" data-rel="${f.Rel}" ${imp.selected.has(f.Rel) ? 'checked' : ''}>
          <div class="img">${img(thumbUrl(f), f.Rel)}<i class="tick">${icon('check')}</i></div>
          <div class="n"><span>${f.Rel.split('/').pop()}</span>${f.Dup ? html`<span class="chip">In library</span>` : ''}</div>
        </label>`)}</div></section>`)}`.s;

    const sync = () => {
      $$('[data-rel]', $center).forEach(cb => {
        cb.checked = imp.selected.has(cb.dataset.rel);
        cb.closest('.cand').classList.toggle('on', cb.checked);
      });
      $$('[data-group]', $center).forEach(cb => {
        const members = imp.src.Files.filter(f => f.Group === cb.dataset.group);
        const n = members.filter(f => imp.selected.has(f.Rel)).length;
        cb.checked = n === members.length;
        cb.indeterminate = n > 0 && n < members.length;
      });
      $('[data-count]', $center).textContent = `${imp.selected.size} of ${plural(imp.src.Files.length, 'photo')} selected`;
      drawDest();
    };
    $center.onchange = e => {
      const t = e.target;
      if (t.dataset.rel !== undefined) {
        t.checked ? imp.selected.add(t.dataset.rel) : imp.selected.delete(t.dataset.rel);
      } else if (t.dataset.group !== undefined) {
        imp.src.Files.filter(f => f.Group === t.dataset.group).forEach(f => (t.checked ? imp.selected.add(f.Rel) : imp.selected.delete(f.Rel)));
      } else if (t.dataset.hidedups !== undefined) {
        imp.hideDups = t.checked;
        if (t.checked) imp.src.Files.filter(f => f.Dup).forEach(f => imp.selected.delete(f.Rel));
        return drawCenter();
      }
      sync();
    };
    $('[data-all]', $center).onclick = () => { files.forEach(f => imp.selected.add(f.Rel)); sync(); };
    $('[data-none]', $center).onclick = () => { imp.selected.clear(); sync(); };
    sync();
  };

  // ---------- destination ----------
  const groupInfo = group => (imp.src?.Groups || []).find(g => g.Group === group);
  const nameRolls = () => {
    const style = ROLL_STYLES.find(([id]) => id === imp.rollStyle) || ROLL_STYLES[0];
    for (const [group, r] of Object.entries(imp.rolls)) {
      const g = groupInfo(group);
      if (g && !r.nameTouched) r.name = style[2](g, r);
    }
  };
  const selectedGroups = () => (imp.src?.Groups || []).filter(g => imp.src.Files.some(f => f.Group === g.Group && imp.selected.has(f.Rel)));
  const multiGroup = () => imp.src && new Set(imp.src.Files.filter(f => imp.selected.has(f.Rel)).map(f => f.Group)).size > 1;
  const request = () => ({
    Source: imp.source, Subfolders: imp.sub, Files: [...imp.selected], Mode: imp.mode, Dest: imp.dest,
    Structure: imp.structure, Rename: imp.rename, Split: imp.split && multiGroup(), Name: imp.name,
    Rolls: imp.split && multiGroup() ? selectedGroups().map(g => ({ Group: g.Group, Name: imp.rolls[g.Group]?.name || '', Film: imp.rolls[g.Group]?.Film || '', ISO: imp.rolls[g.Group]?.ISO || '', Date: imp.rolls[g.Group]?.Date || '' })) : [],
    Film: imp.Film, ISO: imp.ISO, Make: imp.Make, Model: imp.Model, Lens: imp.Lens, Date: imp.Date, After: imp.after,
  });

  const drawDest = () => {
    const focused = document.activeElement?.name;
    const running = S.state.library.job.Running;
    const custom = !STRUCTURES.some(([v]) => v === imp.structure);
    const customRename = !RENAMES.some(([v]) => v === imp.rename);
    const splitting = imp.mode !== 'add' && imp.split && multiGroup();
    $dest.innerHTML = html`<form data-form>
      <h2>File handling</h2>
      <div class="seg full" role="radiogroup">${[['copy', 'Copy'], ['move', 'Move'], ['add', 'Add']].map(([v, l]) => html`
        <button type="button" role="radio" aria-checked="${imp.mode === v}" class="${imp.mode === v ? 'on' : ''}" data-mode="${v}" ${v !== 'copy' && imp.src?.Zip ? 'disabled' : ''}>${l}</button>`)}</div>
      <p class="hint">${{ copy: 'Copies photos into your library. The originals stay where they are.', move: 'Moves photos into your library and removes them from the source.', add: 'Tags photos where they are. The folder must already be inside a library.' }[imp.mode]}</p>

      ${imp.mode !== 'add' ? html`<h2>Destination</h2>
        ${s.libraries.length ? html`<label class="field"><span>Library</span><select class="input" name="dest">${s.libraries.map(l => html`<option ${l === imp.dest ? 'selected' : ''}>${l}</option>`)}</select></label>`
          : html`<div class="note warn">Add a library folder in <a href="#/settings" style="text-decoration:underline">Settings</a> first.</div>`}
        <label class="field"><span>Organise into</span><select class="input" name="structurePreset">
          ${STRUCTURES.map(([v, l]) => html`<option value="${v}" ${v === imp.structure ? 'selected' : ''}>${l}</option>`)}
          <option value="custom" ${custom ? 'selected' : ''}>Custom…</option></select></label>
        ${custom ? html`<label class="field"><span>Folder template</span><input class="input mono" name="structure" value="${imp.structure}"><small>{name} {film} {camera} {make} {model} {iso} {yyyy} {yy} {mm} {dd} {date} {source}</small></label>` : ''}
        ${multiGroup() ? html`<label class="switch"><input type="checkbox" name="split" ${imp.split ? 'checked' : ''}><i></i><span>One roll per sub-folder</span></label>` : ''}
        ${splitting ? html`<label class="field"><span>Name rolls by</span><select class="input" name="rollStyle">
            ${ROLL_STYLES.map(([id, label]) => html`<option value="${id}" ${id === imp.rollStyle ? 'selected' : ''}>${label}</option>`)}</select>
            <small>Pre-fills the names below. Anything you type yourself is kept.</small></label>
          <div class="imp-rolls">${selectedGroups().map(g => {
            const r = imp.rolls[g.Group] || {};
            const key = `r|${g.Group}|`;
            return html`<div class="imp-roll">
              <div class="imp-roll-head"><span class="mono" title="${g.Group}">${icon('folder')}${g.Folder}</span><span class="muted nowrap">${plural(g.Count, 'photo')}</span></div>
              <input class="input" name="${key}name" value="${r.name}" placeholder="${g.Name}" aria-label="Roll name for ${g.Folder}">
              <div class="imp-roll-meta">
                <div class="imp-roll-film"><input class="input" name="${key}Film" value="${r.Film}" placeholder="${imp.Film || 'Film stock'}" aria-label="Film for ${g.Folder}"></div>
                <input class="input" name="${key}ISO" value="${r.ISO}" placeholder="${imp.ISO || 'ISO'}" inputmode="numeric" aria-label="ISO for ${g.Folder}">
                <input class="input" type="date" name="${key}Date" value="${r.Date}" aria-label="Date shot for ${g.Folder}">
              </div></div>`;
          })}</div>`
        : html`<label class="field"><span>Roll name</span><input class="input" name="name" value="${imp.name}" placeholder="e.g. Lisbon trip"></label>`}
        <div class="plan" data-plan></div>

        <h2>File names</h2>
        <label class="field"><span>Rename</span><select class="input" name="renamePreset">
          ${RENAMES.map(([v, l]) => html`<option value="${v}" ${v === imp.rename ? 'selected' : ''}>${l}</option>`)}
          <option value="custom" ${customRename ? 'selected' : ''}>Custom…</option></select></label>
        ${customRename ? html`<label class="field"><span>Name template</span><input class="input mono" name="rename" value="${imp.rename}"><small>{original} {seq} plus any folder token</small></label>` : ''}` : ''}

      <h2>Apply metadata</h2>
      <label class="field"><span>${splitting ? 'Default film stock' : 'Film stock'}</span><input class="input" name="Film" value="${imp.Film}" placeholder="Search the film database">
        ${splitting ? html`<small>For rolls that don't have their own film above. ISO, date, camera and lens work the same way.</small>` : ''}</label>
      <div class="fields two">
        <label class="field"><span>ISO</span><input class="input" name="ISO" inputmode="numeric" value="${imp.ISO}"></label>
        <label class="field"><span>Date shot</span><input class="input" type="date" name="Date" value="${imp.Date}"></label>
        <label class="field"><span>Make</span><input class="input" name="Make" list="imp-makes" value="${imp.Make}" placeholder="Nikon"></label>
        <label class="field"><span>Model</span><input class="input" name="Model" list="imp-models" value="${imp.Model}" placeholder="FM2"></label>
      </div>
      <label class="field"><span>Lens</span><input class="input" name="Lens" list="imp-lenses" value="${imp.Lens}"></label>
      <datalist id="imp-makes">${g.makes.map(m => html`<option value="${m}">`)}</datalist>
      <datalist id="imp-models">${g.models.map(m => html`<option value="${m}">`)}</datalist>
      <datalist id="imp-lenses">${g.lenses.map(m => html`<option value="${m}">`)}</datalist>

      ${openApps.length ? html`<h2>After import</h2>
        <label class="field"><span>Open in</span><select class="input" name="after"><option value="">Nothing</option>
          ${openApps.map(a => html`<option value="${a.ID}" ${a.ID === imp.after ? 'selected' : ''}>${a.Name}</option>`)}
          <option value="folder" ${imp.after === 'folder' ? 'selected' : ''}>${S.state.os === 'darwin' ? 'Finder' : 'File Explorer'}</option></select></label>` : ''}

      <div class="imp-go"><button class="btn primary" data-go ${imp.selected.size && imp.source && !running && (imp.mode === 'add' || imp.dest) ? '' : 'disabled'}>
        ${icon('import')}${running ? 'Importing…' : `Import ${imp.selected.size ? plural(imp.selected.size, 'photo') : ''}`}</button></div>
    </form>`.s;

    const form = $('[data-form]', $dest);
    const refocus = focused && form.elements.namedItem(focused);
    if (refocus) {
      refocus.focus();
      if (refocus.setSelectionRange && refocus.type === 'text') refocus.setSelectionRange(refocus.value.length, refocus.value.length);
    }
    filmPicker(form.Film, f => { imp.Film = f.Name; if (f.ISO) { imp.ISO = f.ISO; form.ISO.value = f.ISO; } plan(); });
    $$('input[name$="|Film"]', form).forEach(input => {
      const group = input.name.split('|')[1];
      filmPicker(input, f => {
        imp.rolls[group].Film = f.Name;
        if (f.ISO) { imp.rolls[group].ISO = f.ISO; form[`r|${group}|ISO`].value = f.ISO; }
        plan();
      });
    });
    $$('[data-mode]', form).forEach(b => (b.onclick = () => { imp.mode = b.dataset.mode; drawDest(); }));
    form.onsubmit = e => e.preventDefault();
    form.oninput = e => {
      const t = e.target;
      if (t.name === 'structurePreset') { imp.structure = t.value === 'custom' ? imp.structure + ' ' : t.value; nameRolls(); return drawDest(); }
      if (t.name === 'renamePreset') { imp.rename = t.value === 'custom' ? '{original}' : t.value; return drawDest(); }
      if (t.name === 'split') { imp.split = t.checked; return drawDest(); }
      if (t.name === 'rollStyle') { imp.rollStyle = t.value; nameRolls(); return drawDest(); }
      if (t.name?.startsWith('r|')) {
        const [, group, field] = t.name.split('|');
        const r = imp.rolls[group];
        if (!r) return;
        r[field] = t.value;
        if (field === 'name') r.nameTouched = t.value.trim() !== '';
        if (field === 'Date' && !r.nameTouched) {
          nameRolls();
          form[`r|${group}|name`].value = r.name;
        }
        return plan();
      }
      if (t.name === 'name') imp.nameTouched = true;
      if (['dest', 'structure', 'rename', 'name', 'Film', 'ISO', 'Make', 'Model', 'Lens', 'Date', 'after'].includes(t.name)) imp[t.name] = t.value;
      plan();
    };
    $('[data-go]', form).onclick = go;
    plan();
  };

  const plan = debounce(async () => {
    const el = $('[data-plan]', $dest);
    if (!el || !imp.src || !imp.selected.size || imp.mode === 'add' || !imp.dest) { if (el) el.innerHTML = ''; return; }
    try {
      const rolls = await api('/api/import/plan', request());
      el.innerHTML = html`<div class="subhead" style="margin:4px 0 8px">Will create</div>${rolls.map(r => html`<div class="plan-row">${icon('folder')}<span class="mono">${r.Dir.slice(imp.dest.length + 1) || r.Dir}</span><span class="muted nowrap">${r.Count}</span></div>`)}`.s;
    } catch (e) {
      el.innerHTML = html`<p class="err" style="margin:0;font-size:13px">${e.message}</p>`.s;
    }
  }, 250);

  const go = guard(async () => {
    const req = request();
    await api('/api/import', req);
    // Remember choices for next time, like Lightroom does.
    api('/api/settings', { Libraries: s.libraries, ImportTo: imp.dest || s.importTo, UpdateHours: s.updateHours, Structure: imp.structure, Rename: imp.rename }).catch(() => {});
    await refreshState();
    drawJob();
    drawDest();
  });

  // ---------- job ----------
  const drawJob = () => {
    const job = S.state.library.job;
    const box = $('[data-job]', main);
    if (!box) return;
    if (!job.Kind || job.Kind !== 'import') { box.innerHTML = ''; return; }
    box.innerHTML = html`<div class="card section imp-job" aria-live="polite">
      <div class="row" style="justify-content:space-between"><b>${job.Message}</b><span class="mono muted">${job.Done} / ${job.Total}</span></div>
      <div class="progress" style="margin-top:10px"><i style="width:${Math.round(100 * job.Done / Math.max(1, job.Total))}%"></i></div>
      ${job.Error ? html`<p class="err" style="margin:10px 0 0">${job.Error}</p>` : ''}
      ${!job.Running && !job.Error && job.Dirs?.length ? html`<div class="row" style="margin-top:12px">${job.Dirs.slice(0, 6).map(d => html`<a class="btn sm" href="#/roll/${enc(d)}">${icon('frames')}${d.split('/').pop()}</a>`)}</div>` : ''}
    </div>`.s;
  };

  S.onState = ({ jobDone }) => {
    if (!S.state.library.job.Running && !jobDone) return;
    drawJob();
    if (jobDone) {
      drawDest();
      if (imp.source && !S.state.library.job.Error) load(imp.source).catch(() => {}); // refresh duplicate flags
    }
  };

  // Drop anywhere on the page to upload.
  const veil = $('.dropveil', main);
  let depth = 0;
  main.ondragenter = e => { if (e.dataTransfer?.types?.includes('Files')) { depth++; veil.hidden = false; } };
  main.ondragleave = () => { if (--depth <= 0) { depth = 0; veil.hidden = true; } };
  main.ondragover = e => e.preventDefault();
  main.ondrop = e => {
    e.preventDefault();
    depth = 0;
    veil.hidden = true;
    upload([...e.dataTransfer.files]);
  };

  drawSource();
  drawCenter();
  drawDest();
  drawJob();
};

function xhrUpload(url, file, onProgress) {
  return new Promise((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open('POST', url);
    x.upload.onprogress = e => onProgress(e.loaded);
    x.onload = () => {
      let data = {};
      try { data = JSON.parse(x.responseText); } catch { /* not JSON */ }
      x.status < 300 ? resolve(data) : reject(new Error(data.error || `Upload failed (${x.status})`));
    };
    x.onerror = () => reject(new Error('Upload failed — connection lost'));
    x.send(file);
  });
}

// Stop the desktop window navigating away when a file is dropped outside the import page.
addEventListener('dragover', e => e.preventDefault());
addEventListener('drop', e => e.preventDefault());
