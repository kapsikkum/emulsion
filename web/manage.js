'use strict';
// View: settings.

views.settings = async (main, _, __, alive) => {
  await refreshState();
  const apps = S.state.local ? await api('/api/apps').catch(() => []) : [];
  if (!alive()) return;
  const st = S.state, s = st.settings, w = s.webui, db = st.filmdb, hf = s.hotFolder || {};
  const save = guard(async (patch, msg) => {
    await api('/api/settings', { Libraries: s.libraries, ImportTo: s.importTo, UpdateHours: s.updateHours, ...patch });
    await refreshState();
    if (msg) toast(msg);
    route();
  });
  const dep = (ok, name, cmd) => html`<dt>${name}</dt><dd>${ok ? html`<span class="ok">${icon('check')} Installed</span>`
    : html`<span class="err">Missing</span> — install with <code>${cmd}</code>, then restart Emulsion`}</dd>`;
  const fm = st.os === 'darwin' ? 'Finder' : 'File Explorer';

  main.innerHTML = html`${pageHead('Settings')}
    <div class="settings">
      ${!st.deps.git || !st.deps.exiftool ? html`<div class="note warn">${icon('alert')} Emulsion needs Git and ExifTool. See System below.</div>` : ''}

      <section class="card">
        <div class="section">
          <h2>Libraries</h2><p class="muted">Folders scanned for rolls. Each sub-folder with images is one roll.</p>
          <div class="list">${s.libraries.map(l => html`<div class="item">${icon('folder', 'lead')}<span class="path">${l}</span>
            ${l === s.importTo ? html`<span class="chip accent">Import target</span>` : html`<button class="btn sm ghost" data-target="${l}">Import here</button>`}
            <button class="btn sm ghost icon" data-remove="${l}" aria-label="Remove ${l}" title="Remove from Emulsion (files stay)">${icon('trash')}</button></div>`)}
          </div>
          <div class="row" style="margin-top:12px"><button class="btn" data-add>${icon('plus')}Add folder</button><button class="btn ghost" data-rescan>${icon('refresh')}Rescan now</button><span class="muted">${st.library.status}</span></div>
        </div>
      </section>

      <section class="card">
        <form class="section" data-organise>
          <h2>Importing</h2><p class="muted">Defaults for the Import page and the hot folder. The Import page remembers what you used last.</p>
          <div class="stack">
            <label class="field"><span>Folder structure</span><input class="input mono" name="structure" value="${s.structure}" required>
              <small>{name} {film} {camera} {make} {model} {iso} {yyyy} {yy} {mm} {dd} {date} {source} · e.g. <code>{yyyy}/{date} {name}</code></small></label>
            <label class="field"><span>Rename files</span><input class="input mono" name="rename" value="${s.rename}" placeholder="Keep original names">
              <small>{original} {seq} plus the tokens above · e.g. <code>{name}_{seq}</code></small></label>
            <label class="field"><span>Converted positives folders</span><input class="input mono" name="exportDirs" value="${(s.exportDirs || []).join(', ')}">
              <small>Sub-folders of a roll holding inverted scans, such as NegPy exports. They show as the roll’s positives instead of separate rolls.</small></label>
            <div class="row"><button class="btn primary">${icon('check')}Save</button></div>
          </div>
        </form>
        <form class="section" data-hot>
          <h2>Hot folder</h2><p class="muted">Drop lab zips or scan folders here and Emulsion files them into <b>${s.importTo || 'your import library'}</b> using the folder structure above, about a minute after they finish downloading. Imported originals move to an <code>Imported</code> sub-folder.</p>
          <div class="stack">
            <label class="switch"><input type="checkbox" name="enabled" ${hf.enabled ? 'checked' : ''}><i></i><span>Watch a folder</span></label>
            <div class="picked">${icon('folder')}<span class="path">${hf.path || 'No folder chosen'}</span><button type="button" class="btn sm" data-hotpick>Choose…</button></div>
            <input type="hidden" name="path" value="${hf.path || ''}">
            <div class="row"><button class="btn primary">${icon('check')}Save hot folder</button></div>
          </div>
        </form>
      </section>

      ${st.local ? html`<section class="card">
        <form class="section" data-apps>
          <h2>Integrations</h2><p class="muted">Apps in the “Open in” menu on rolls and after an import. Emulsion finds the usual installs. Paste a different path to override.</p>
          <div class="list">${apps.filter(a => !a.ID.startsWith('editor:')).map(a => html`<div class="item app-row">
            <span class="dot ${a.Found ? 'ok' : ''}" aria-hidden="true"></span>
            <div style="flex:1;min-width:0"><b>${a.Name}</b> <span class="muted">${a.Found ? 'found' : 'not found'}</span>
              <input class="input mono sm" name="app:${a.ID}" value="${(s.apps || {})[a.ID] || ''}" placeholder="${a.Path || 'Path to the app'}">
              ${a.Note ? html`<small class="faint">${a.Note}</small>` : ''}</div></div>`)}</div>
          <div class="subhead" style="margin:22px 0 10px">Other editors</div>
          <div class="list" data-editors>${(s.editors || []).map(e => html`<div class="item editor-row">
            <input class="input sm" data-ename value="${e.name}" placeholder="Name" style="max-width:180px">
            <input class="input mono sm" data-epath value="${e.path}" placeholder="C:\\Program Files\\…\\app.exe" style="flex:1">
            <button type="button" class="btn sm ghost icon" data-edel aria-label="Remove editor">${icon('trash')}</button></div>`)}</div>
          <div class="row" style="margin-top:12px"><button type="button" class="btn" data-eadd>${icon('plus')}Add editor</button><button class="btn primary">${icon('check')}Save integrations</button></div>
          <div class="note" style="margin-top:18px"><b>Working with NegPy</b><br>
            In NegPy’s export settings choose <i>sub-folder of source</i> and name it <code>${(s.exportDirs || ['export'])[0]}</code>, and turn on copying metadata. Your positives then appear inside their roll here, with the film and camera Emulsion wrote into the scans. Film stocks you set in NegPy’s metadata panel show up in Emulsion too.</div>
        </form>
      </section>` : ''}

      <section class="card">
        <div class="section">
          <h2>Gear database</h2><p class="muted">Cameras and lenses from the
            <a href="https://github.com/kapsikkum/camera-gear-database" target="_blank" rel="noopener" style="text-decoration:underline">camera gear database</a>,
            with pictures. Gear you add yourself is kept separately and is never touched by an update.</p>
          <dl class="kv"><dt>Status</dt><dd>${st.geardb?.status || '—'}</dd><dt>Models</dt><dd>${(st.geardb?.count || 0).toLocaleString()}</dd>
            <dt>Last checked</dt><dd>${ago(st.geardb?.updated)}</dd></dl>
          <div class="row" style="margin-top:14px"><button class="btn" data-gearupdate>${icon('refresh')}Check for updates</button>
            <a class="btn ghost" href="#/gear">${icon('camera')}Your gear</a></div>
        </div>
      </section>

      <section class="card">
        <div class="section">
          <h2>Film database</h2><p class="muted">A local copy of the <a href="https://github.com/dxdatabase/Open-source-film-database" target="_blank" rel="noopener" style="text-decoration:underline">Open Source Film Database</a> (CC BY-SA 4.0). Your edits are kept as local changes and win if the same film changes upstream.</p>
          <dl class="kv"><dt>Status</dt><dd data-dbstatus>${db.status || '—'}</dd><dt>Last checked</dt><dd>${ago(db.updated)}</dd>
            <dt>Check every</dt><dd><select class="input" style="width:auto;height:32px" data-hours>${[6, 12, 24, 72, 168].map(h => html`<option value="${h}" ${h === s.updateHours ? 'selected' : ''}>${h < 24 ? `${h} hours` : h === 24 ? 'day' : h === 168 ? 'week' : `${h / 24} days`}</option>`)}</select></dd></dl>
          <div class="row" style="margin-top:14px"><button class="btn" data-update>${icon('refresh')}Check for updates</button></div>
        </div>
        <div class="section">
          <h2>Your edits · ${db.edits.length}</h2>
          ${db.edits.length ? html`<div class="list">${db.edits.map(e => html`<div class="item"><span class="mono faint">${e.Hash}</span><span style="flex:1">${e.Message}</span><span class="muted nowrap">${e.Date}</span>
              <button class="btn sm ghost" data-revert="${e.Hash}" title="Undo this edit">${icon('undo')}Revert</button></div>`)}</div>`
            : html`<p class="muted" style="margin:0">No local edits. Edit or add films from the Films page.</p>`}
        </div>
      </section>

      ${st.desktop ? html`<section class="card"><form class="section" data-remote>
          <h2>Remote access</h2><p class="muted">Open this library from other devices, like a phone or laptop, while Emulsion is running here. Same idea as qBittorrent’s Web UI.</p>
          <div class="stack">
            <label class="switch"><input type="checkbox" name="enabled" ${w.enabled ? 'checked' : ''}><i></i><span>Enable web UI</span></label>
            <div class="fields">
              <label class="field"><span>Listen address</span><input class="input mono" name="address" value="${w.address}" required><small><code>0.0.0.0:8080</code> = all networks · <code>127.0.0.1:8080</code> = this PC only</small></label>
              <label class="field"><span>${w.hasPassword ? 'New password' : 'Password'}</span><input class="input" type="password" name="password" minlength="8" autocomplete="new-password" placeholder="${w.hasPassword ? 'Leave blank to keep current' : 'At least 8 characters'}"></label>
            </div>
            ${w.error ? html`<div class="note warn">Couldn’t start: ${w.error}</div>` : ''}
            ${w.running ? html`<div class="note"><b class="ok">Running.</b> Open ${w.urls.map((u, i) => html`${i ? ', ' : ''}<a class="mono" style="text-decoration:underline" href="${u}" target="_blank" rel="noopener">${u}</a>`)}</div>` : ''}
            <p class="faint" style="margin:0;font-size:12.5px">Anyone who can reach this address and knows the password can browse and edit your library. To use it away from home, use a VPN such as Tailscale or an HTTPS reverse proxy. Don’t forward the port directly.</p>
            <div class="row"><button class="btn primary">${icon('check')}Save remote access</button></div>
          </div>
        </form></section>`
      : !st.local && w.hasPassword ? html`<section class="card"><div class="section"><h2>Session</h2><p class="muted">You’re signed in remotely.</p><button class="btn" data-logout>${icon('logout')}Sign out</button></div></section>` : ''}

      <section class="card" id="updates"><div class="section" data-updates><h2>Updates</h2><p class="muted">Checking…</p></div></section>

      <section class="card"><div class="section">
        <h2>System</h2>
        <dl class="kv">
          ${dep(st.deps.git, 'Git', st.os === 'windows' ? 'winget install Git.Git' : 'apt install git')}
          ${dep(st.deps.exiftool, 'ExifTool', st.os === 'windows' ? 'winget install OliverBetz.ExifTool' : 'apt install libimage-exiftool-perl')}
          <dt>Data folder</dt><dd class="mono" style="overflow-wrap:anywhere">${st.dataDir}</dd></dl>
      </div></section>
    </div>`.s;

  $('[data-add]', main).onclick = guard(async () => {
    const p = await pickFolder('Add a library folder');
    if (p) save({ Libraries: [...s.libraries, p], ImportTo: s.importTo || p }, 'Folder added — scanning');
  });
  $$('[data-target]', main).forEach(b => (b.onclick = () => save({ ImportTo: b.dataset.target }, 'Import target updated')));
  $$('[data-remove]', main).forEach(b => (b.onclick = () => {
    if (confirm(`Stop showing rolls from\n${b.dataset.remove}?\n\nNo files are deleted.`)) save({ Libraries: s.libraries.filter(l => l !== b.dataset.remove) }, 'Folder removed');
  }));
  $('[data-rescan]', main).onclick = guard(async () => { await api('/api/rescan', {}); toast('Rescanning…'); });

  const organise = $('[data-organise]', main);
  organise.onsubmit = e => {
    e.preventDefault();
    save({
      Structure: organise.structure.value.trim(),
      Rename: organise.rename.value.trim(),
      ExportDirs: organise.exportDirs.value.split(',').map(x => x.trim()).filter(Boolean),
    }, 'Import settings saved');
  };

  const hot = $('[data-hot]', main);
  $('[data-hotpick]', hot).onclick = guard(async () => {
    const p = await pickFolder('Choose the hot folder', hf.path || '');
    if (p) {
      hot.path.value = p;
      $('.picked .path', hot).textContent = p;
    }
  });
  hot.onsubmit = e => {
    e.preventDefault();
    save({ HotFolder: { enabled: hot.enabled.checked, path: hot.path.value } }, hot.enabled.checked ? 'Watching the hot folder' : 'Hot folder off');
  };

  const appsForm = $('[data-apps]', main);
  if (appsForm) {
    const editorRow = () => html`<div class="item editor-row">
      <input class="input sm" data-ename placeholder="Name" style="max-width:180px">
      <input class="input mono sm" data-epath placeholder="Path to the app" style="flex:1">
      <button type="button" class="btn sm ghost icon" data-edel aria-label="Remove editor">${icon('trash')}</button></div>`;
    $('[data-eadd]', appsForm).onclick = () => {
      $('[data-editors]', appsForm).insertAdjacentHTML('beforeend', editorRow().s);
      $$('[data-ename]', appsForm).pop().focus();
    };
    appsForm.onclick = e => { if (e.target.closest('[data-edel]')) e.target.closest('.editor-row').remove(); };
    appsForm.onsubmit = e => {
      e.preventDefault();
      const Apps = {};
      $$('input[name^="app:"]', appsForm).forEach(i => { if (i.value.trim()) Apps[i.name.slice(4)] = i.value.trim().replace(/^"|"$/g, ''); });
      const Editors = $$('.editor-row', appsForm)
        .map(r => ({ name: $('[data-ename]', r).value.trim(), path: $('[data-epath]', r).value.trim().replace(/^"|"$/g, '') }))
        .filter(x => x.name && x.path);
      save({ Apps, Editors }, 'Integrations saved');
    };
  }

  $('[data-hours]', main).onchange = e => save({ UpdateHours: +e.target.value }, 'Saved');
  $('[data-update]', main).onclick = guard(async () => { await api('/api/filmdb/update', {}); toast('Checking for film database updates…'); });
  $('[data-gearupdate]', main).onclick = guard(async () => { await api('/api/geardb/update', {}); toast('Checking for gear database updates…'); });
  $$('[data-revert]', main).forEach(b => (b.onclick = guard(async () => {
    if (!confirm('Undo this edit? The film goes back to its previous version.')) return;
    await api('/api/filmdb/revert', { Hash: b.dataset.revert });
    toast('Edit reverted');
    route();
  })));
  $('[data-logout]', main)?.addEventListener('click', guard(async () => { await api('/api/logout', {}); location.reload(); }));
  const remote = $('[data-remote]', main);
  if (remote) remote.onsubmit = e => {
    e.preventDefault();
    save({ WebUI: { Enabled: remote.enabled.checked, Address: remote.address.value.trim(), Password: remote.password.value } }, 'Remote access saved');
  };
  // ---------- updates ----------
  const upBox = $('[data-updates]', main);
  let upTimer;
  const drawUpdates = u => {
    const auto = st.settings.autoUpdateCheck;
    upBox.innerHTML = html`<h2>Updates</h2>
      <dl class="kv"><dt>Installed</dt><dd class="mono">${u.Current}</dd>
        <dt>Latest release</dt><dd class="mono">${u.Latest || '—'}${u.URL ? html` <a class="muted" href="${u.URL}" target="_blank" rel="noopener" style="text-decoration:underline;font-family:var(--sans)">release notes</a>` : ''}</dd>
        <dt>Last checked</dt><dd>${u.Checking ? 'checking…' : ago(u.Checked)}</dd></dl>
      ${u.Error ? html`<div class="note warn" style="margin-top:14px">${u.Error}</div>` : ''}
      ${u.Available ? html`<div class="note update-note" style="margin-top:14px"><b>Emulsion ${u.Latest} is available.</b>
          ${u.Notes ? html`<div class="notes">${u.Notes}</div>` : ''}
          ${u.Installing ? html`<div class="row" style="justify-content:space-between;margin-top:12px"><span>${u.Progress < 100 ? 'Downloading…' : 'Installing…'}</span><span class="mono">${u.Progress}%</span></div><div class="progress" style="margin-top:6px"><i style="width:${u.Progress}%"></i></div>`
            : u.CanInstall ? '' : html`<p style="margin:10px 0 0">${u.Reason}</p>`}</div>`
        : u.Latest && !u.Checking ? html`<p class="ok" style="margin:14px 0 0">${icon('check')} You're on the latest version.</p>` : ''}
      <div class="row" style="margin-top:16px">
        ${u.Available && u.CanInstall ? html`<button class="btn primary" data-install ${u.Installing ? 'disabled' : ''}>${icon('import')}Install and restart</button>` : ''}
        <button class="btn" data-check ${u.Checking || u.Installing ? 'disabled' : ''}>${icon('refresh')}Check now</button>
        <label class="switch" style="margin-left:auto"><input type="checkbox" data-autocheck ${auto ? 'checked' : ''}><i></i><span class="muted">Check daily</span></label>
      </div>`.s;
    $('[data-check]', upBox).onclick = guard(async () => { drawUpdates({ ...u, Checking: true }); drawUpdates(await api('/api/update/check', {})); await refreshState(); });
    $('[data-autocheck]', upBox).onchange = e => save({ AutoUpdateCheck: e.target.checked }, e.target.checked ? 'Emulsion will check for updates daily' : 'Automatic update checks off');
    $('[data-install]', upBox)?.addEventListener('click', guard(async () => {
      if (!confirm(`Install Emulsion ${u.Latest} and restart? The current version is kept as a backup.`)) return;
      await api('/api/update/install', {});
      watchInstall();
    }));
  };
  // Poll while installing; when the server restarts on the new version, reload the page.
  const watchInstall = async () => {
    clearTimeout(upTimer);
    let u;
    try {
      u = await api('/api/update');
    } catch {
      return restarting();
    }
    if (!alive()) return;
    drawUpdates(u);
    if (u.Installing) upTimer = setTimeout(watchInstall, 700);
  };
  const restarting = () => {
    const from = st.version;
    document.body.insertAdjacentHTML('beforeend', html`<div class="restart-veil">${LOGO}<b>Restarting Emulsion…</b><span>This page reloads when the new version is up.</span></div>`.s);
    const wait = async () => {
      try {
        const s = await fetch('/api/state').then(r => r.json());
        if (s.version && s.version !== from) return location.reload();
      } catch { /* still restarting */ }
      setTimeout(wait, 1000);
    };
    setTimeout(wait, 1500);
  };
  api('/api/update').then(u => {
    if (!alive()) return;
    drawUpdates(u);
    if (u.Installing) watchInstall();
    else if (!u.Checked || new Date(u.Checked).getFullYear() < 2000) $('[data-check]', upBox)?.click();
  }).catch(e => { upBox.innerHTML = html`<h2>Updates</h2><p class="err">${e.message}</p>`.s; });
  // After the router restores scroll position for this page.
  if (location.hash === '#/settings/updates') setTimeout(() => $('#updates', main)?.scrollIntoView({ block: 'start', behavior: 'smooth' }), 120);

  S.onState = () => {
    const el = $('[data-dbstatus]', main);
    if (el && el.textContent !== (S.state.filmdb.status || '—')) el.textContent = S.state.filmdb.status || '—';
  };
};
