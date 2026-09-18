'use strict';
// View: camera gear — bodies and lenses from the gear database, plus your own.

const gearFilter = { kind: 'body', q: '', brand: '', mount: '', format: '' };
const FILM_FORMATS = ['35mm', '120', '220', '110', '126', '127', 'APS', 'half-frame', 'sheet', 'instant'];
const gearYears = g => [g.Introduced, g.Mount && g.Mount.replace(/ lens mount$/i, ' mount')].filter(Boolean).join(' · ');
const lensSpec = g => [g.Focal && `${g.Focal}mm`, g.Aperture && `f/${g.Aperture}`].filter(Boolean).join(' ');
const gearSpec = g => (g.Kind === 'lens' ? [lensSpec(g), gearYears(g), g.Format].filter(Boolean).join(' · ')
  : [gearYears(g), g.Format].filter(Boolean).join(' · '));

const gearCard = g => html`<button class="film gear-card" data-slug="${g.Slug}">
  <div class="box">${g.Image ? img(g.Image, g.Name) : html`<div class="none">${icon(g.Kind === 'lens' ? 'lens' : 'camera')}</div>`}
    ${g.Custom ? html`<span class="chip accent badge">Yours</span>` : ''}
    ${g.Rolls ? html`<span class="chip badge" style="left:8px;right:auto;top:auto;bottom:8px">${plural(g.Rolls, 'roll')}</span>` : ''}</div>
  <div class="info"><b title="${g.Name}">${g.Name}</b><small><span>${gearSpec(g) || '—'}</span></small></div>
</button>`;

views.gear = async (main, _, __, alive) => {
  let facets = { brands: [], mounts: [], formats: [] };
  const draw = async () => {
    const { kind, q, brand, mount, format } = gearFilter;
    const [items, f] = await Promise.all([
      api(`/api/gear?kind=${kind}&q=${enc(q)}&brand=${enc(brand)}&mount=${enc(mount)}&format=${enc(format)}&limit=600`),
      api(`/api/gear/facets?kind=${kind}`).catch(() => facets),
    ]);
    if (!alive()) return;
    facets = f;
    const yours = items.filter(g => g.Custom).length;
    main.innerHTML = html`${pageHead('Gear', 'Cameras and lenses', html`
        <div class="search">${icon('search')}<input class="input" type="search" placeholder="Search gear" aria-label="Search gear" value="${gearFilter.q}" data-q></div>
        <button class="btn primary" data-add>${icon('plus')}Add gear</button>`)}
      <div class="toolbar">
        <div class="seg" role="tablist">
          <button role="tab" data-kind="body" class="${gearFilter.kind === 'body' ? 'on' : ''}">Cameras</button>
          <button role="tab" data-kind="lens" class="${gearFilter.kind === 'lens' ? 'on' : ''}">Lenses</button>
        </div>
        ${[['brand', 'All brands', facets.brands], ['mount', 'All mounts', facets.mounts], ['format', 'All film', facets.formats]]
          .map(([key, label, values]) => html`<select class="input pick" data-f="${key}" aria-label="${label}">
            <option value="">${label}</option>
            ${(values || []).map(v => html`<option ${v === gearFilter[key] ? 'selected' : ''}>${v}</option>`)}</select>`)}
        ${brand || mount || format ? html`<button class="btn sm ghost" data-clear>${icon('x')}Clear</button>` : ''}
        <span class="muted">${plural(items.length, 'item')}${yours ? ` · ${yours} yours` : ''}</span>
        <span class="muted" style="margin-left:auto">${S.state.geardb?.status || ''}</span>
      </div>
      ${items.length ? html`<div class="films">${items.map(gearCard)}</div>`
        : html`<div class="empty card">${icon('camera', 'big')}<h2>${q || brand || mount || format ? 'Nothing found' : 'No gear yet'}</h2>
            <p>${S.state.geardb?.status?.includes('…') ? 'The gear database is still downloading.' : 'Add your camera or lens and it shows up here.'}</p>
            <button class="btn primary" data-add2>${icon('plus')}Add gear</button></div>`}`.s;
    searchBox(main, v => (gearFilter.q = v), draw, 200);
    $$('[data-kind]', main).forEach(b => (b.onclick = () => {
      Object.assign(gearFilter, { kind: b.dataset.kind, brand: '', mount: '', format: '' }); // a lens mount is not a camera mount
      draw();
    }));
    $$('[data-f]', main).forEach(s => (s.onchange = () => { gearFilter[s.dataset.f] = s.value; draw(); }));
    $('[data-clear]', main)?.addEventListener('click', () => {
      Object.assign(gearFilter, { brand: '', mount: '', format: '' });
      draw();
    });
    $$('[data-add], [data-add2]', main).forEach(b => (b.onclick = () => editGear({ Kind: gearFilter.kind }, draw)));
    $$('[data-slug]', main).forEach(b => (b.onclick = () => editGear(items.find(g => g.Slug === b.dataset.slug), draw)));
  };
  S.onState = () => { if (/downloading|Up to date/i.test(S.state.geardb?.status || '') && !$('.overlay')) draw(); };
  await draw();
};

