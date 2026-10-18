(() => {
  const pickers = [...document.querySelectorAll('[data-live-picker]')];
  if (!pickers.length) return;
  const labels = new Map();
  const turns = new Map();
  const name = (kind, id) => labels.get(kind + ':' + id) || `Selected ${kind}: ${id}`;
  const row = (kind, id, label, checked) => {
    const node = document.createElement('label'), input = document.createElement('input'), text = document.createElement('span');
    input.type = 'checkbox'; input.name = `live_${kind}_ids`; input.value = id; input.checked = checked;
    text.textContent = label; node.append(input, text); return node;
  };
  for (const picker of pickers) {
    const kind = picker.dataset.livePicker;
    for (const input of picker.querySelectorAll('input[type=checkbox]')) labels.set(kind + ':' + input.value, input.nextElementSibling.textContent);
    const clear = picker.querySelector('[data-live-clear]'); clear.hidden = false;
    clear.onclick = () => { picker.querySelectorAll('input[type=checkbox]').forEach(input => { input.checked = false; }); };
    const search = picker.querySelector('[data-live-search]');
    if (!search) continue;
    const keyword = picker.querySelector('[data-live-keyword]');
    keyword.addEventListener('keydown', event => { if (event.key === 'Enter') { event.preventDefault(); search.click(); } });
    search.addEventListener('click', async event => {
      event.preventDefault();
      const turn = (turns.get(kind) || 0) + 1; turns.set(kind, turn);
      const status = picker.querySelector('[data-live-picker-status]'), results = picker.querySelector('[data-live-results]');
      // Retain checked results when searching again; only Apply changes the feed.
      const selected = picker.querySelector('[data-live-selected]');
      results.querySelectorAll('input:checked').forEach(input => selected.append(input.parentElement)); results.replaceChildren();
      status.textContent = 'Searching…'; picker.setAttribute('aria-busy', 'true');
      try {
        const response = await fetch(`/api/v1/${kind === 'artist' ? 'artists' : 'venues'}?` + new URLSearchParams({ keyword: keyword.value }), { cache: 'no-store', signal: AbortSignal.timeout(10000) });
        if (!response.ok) throw new Error(response.status === 401 ? 'Sign in again to search. Your confirmed filters are unchanged.' : 'Search could not load. Try again; your selections are retained.');
        const data = await response.json();
        if (turns.get(kind) !== turn) return;
        const ids = new Set([...selected.querySelectorAll('input')].map(input => input.value));
        for (const item of data.items) {
          labels.set(kind + ':' + item.id, item.name);
          if (ids.has(item.id)) continue;
          const place = item.location?.city ? ` · ${item.location.city}, ${item.location.country_code || ''}` : '';
          results.append(row(kind, item.id, item.name + place, false));
        }
        status.textContent = data.items.length ? 'Select matches, then Apply. Refine the name if your choice is missing.' : 'No matches. Try another name.';
      } catch (error) { if (turns.get(kind) === turn) status.textContent = error.message; }
      finally { if (turns.get(kind) === turn) picker.removeAttribute('aria-busy'); }
    });
  }
  window.ticketopiaLivePickers = {
    name,
    get: () => Object.fromEntries(pickers.map(picker => [picker.dataset.livePicker + '_ids', [...new Set([...picker.querySelectorAll('input[type=checkbox]:checked')].map(input => input.value))].sort()])),
    set: (filters, authenticated) => {
      for (const picker of pickers) {
        const kind = picker.dataset.livePicker, turn = (turns.get(kind) || 0) + 1; turns.set(kind, turn);
        picker.removeAttribute('aria-busy');
        picker.querySelector('[data-live-results]')?.replaceChildren();
        const selected = picker.querySelector('[data-live-selected]'); selected.replaceChildren();
        const status = picker.querySelector('[data-live-picker-status]'); if (status) status.textContent = '';
        for (const id of filters[kind + '_ids'] || []) {
          const node = row(kind, id, name(kind, id), true); selected.append(node);
          if (!authenticated || labels.has(kind + ':' + id)) continue;
          fetch(`/api/v1/${kind === 'artist' ? 'artists' : 'venues'}/` + encodeURIComponent(id), { cache: 'no-store', signal: AbortSignal.timeout(10000) })
            .then(async response => { if (!response.ok) throw new Error(); return response.json(); })
            .then(data => { if (turns.get(kind) !== turn) return; labels.set(kind + ':' + id, data.item.name); node.querySelector('span').textContent = data.item.name; document.dispatchEvent(new Event('ticketopia:live-labels')); })
            .catch(() => { if (turns.get(kind) === turn && status) status.textContent = 'Some names could not load. Selected IDs are retained; reopen the page to retry.'; });
        }
      }
    }
  };
})();
