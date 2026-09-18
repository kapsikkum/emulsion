'use strict';
// Views: film catalogue and film detail/edit.

const catFilter = { tab: null, q: '', pic: false };
// The database's "Ava" column.
const AVAIL = { 2: ['On the market', 'current'], 1: ['Maybe discontinued', 'unsure'], 0: ['Discontinued', 'gone'] };
const availChip = a => (a === 2 ? html`<span class="avail current" title="Still being made">${icon('check')}In production</span>` : '');
const years = f => (f.Begin && f.End ? `${f.Begin} – ${f.End}` : f.Begin ? `${f.Begin} –` : f.End ? `– ${f.End}` : '');
const initials = name => name.split(/\s+/).slice(0, 2).map(w => w[0]).join('').toUpperCase();

const filmCard = f => html`<a class="film" href="#/film/${f.Line}">
  <div class="box">${f.Pic ? img(filmImg(f.Pic), f.Name) : html`<div class="none">${initials(f.Name)}</div>`}
    ${f.Rolls ? html`<span class="chip accent badge">${plural(f.Rolls, 'roll')}</span>` : ''}
    ${f.Avail === 2 ? html`<span class="avail-dot" title="In production"></span>` : ''}</div>
  <div class="info"><b title="${f.Name}">${f.Name}</b>
    <small><span>${f.Maker || '—'}</span>${f.ISO ? html`<span class="iso">${f.ISO}</span>` : ''}</small>
    ${years(f) ? html`<small class="faint"><span>${years(f)}</span></small>` : ''}</div>
</a>`;

views.films = async (main, _, __, alive) => {
  const mine = await api('/api/films?shot=1&limit=1').catch(e => ({ error: e.message }));
  if (!alive()) return;
  if (mine.error) {
    main.innerHTML = html`${pageHead('Films')}<div class="empty card">${icon('database', 'big')}<h2>Film database not ready</h2><p>${S.state.filmdb.status || mine.error}</p>
      <button class="btn primary" data-update>${icon('refresh')}Download now</button></div>`.s;
    $('[data-update]', main).onclick = guard(async () => { await api('/api/filmdb/update', {}); toast('Downloading film database…'); });
    S.onState = () => { if (S.state.filmdb.status === 'Up to date') route(); };
    return;
  }
  catFilter.tab ??= mine.total ? 'mine' : 'current';
  main.innerHTML = html`${pageHead('Films', 'Open Source Film Database', html`
      <div class="search">${icon('search')}<input class="input" type="search" placeholder="Search 8,000+ films" aria-label="Search films" value="${catFilter.q}" data-q></div>
      <a class="btn primary" href="#/film/new">${icon('plus')}Add film</a>`)}
    <div class="toolbar">
      <div class="seg" role="tablist">
        <button role="tab" data-tab="mine" class="${catFilter.tab === 'mine' ? 'on' : ''}">Your stocks${mine.total ? ` · ${mine.total}` : ''}</button>
        <button role="tab" data-tab="current" class="${catFilter.tab === 'current' ? 'on' : ''}">In production</button>
        <button role="tab" data-tab="all" class="${catFilter.tab === 'all' ? 'on' : ''}">Full catalogue</button>
      </div>
      <label class="switch"><input type="checkbox" data-pic ${catFilter.pic ? 'checked' : ''}><i></i><span class="muted">With box art only</span></label>
      <span class="muted" data-count></span>
    </div>
    <div class="films" data-list></div><div class="more"><button class="btn" data-more hidden>Load more</button></div>`.s;

  const list = $('[data-list]', main), more = $('[data-more]', main);
  let offset = 0, seq = 0;
  const load = guard(async reset => {
    const my = ++seq;
    if (reset) { offset = 0; list.innerHTML = ''; }
    const q = new URLSearchParams({ q: catFilter.q, limit: 60, offset, shot: catFilter.tab === 'mine' ? 1 : 0, current: catFilter.tab === 'current' ? 1 : 0, pic: catFilter.pic ? 1 : 0 });
    const res = await api('/api/films?' + q);
    if (my !== seq || !alive()) return;
    offset += res.items.length;
    list.insertAdjacentHTML('beforeend', html`${res.items.map(filmCard)}`.s);
    $('[data-count]', main).textContent = plural(res.total, 'film');
    more.hidden = offset >= res.total;
    if (!res.total) {
      list.innerHTML = html`<div class="empty" style="grid-column:1/-1">${icon('film', 'big')}
        <h2>${catFilter.tab === 'mine' && !catFilter.q ? 'No stocks tagged yet' : 'No films found'}</h2>
        <p>${catFilter.tab === 'mine' && !catFilter.q ? 'Set the film on a roll and it shows up here.' : 'Try another search, or add the film yourself.'}</p></div>`.s;
    }
  });
  $$('[data-tab]', main).forEach(b => (b.onclick = () => {
    catFilter.tab = b.dataset.tab;
    $$('[data-tab]', main).forEach(x => x.classList.toggle('on', x === b));
    load(true);
  }));
  searchBox(main, v => (catFilter.q = v), () => load(true), 180);
  $('[data-pic]', main).onchange = e => { catFilter.pic = e.target.checked; load(true); };
  more.onclick = () => load(false);
  await load(true);
};