// editGear edits your own gear, or makes your own version of a database entry.
function editGear(item, done) {
  const g = { Kind: 'body', ...item };
  const isNew = !g.Slug;
  const { box, close } = layer(html`<header><h2>${isNew ? 'Add gear' : g.Custom ? 'Edit gear' : 'Your version of this'}</h2>
      <button class="btn ghost icon" data-close aria-label="Close">${icon('x')}</button></header>
    <form class="body" id="gearform">
      ${!isNew && !g.Custom ? html`<div class="note">This comes from the gear database. Saving keeps your version instead, everywhere it's used.</div>` : ''}
      <div class="seg full" role="radiogroup">${[['body', 'Camera'], ['lens', 'Lens']].map(([v, l]) => html`
        <button type="button" role="radio" aria-checked="${g.Kind === v}" class="${g.Kind === v ? 'on' : ''}" data-kind="${v}" ${isNew ? '' : 'disabled'}>${l}</button>`)}</div>
      <label class="field"><span>Name</span><input class="input" name="Name" value="${g.Name || ''}" placeholder="${g.Kind === 'lens' ? 'Nikkor 50mm f/1.4' : 'Nikon FM2'}" required autofocus>
        <small>Written to your photos as make and model, split at the first space.</small></label>
      <div class="fields two">
        <label class="field"><span>Mount</span><input class="input" name="Mount" value="${g.Mount || ''}" placeholder="Canon FD"></label>
        <label class="field"><span>Film</span><select class="input" name="Format">
          ${FILM_FORMATS.map(f => html`<option ${f === (g.Format || '35mm') ? 'selected' : ''}>${f}</option>`)}</select></label>
      </div>
      <div class="fields two">
        <label class="field"><span>${g.Kind === 'lens' ? 'Focal length (mm)' : 'Type'}</span>
          ${g.Kind === 'lens' ? html`<input class="input" name="Focal" value="${g.Focal || ''}" placeholder="50 or 28-70">`
            : html`<select class="input" name="Type"><option value="">—</option>${['SLR', 'DSLR', 'Rangefinder', 'Point and shoot', 'Medium format'].map(t => html`<option ${t === g.Type ? 'selected' : ''}>${t}</option>`)}</select>`}</label>
        <label class="field"><span>${g.Kind === 'lens' ? 'Max aperture' : 'Introduced'}</span>
          ${g.Kind === 'lens' ? html`<input class="input" name="Aperture" value="${g.Aperture || ''}" placeholder="1.4">`
            : html`<input class="input" name="Introduced" value="${g.Introduced || ''}" placeholder="1982">`}</label>
      </div>
      ${g.Kind === 'lens' ? html`<div class="fields two">
        <label class="field"><span>Introduced</span><input class="input" name="Introduced" value="${g.Introduced || ''}" placeholder="1971"></label>
        <label class="field"><span>Filter (mm)</span><input class="input" name="Filter" value="${g.Filter || ''}" placeholder="55"></label></div>` : ''}
      <div class="field"><span>Photo</span>
        <div class="gear-photo">${g.Image ? img(g.Image, g.Name) : html`<div class="none">${icon(g.Kind === 'lens' ? 'lens' : 'camera')}</div>`}
          <label class="btn sm">${icon('import')}Choose photo<input type="file" accept="image/jpeg,image/png,image/webp" hidden data-photo></label></div>
        ${isNew ? html`<small class="faint">You can add a photo once it's saved.</small>` : ''}</div>
    </form>
    <footer>${!isNew && g.Custom ? html`<button class="btn danger" data-forget>${icon('trash')}${item.Custom && item.Slug ? 'Remove' : ''}</button>` : ''}
      <span style="flex:1"></span><button class="btn" data-close>Cancel</button><button class="btn primary" form="gearform">${icon('check')}Save</button></footer>`, 'drawer');

  const form = $('form', box);
  $$('[data-kind]', box).forEach(b => (b.onclick = () => { close(); editGear({ ...g, Kind: b.dataset.kind, Name: form.Name.value }, done); }));
  $('[data-photo]', box).onchange = guard(async e => {
    const file = e.target.files[0];
    if (!file) return;
    if (isNew) return toast('Save it first, then add a photo', true);
    await xhrUpload(`/api/gear/photo?slug=${enc(g.Slug)}&name=${enc(file.name)}`, file, () => {});
    toast('Photo saved');
    close();
    done?.();
  });
  $('[data-forget]', box)?.addEventListener('click', guard(async () => {
    if (!confirm(`Remove ${g.Name} from your gear?`)) return;
    await api('/api/gear/delete', { Slug: g.Slug });
    close();
    toast('Removed');
    done?.();
  }));
  form.onsubmit = guard(async e => {
    e.preventDefault();
    const data = Object.fromEntries(new FormData(form));
    const saved = await api('/api/gear', { ...g, ...data, Rolls: 0 });
    close();
    toast(isNew ? 'Added to your gear' : 'Saved');
    done?.(saved);
  });
}

// gearPicker turns a text input into a gear search that can also add what you type.
function gearPicker(input, kind, onPick) {
  const wrap = input.parentElement;
  wrap.classList.add('ac');
  input.setAttribute('autocomplete', 'off');
  const list = document.createElement('div');
  list.className = 'ac-list';
  list.hidden = true;
  wrap.append(list);
  let items = [], sel = -1;
  const show = () => {
    const typed = input.value.trim();
    const exact = items.some(g => g.Name.toLowerCase() === typed.toLowerCase());
    list.hidden = !items.length && !typed;
    list.innerHTML = html`${items.map((g, i) => html`<div class="ac-item ${i === sel ? 'on' : ''}" data-i="${i}">
        <div class="thumb">${g.Image ? img(g.Image) : icon(kind === 'lens' ? 'lens' : 'camera')}</div>
        <div><b>${g.Name}</b><small>${[gearSpec(g), g.Custom ? 'yours' : '', g.Rolls ? `shot ${plural(g.Rolls, 'roll')}` : ''].filter(Boolean).join(' · ')}</small></div></div>`)}
      ${typed && !exact ? html`<div class="ac-item add" data-add><div class="thumb">${icon('plus')}</div>
        <div><b>Add “${typed}”</b><small>to your gear</small></div></div>` : ''}`.s;
    $$('.ac-item', list).forEach(el => (el.onmousedown = e => {
      e.preventDefault();
      if (el.dataset.add !== undefined) return add(typed);
      pick(items[+el.dataset.i]);
    }));
  };
  const pick = g => {
    input.value = g.Name;
    items = [];
    list.hidden = true; // picking closes the list; don't offer to add what was just picked
    onPick?.(g);
  };
  const add = name => editGear({ Kind: kind, Name: name }, saved => saved && pick(saved));
  const search = debounce(async () => {
    try {
      items = await api(`/api/gear?kind=${kind}&limit=8&q=${enc(input.value.trim())}`);
    } catch {
      items = [];
    }
    sel = -1;
    show();
  }, 140);
  input.addEventListener('input', search);
  input.addEventListener('focus', search);
  input.addEventListener('blur', () => setTimeout(() => { items = []; list.hidden = true; }, 150));
  input.addEventListener('keydown', e => {
    if (list.hidden) return;
    const n = items.length + (list.querySelector('[data-add]') ? 1 : 0);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      sel = (sel + (e.key === 'ArrowDown' ? 1 : -1) + n) % n;
      show();
    } else if (e.key === 'Enter' && sel >= 0) {
      e.preventDefault();
      sel < items.length ? pick(items[sel]) : add(input.value.trim());
    } else if (e.key === 'Escape') {
      items = [];
      list.hidden = true;
    }
  });
}
