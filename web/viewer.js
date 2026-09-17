'use strict';
// Full-screen photo viewer: zoom and pan, rotate (and save it to the file), info panel, filmstrip.

const viewerPrefs = (() => { try { return JSON.parse(localStorage.getItem('emulsion.viewer')) || {}; } catch { return {}; } })();
const saveViewerPrefs = () => { try { localStorage.setItem('emulsion.viewer', JSON.stringify(viewerPrefs)); } catch { /* storage unavailable */ } };
const ORIENT_DEG = { 1: 0, 2: 0, 3: 180, 4: 180, 5: 270, 6: 90, 7: 90, 8: 270 };
const photoFilm = p => (Array.isArray(p.Subject) ? p.Subject : [p.Subject]).map(String).find(s => s.startsWith('film:'))?.slice(5) || p.FilmStock || '';
const fileSize = n => (n >= 1e9 ? `${(n / 1e9).toFixed(2)} GB` : n >= 1e6 ? `${(n / 1e6).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1e3))} KB`);

function lightbox(frames, index, { roll } = {}) {
  viewerPrefs.info ??= innerWidth > 1100;
  const prevFocus = document.activeElement;
  const v = document.createElement('div');
  v.className = 'viewer';
  v.setAttribute('role', 'dialog');
  v.setAttribute('aria-modal', 'true');
  v.setAttribute('aria-label', 'Photo viewer');
  const tool = (act, ic, label) => html`<button class="v-btn" data-act="${act}" title="${label}" aria-label="${label}">${icon(ic)}</button>`;
  v.innerHTML = html`
    <header class="v-top">
      <span class="v-count"></span>
      <span class="v-name"></span>
      <div class="v-tools">
        ${tool('zoom-out', 'minus', 'Zoom out (−)')}
        <button class="v-btn v-zoom" data-act="fit" title="Fit to window (0)" aria-label="Fit to window"></button>
        ${tool('zoom-in', 'plus', 'Zoom in (+)')}
        <button class="v-btn v-text" data-act="actual" title="Actual pixels (1)">1:1</button>
        <span class="v-sep"></span>
        ${tool('rot-left', 'rotl', 'Rotate left (Shift+R)')}
        ${tool('rot-right', 'rotr', 'Rotate right (R)')}
        <button class="btn sm primary v-save" data-act="save-rot" hidden>${icon('check')}Save rotation</button>
        <span class="v-sep"></span>
        ${tool('info', 'info', 'Info (I)')}
        <a class="v-btn v-orig" target="_blank" rel="noopener" title="Open original" aria-label="Open original">${icon('external')}</a>
        ${tool('close', 'x', 'Close (Esc)')}
      </div>
    </header>
    <div class="v-main">
      <div class="v-stage">
        <img class="v-img" alt="" draggable="false">
        <div class="v-loading" hidden><span class="spinner"></span>Loading full resolution…</div>
        <button class="v-nav prev" data-act="prev" aria-label="Previous (←)">${icon('left')}</button>
        <button class="v-nav next" data-act="next" aria-label="Next (→)">${icon('right')}</button>
      </div>
      <aside class="v-info" aria-label="Photo info"></aside>
    </div>
    <div class="v-strip" role="listbox" aria-label="Frames">${frames.map((p, i) => html`<button class="v-thumb" role="option" data-i="${i}" aria-label="Frame ${i + 1}">${img(thumb(p.SourceFile, false, p.Orientation), '')}</button>`)}</div>`.s;
  document.body.append(v);
  document.body.style.overflow = 'hidden';

  const stage = $('.v-stage', v), im = $('.v-img', v), info = $('.v-info', v), strip = $('.v-strip', v);
  let s = 1, tx = 0, ty = 0, rot = 0, w = 0, h = 0, fullLoaded = false, fullTimer, token = 0, fitted = true;
  const p = () => frames[index];
  const upright = q => (ORIENT_DEG[q.Orientation] % 180 ? [q.ImageHeight, q.ImageWidth] : [q.ImageWidth, q.ImageHeight]);
  const rotated = () => (rot % 180 ? [h, w] : [w, h]);
  const fitScale = () => {
    const [rw, rh] = rotated();
    return rw && rh ? Math.min(stage.clientWidth / rw, stage.clientHeight / rh, 2) : 1;
  };
  const clamp = n => Math.min(Math.max(n, Math.min(fitScale(), 0.05)), 8);
  const atFit = () => fitted; // fit to the window; stays true across resizes until you zoom or pan

  const apply = () => {
    im.style.width = `${w}px`;
    im.style.height = `${h}px`;
    im.style.transform = `translate(calc(-50% + ${tx}px), calc(-50% + ${ty}px)) rotate(${rot}deg) scale(${s})`;
    $('.v-zoom', v).textContent = `${Math.round(s * 100)}%`;
    v.classList.toggle('zoomed', !atFit());
    $('.v-save', v).hidden = rot % 360 === 0;
    clearTimeout(fullTimer);
    fullTimer = setTimeout(loadFull, 180);
  };
  const fit = () => { s = fitScale(); tx = ty = 0; fitted = true; apply(); };
  const zoomAt = (ns, cx, cy) => {
    ns = clamp(ns);
    const r = stage.getBoundingClientRect();
    const px = (cx ?? r.left + r.width / 2) - (r.left + r.width / 2);
    const py = (cy ?? r.top + r.height / 2) - (r.top + r.height / 2);
    tx = px - (ns / s) * (px - tx);
    ty = py - (ns / s) * (py - ty);
    s = ns;
    if (s <= fitScale() + 0.001) return fit();
    fitted = false;
    apply();
  };

  // The 2000px preview is swapped for full resolution once you zoom past it.
  const loadFull = () => {
    const q = p();
    if (fullLoaded || !w || s * Math.max(w, h) <= (im.naturalWidth ? Math.max(im.naturalWidth, im.naturalHeight) : 2000) * 1.1) return;
    fullLoaded = true;
    const my = token;
    const src = /\.(jpe?g|png|webp)$/i.test(q.SourceFile) ? `/api/photo?f=${enc(q.SourceFile)}` : thumb(q.SourceFile, 'f', q.Orientation);
    $('.v-loading', v).hidden = false;
    const hi = new Image();
    hi.onload = () => { if (my === token) { im.src = src; $('.v-loading', v).hidden = true; } };
    hi.onerror = () => { $('.v-loading', v).hidden = true; };
    hi.src = src;
  };

  const show = () => {
    const q = p();
    token++;
    rot = 0;
    fullLoaded = false;
    [w, h] = upright(q);
    $('.v-count', v).textContent = `${String(index + 1).padStart(2, '0')} / ${String(frames.length).padStart(2, '0')}`;
    $('.v-name', v).textContent = q.SourceFile.split('/').pop();
    $('.v-orig', v).href = `/api/photo?f=${enc(q.SourceFile)}`;
    $('.v-loading', v).hidden = true;
    const my = token;
    im.onload = () => {
      if (my !== token || w) return;
      [w, h] = [im.naturalWidth, im.naturalHeight]; // no dimensions in the metadata: trust the preview
      fit();
    };
    im.src = thumb(q.SourceFile, true, q.Orientation);
    if (w) fit();
    $$('.v-thumb', strip).forEach((b, i) => b.setAttribute('aria-selected', String(i === index)));
    $(`.v-thumb[data-i="${index}"]`, strip)?.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
    [index - 1, index + 1].forEach(i => { if (frames[i]) new Image().src = thumb(frames[i].SourceFile, true, frames[i].Orientation); });
    drawInfo();
  };
  const go = d => { index = (index + d + frames.length) % frames.length; show(); };

  // ---------- info ----------
  const drawInfo = () => {
    v.classList.toggle('with-info', !!viewerPrefs.info);
    $('[data-act="info"]', v).setAttribute('aria-pressed', String(!!viewerPrefs.info));
    if (!viewerPrefs.info) return;
    const q = p();
    const my = token;
    const [iw, ih] = upright(q);
    const film = photoFilm(q);
    const row = (k, val) => (val ? html`<dt>${k}</dt><dd>${val}</dd>` : '');
    const folder = q.SourceFile.split('/').slice(0, -1).join('/');
    info.innerHTML = html`
      <h3>${q.SourceFile.split('/').pop()}</h3>
      <p class="muted">Frame ${index + 1} of ${frames.length}${roll ? html` · <a href="#/roll/${enc(roll.Dir)}" data-act="close">${roll.Name}</a>` : ''}${q.Export ? ' · Positive' : ''}</p>
      <dl class="v-facts">
        ${row('Film', film ? html`<a data-film="${film}" href="#">${film}</a>` : '')}
        ${row('Camera', [q.Make, q.Model].filter(Boolean).join(' '))}
        ${row('Lens', q.LensModel)}
        ${row('ISO', q.ISO)}
        ${row('Shot', q.DateTimeOriginal && !q.DateTimeOriginal.startsWith('0000') ? fmtDate(q.DateTimeOriginal.slice(0, 10)) : '')}
        ${row('Size', iw ? `${iw.toLocaleString()} × ${ih.toLocaleString()} · ${((iw * ih) / 1e6).toFixed(1)} MP` : '')}
        ${row('File', [q.SourceFile.split('.').pop().toUpperCase(), q.FileSize ? fileSize(q.FileSize) : ''].filter(Boolean).join(' · '))}
        ${row('Rotation', ORIENT_DEG[q.Orientation] ? `${ORIENT_DEG[q.Orientation]}° (saved)` : '')}
        ${row('Folder', html`<span class="mono">${folder}</span>`)}
      </dl>
      <div class="v-open" data-open></div>
      <details class="v-all"><summary>All metadata</summary><div data-all class="muted">Loading…</div></details>`.s;
    $('[data-film]', info)?.addEventListener('click', guard(async e => {
      e.preventDefault();
      const f = await api('/api/film?name=' + enc(e.currentTarget.dataset.film));
      close();
      location.hash = `#/film/${f.line}`;
    }));
    $('details', info).ontoggle = guard(async e => {
      if (!e.target.open || e.target.dataset.loaded) return;
      e.target.dataset.loaded = '1';
      const meta = await api('/api/photo/meta?f=' + enc(q.SourceFile));
      const groups = new Map();
      for (const [k, val] of Object.entries(meta)) {
        if (k === 'SourceFile' || String(val).startsWith('(Binary data')) continue;
        const [g, tag] = k.includes(':') ? k.split(/:(.*)/s) : ['Other', k];
        groups.set(g, [...(groups.get(g) || []), [tag, Array.isArray(val) ? val.join(', ') : val]]);
      }
      $('[data-all]', info).className = '';
      $('[data-all]', info).innerHTML = html`${[...groups].map(([g, tags]) => html`<div class="v-group">${g}</div>
        <dl class="v-facts v-meta">${tags.map(([t, val]) => html`<dt title="${t}">${t}</dt><dd>${String(val)}</dd>`)}</dl>`)}`.s;
    });
    if (S.state.local) {
      api('/api/apps').then(apps => {
        const box = $('[data-open]', info);
        if (!box || my !== token) return;
        const found = apps.filter(a => a.Found && a.TakesFiles);
        box.innerHTML = html`${found.map(a => html`<button class="btn sm" data-app="${a.ID}">${icon('external')}${a.Name}</button>`)}
          <button class="btn sm" data-app="folder">${icon('folder')}${S.state.os === 'darwin' ? 'Show in Finder' : 'Show in folder'}</button>`.s;
        $$('[data-app]', box).forEach(b => (b.onclick = guard(() => api('/api/open', { App: b.dataset.app, Dir: roll?.Dir || folder, Files: [q.SourceFile] }))));
      }).catch(() => {});
    }
  };

  // ---------- actions ----------
  const saveRotation = guard(async () => {
    if (rot % 360 === 0) return;
    const btn = $('.v-save', v);
    btn.disabled = true;
    try {
      const updated = await api('/api/photo/rotate', { File: p().SourceFile, Degrees: rot });
      frames[index] = { ...p(), ...updated, Export: p().Export };
      S.rolls = null;
      $(`.v-thumb[data-i="${index}"] img`, strip).src = thumb(updated.SourceFile, false, updated.Orientation);
      toast(isRawFile(updated.SourceFile) ? 'Rotation saved to the XMP sidecar' : 'Rotation saved to the photo');
      v.dataset.changed = '1';
      show();
    } finally {
      btn.disabled = false;
    }
  });
  const act = name => ({
    'zoom-in': () => zoomAt(s * 1.25),
    'zoom-out': () => zoomAt(s / 1.25),
    fit,
    actual: () => zoomAt(1),
    'rot-left': () => { rot -= 90; fit(); },
    'rot-right': () => { rot += 90; fit(); },
    'save-rot': saveRotation,
    info: () => { viewerPrefs.info = !viewerPrefs.info; saveViewerPrefs(); drawInfo(); },
    prev: () => go(-1),
    next: () => go(1),
    close,
  })[name]?.();
  v.addEventListener('click', e => {
    const b = e.target.closest('[data-act]');
    if (b) act(b.dataset.act);
    const t = e.target.closest('.v-thumb');
    if (t) { index = +t.dataset.i; show(); }
  });

  // ---------- zoom & pan ----------
  stage.addEventListener('wheel', e => {
    e.preventDefault();
    zoomAt(s * Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.0015)), e.clientX, e.clientY);
  }, { passive: false });
  stage.addEventListener('dblclick', e => {
    if (e.target.closest('.v-nav')) return;
    atFit() ? zoomAt(Math.max(1, fitScale() * 2.5), e.clientX, e.clientY) : fit();
  });
  const pointers = new Map();
  let pinch = null, swipeStart = null;
  stage.addEventListener('pointerdown', e => {
    if (e.target.closest('.v-nav') || e.button !== 0) return;
    stage.setPointerCapture(e.pointerId);
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      pinch = { d: Math.hypot(a.x - b.x, a.y - b.y), s };
    } else {
      swipeStart = { x: e.clientX, y: e.clientY, fit: atFit() };
    }
  });
  stage.addEventListener('pointermove', e => {
    const prev = pointers.get(e.pointerId);
    if (!prev) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pinch && pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      zoomAt(pinch.s * Math.hypot(a.x - b.x, a.y - b.y) / pinch.d, (a.x + b.x) / 2, (a.y + b.y) / 2);
    } else if (!atFit()) {
      tx += e.clientX - prev.x;
      ty += e.clientY - prev.y;
      fitted = false;
      stage.classList.add('panning');
      apply();
    }
  });
  const release = e => {
    pointers.delete(e.pointerId);
    if (pointers.size < 2) pinch = null;
    stage.classList.remove('panning');
    // Swipe between frames when not zoomed in (touch screens).
    if (swipeStart?.fit && e.pointerType !== 'mouse' && Math.abs(e.clientX - swipeStart.x) > 60 && Math.abs(e.clientY - swipeStart.y) < 80) {
      go(e.clientX < swipeStart.x ? 1 : -1);
    }
    swipeStart = null;
  };
  stage.addEventListener('pointerup', release);
  stage.addEventListener('pointercancel', release);

  const onKey = e => {
    if (e.target.closest?.('input, textarea, select')) return;
    const k = e.key;
    if (k === 'Escape') close();
    else if (k === 'ArrowRight') go(1);
    else if (k === 'ArrowLeft') go(-1);
    else if (k === '+' || k === '=') act('zoom-in');
    else if (k === '-' || k === '_') act('zoom-out');
    else if (k === '0') fit();
    else if (k === '1') act('actual');
    else if (k.toLowerCase() === 'r' && !e.ctrlKey && !e.metaKey) act(e.shiftKey ? 'rot-left' : 'rot-right');
    else if (k.toLowerCase() === 'i') act('info');
    else if (k.toLowerCase() === 's' && (e.ctrlKey || e.metaKey)) saveRotation();
    else return;
    e.preventDefault();
  };
  // Refit when the stage changes size: window resizes, rotating a phone, or opening the info panel.
  const resizer = new ResizeObserver(() => { if (fitted) fit(); });
  resizer.observe(stage);
  addEventListener('keydown', onKey);

  function close() {
    if (rot % 360 && !confirm('Discard the unsaved rotation?')) return;
    v.remove();
    document.body.style.overflow = '';
    removeEventListener('keydown', onKey);
    resizer.disconnect();
    prevFocus?.focus?.();
    if (v.dataset.changed) route(); // show rotated thumbnails
  }

  show();
  $('[data-act="close"]', v).focus();
}