const LABELS = {
  'DX extract': 'DX number', 'DX full': 'DX full code', Name: 'Name', 'Original Film or informations': 'Notes / original film',
  Manufacturer: 'Manufacturer', reliability: 'Reliability', Country: 'Country', 'Beginning year': 'Introduced',
  'End year': 'Discontinued', Distributor: 'Distributor', Ava: 'Availability', Pic: 'Box image file',
};
const label = h => LABELS[h] || h;

views.film = async (main, [id], _, alive) => {
  const isNew = id === 'new';
  const f = await api('/api/film?line=' + (isNew ? 0 : +id));
  if (!alive()) return;
  const [name, maker, info, pic] = [f.fields[2], f.fields[4], f.fields[3], f.fields[11]];

  const showEdit = () => {
    main.innerHTML = html`<div style="padding-top:22px"><a class="back" href="${isNew ? '#/films' : `#/film/${f.line}`}">${icon('left')}${isNew ? 'Films' : name}</a></div>
      ${pageHead(isNew ? 'Add film' : 'Edit film', 'Saved to your local copy · kept when the database updates')}
      <form class="card section stack" style="max-width:860px">
        <div class="fields">${f.header.map((h, i) => i === 3
          ? html`<label class="field wide"><span>${label(h)}</span><textarea class="input" name="f${i}">${f.fields[i]}</textarea></label>`
          : i === 10
          ? html`<label class="field"><span>${label(h)}</span><select class="input" name="f${i}">${[2, 1, 0].map(n => html`<option value="${n}" ${String(n) === (f.fields[i] || '0').trim() ? 'selected' : ''}>${AVAIL[n][0]}</option>`)}</select></label>`
          : html`<label class="field ${i === 2 ? 'wide' : ''}"><span>${label(h)}</span><input class="input" name="f${i}" value="${f.fields[i]}" ${i === 2 ? 'required autofocus' : ''}></label>`)}</div>
        <div class="row"><button class="btn primary">${icon('check')}${isNew ? 'Add film' : 'Save changes'}</button><a class="btn ghost" href="${isNew ? '#/films' : `#/film/${f.line}`}">Cancel</a></div>
      </form>`.s;
    const form = $('form', main);
    form.onsubmit = guard(async e => {
      e.preventDefault();
      const fields = f.header.map((_, i) => form[`f${i}`].value.replace(/\s*[\r\n]+\s*/g, ' '));
      if (fields.some(v => v.includes(';'))) throw new Error('Fields can’t contain semicolons');
      const res = await api('/api/film', { Line: f.line, Orig: f.orig, Fields: fields });
      toast(isNew ? 'Film added to your database' : 'Film saved');
      if (location.hash === `#/film/${res.line}`) route(); else location.hash = `#/film/${res.line}`;
    });
  };
  if (isNew) return showEdit();

  const specs = f.header.map((h, i) => [h, f.fields[i]]).filter(([h, v], i) => v && ![2, 3, 4, 10, 11].includes(i))
    .map(([h, v]) => [label(h), v]);
  if (AVAIL[f.avail]) specs.unshift(['Availability', AVAIL[f.avail][0]]);
  if (f.iso) specs.unshift(['ISO', f.iso]);
  main.innerHTML = html`<div style="padding-top:22px"><a class="back" href="#/films">${icon('left')}Films</a></div>
    <div class="film-page" style="padding-top:12px">
      <div class="art">${pic ? html`<img alt="${name}" src="${filmImg(pic)}">` : html`<div class="none">${initials(name)}</div>`}</div>
      <div>
        <div class="maker">${maker || 'Unknown manufacturer'} ${availChip(f.avail)}</div>
        <h1>${name}</h1>
        ${info ? html`<p class="desc">${info}</p>` : ''}
        ${specs.length ? html`<div class="specs">${specs.map(([k, v]) => html`<div><small>${k}</small><b>${v}</b></div>`)}</div>` : html`<div style="height:24px"></div>`}
        <div class="row">
          <a class="btn primary" href="#/import?film=${enc(name)}">${icon('import')}Import a roll on this film</a>
          <button class="btn" data-edit>${icon('edit')}Edit details</button>
        </div>
        <div class="subhead">Your rolls · ${f.rolls.length}</div>
        ${f.rolls.length ? html`<div class="rolls">${f.rolls.map(rollCard)}</div>` : html`<p class="muted">You haven’t tagged any rolls with this film yet.</p>`}
      </div>
    </div>`.s;
  $('[data-edit]', main).onclick = showEdit;
};
