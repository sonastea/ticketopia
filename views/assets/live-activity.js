(() => {
  'use strict';
  const root = document.querySelector('[data-live-activity]');
  if (!root) return;
  const node = name => root.querySelector(`[data-live-${name}]`);
  const details = node('details'), list = node('list'), updates = node('updates');
  const onPage = !!node('page'), toasts = node('toasts');
  const text = (name, value) => { if (node(name)) node(name).textContent = value; };
  const pageFocus = () => node('heading')?.focus({ preventScroll: true });
  let priorFocus;
  document.addEventListener('focusin', event => { if (!toasts.contains(event.target)) priorFocus = event.target; });
  const removeToast = row => {
    const focused = row.contains(document.activeElement);
    row.remove();
    if (!onPage && !toasts.children.length) {
      clearTimeout(announceTimer);
      text('status', '');
    }
    if (focused) {
      if (priorFocus?.isConnected && priorFocus.getClientRects().length && !priorFocus.disabled) priorFocus.focus({ preventScroll: true });
      // Happening Now is a Community tab, so fall back to Community navigation.
      else [...document.querySelectorAll('a[href="/happening-now"], nav a[href="/community"]')].find(link => link.getClientRects().length)?.focus({ preventScroll: true });
    }
  };
  const compact = matchMedia('(max-width: 61.999rem)');
  const header = document.querySelector('.compact-header');
  if (header) new ResizeObserver(() => {
    toasts.style.setProperty('--activity-header-edge', Math.max(0, header.getBoundingClientRect().bottom) + 'px');
  }).observe(header);
  const hoverable = matchMedia('(hover: hover) and (pointer: fine)');
  const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
  const toastBlocked = () => {
    if (document.querySelector('dialog[open], [aria-modal="true"]')) return true;
    const viewport = window.visualViewport;
    if (viewport && viewport.height < innerHeight * .75) return true;
    const rect = toasts.getBoundingClientRect();
    const focus = document.activeElement;
    if (focus && focus !== document.body && !toasts.contains(focus) && overlaps(rect, focus.getBoundingClientRect())) return true;
    const nav = document.querySelector('.bottom-nav');
    return rect.bottom > innerHeight - 12 || (nav?.getClientRects().length && overlaps(rect, nav.getBoundingClientRect()));
  };
  const reconcileToasts = () => {
    for (const row of [...toasts.children]) {
      const item = current.find(item => item.event_id === row.dataset.id);
      if (!item || Date.parse(item.expires_at) <= now()) removeToast(row);
      else fillRow(row, item);
    }
  };
  const showToasts = fresh => {
    if (onPage || hidden || paused) return;
    for (const item of fresh) {
      if ([...toasts.children].some(row => row.dataset.id === item.event_id)) continue;
      if (toasts.children.length >= (compact.matches ? 1 : 3)) break;
      const row = makeRow(item.event_id); row.className = 'activity-toast'; row.remaining = 8000;
      const actions = document.createElement('div'); actions.className = 'account-actions';
      const browse = document.createElement('a'); browse.href = '/happening-now'; browse.className = 'text-link'; browse.textContent = 'Happening Now';
      const dismiss = document.createElement('button'); dismiss.type = 'button'; dismiss.className = 'text-link'; dismiss.textContent = 'Dismiss'; dismiss.setAttribute('aria-label', 'Dismiss recommendation for ' + item.event_title);
      dismiss.onclick = () => removeToast(row);
      actions.append(browse, dismiss); row.append(actions); toasts.append(row); fillRow(row, item);
      // Coalesce overflow rather than queueing a backlog behind the current task.
      if (toastBlocked()) removeToast(row);
    }
    if (toasts.children.length) watchToasts();
  };
  // Collision and countdown checks read layout, so they run only while a toast
  // is shown and stop once the last one is removed.
  let toastTick = 0, toastTimer = 0;
  const tickToasts = () => {
    const at = performance.now(), elapsed = at - toastTick; toastTick = at;
    while (toasts.children.length > (compact.matches ? 1 : 3)) removeToast(toasts.lastElementChild);
    const blocked = toasts.children.length && toastBlocked();
    if (blocked) [...toasts.children].forEach(removeToast);
    for (const row of [...toasts.children]) {
      if (!document.hidden && !blocked && !(hoverable.matches && row.matches(':hover')) && !row.contains(document.activeElement)) row.remaining -= elapsed;
      if (row.remaining <= 0 || !current.some(item => item.event_id === row.dataset.id && Date.parse(item.expires_at) > now())) removeToast(row);
    }
    if (!toasts.children.length) { clearInterval(toastTimer); toastTimer = 0; }
  };
  const watchToasts = () => {
    if (toastTimer) return;
    toastTick = performance.now();
    toastTimer = setInterval(tickToasts, 200);
  };
  let hidden = false, paused = true, ready = false, stream, generation = 0, countsGeneration = 0;
  let reconnectTimer, debounceTimer, expiryTimer, announceTimer, lastMessage = 0, backoff = 5000, idsKey = '';
  let current = [], shown = [], baseline = true, serverAt = 0, receivedAt = 0, writes = 0;
  let authenticated = false, revision = 0, preferenceScope = '';
  const defaults = () => ({ location: { mode: 'all' }, category_ids: [], artist_ids: [], venue_ids: [], connections: { mode: 'all_events', sources: [] } });
  const storageKey = 'ticketopia-live-filters-v1';
  let filters = defaults(), guestFilters = defaults(), enabled = true, catalog = [], saving = false;
  try { const value = JSON.parse(localStorage.getItem(storageKey)); if (value && JSON.stringify(value).length <= 4096 && value.location && Array.isArray(value.category_ids)) guestFilters = value; } catch {}
  const publicFilter = value => ({ location: value.location, category_ids: value.category_ids, artist_ids: value.artist_ids || [], venue_ids: value.venue_ids || [] });
  const restricted = () => filters.location.mode !== 'all' || filters.category_ids.length || filters.artist_ids?.length || filters.venue_ids?.length || filters.connections.mode === 'any';
  const activityURL = () => authenticated ? '/api/v1/me/community/activity' : '/api/v1/community/activity';
  const activityQuery = values => { const q = query(values); if (authenticated) q.set('preferences_revision', revision); else q.set('filter', JSON.stringify(publicFilter(filters))); return q; };
  const seen = new Map(), pending = new Set();
  try { hidden = localStorage.getItem('ticketopia-live-hidden') === 'true'; } catch { /* Page memory remains usable. */ }
  const eventTargets = '[data-event-id], [data-interest-count], [data-event-recommendations], [data-live-total]';
  const ids = () => [...new Set([...document.querySelectorAll(eventTargets)]
    .filter(element => !element.closest('[data-live-list], [data-live-toasts]'))
    .map(element => element.dataset.eventId || element.dataset.interestCount || element.dataset.eventRecommendations || element.dataset.liveTotal))].sort();
  const query = values => { const q = new URLSearchParams(); values.forEach(id => q.append('event_id', id)); return q; };
  const allowed = () => ready && (onPage || !hidden) && !paused && !document.hidden && navigator.onLine && !writes;
  const now = () => serverAt + performance.now() - receivedAt;
  const clear = () => {
    if (list?.contains(document.activeElement)) pageFocus();
    current = []; shown = []; pending.clear(); list?.replaceChildren();
    [...toasts.children].forEach(removeToast);
    text('badge', ''); if (updates) updates.hidden = true; text('status', '');
    if (node('empty-reset')) node('empty-reset').hidden = true;
    if (node('empty-community')) node('empty-community').hidden = true;
    if (node('empty-state')) node('empty-state').hidden = true;
    clearTimeout(expiryTimer);
    clearTimeout(announceTimer);
  };
  const stop = () => { ++generation; stream?.close(); stream = null; clearTimeout(reconnectTimer); reconnectTimer = undefined; clear(); baseline = true; };
  const controls = () => {
    if (!onPage) return;
    details.hidden = paused;
    node('show').hidden = !hidden; node('hide').hidden = hidden;
    node('paused').hidden = !paused;
  };
  const linkURL = id => {
    const url = new URL('/events/' + encodeURIComponent(id), location.origin);
    url.searchParams.set('return_to', location.pathname + location.search);
    return url.pathname + url.search;
  };
  // Cards match the server markup. Live payloads carry no artwork or date, so
  // new cards use the page's fallback art until the next snapshot render.
  const makeRow = id => {
    const row = document.createElement('li'); row.className = 'live-card'; row.dataset.id = id;
    const art = node('art')?.content.firstElementChild?.cloneNode(true);
    const copy = document.createElement('div'); copy.className = 'live-card-copy';
    const byline = document.createElement('p'); byline.className = 'byline';
    const avatar = document.createElement('span'); avatar.className = 'avatar'; avatar.setAttribute('aria-hidden', 'true'); avatar.dataset.liveAvatar = '';
    const attribution = document.createElement('span'); attribution.dataset.liveAttribution = '';
    byline.append(avatar, attribution);
    const link = document.createElement('a'); link.className = 'text-link live-card-link'; link.dataset.eventLink = ''; link.dataset.liveEventLink = ''; link.dataset.id = id; link.href = linkURL(id);
    const meta = document.createElement('p'); meta.className = 'field-note'; meta.dataset.liveMeta = '';
    copy.append(byline, link, meta);
    if (art) row.append(art);
    row.append(copy); return row;
  };
  const fillRow = (row, item) => {
    const name = item.actor_first_name || '';
    const avatar = row.querySelector('[data-live-avatar]');
    // Same letters as the server's initials(): first and last words.
    const words = name.trim().split(/\s+/).filter(Boolean);
    const initials = words.length ? [words[0], ...(words.length > 1 ? [words.at(-1)] : [])].map(word => [...word][0].toUpperCase()).join('') : '';
    if (avatar) avatar.textContent = item.recent_recommendations > 1 ? String(item.recent_recommendations) : initials;
    row.querySelector('[data-live-attribution]').textContent = item.recent_recommendations > 1 ? `${item.recent_recommendations} people recommended ` : `${name || 'Someone'} recommended `;
    const link = row.querySelector('[data-live-event-link]');
    link.textContent = item.event_title;
    link.href = linkURL(item.event_id);
    const minutes = Math.max(0, Math.floor((now() - Date.parse(item.latest_recommended_at)) / 60000));
    row.querySelector('[data-live-meta]').textContent = `${item.city ? item.city + ' · ' : ''}${minutes ? minutes + (minutes === 1 ? ' minute ago' : ' minutes ago') : 'just now'}`;
  };
  const render = () => {
    reconcileToasts();
    if (!onPage) return;
    const eligible = new Map(current.map(item => [item.event_id, item]));
    // Reconcile existing rows in place: redactions/removals are never buffered.
    for (const row of [...list.children]) {
      if (!shown.includes(row.dataset.id) || !eligible.has(row.dataset.id)) {
        if (row.contains(document.activeElement)) pageFocus();
        row.remove();
      }
    }
    for (const id of shown) {
      const item = eligible.get(id); if (!item) continue;
      let row = [...list.children].find(child => child.dataset.id === id);
      if (!row) {
        row = makeRow(id); list.append(row);
      }
      fillRow(row, item);
    }
    node('badge').textContent = pending.size ? `${pending.size} new` : '';
    updates.hidden = !pending.size;
    updates.textContent = `Show ${pending.size} new updates`;
    node('connection').textContent = current.length ? 'Recent public recommendations' : (restricted() ? 'No recent recommendations match these filters. Use Reset filters to see all events.' : 'Nothing in the last 10 minutes. New public recommendations appear here as people share them.');
    node('empty-reset').hidden = !!current.length || !restricted();
    node('empty-community').hidden = !!current.length || restricted();
    node('empty-state').hidden = !!current.length;
  };
  const receive = data => {
    if (!Array.isArray(data.items) || !Number.isFinite(Date.parse(data.as_of))) throw new Error();
    if (!baseline && Date.parse(data.as_of) < serverAt) return;
    // Metadata changes can admit older contributions. They replace this view
    // quietly rather than being announced as newly published recommendations.
    const quietReplacement = !baseline && data.items.some(item => !current.some(old => old.event_id === item.event_id) && Date.parse(item.latest_recommended_at) <= serverAt);
    if (quietReplacement) pending.clear();
    serverAt = Date.parse(data.as_of); receivedAt = performance.now();
    current = data.items.filter(item => Date.parse(item.expires_at) > serverAt).slice(0, 20);
    const active = new Set(current.map(item => item.event_id));
    const fresh = [];
    for (const id of pending) if (!active.has(id)) pending.delete(id);
    for (const item of current) {
      const previous = seen.get(item.event_id);
      const stamp = Date.parse(item.latest_recommended_at);
      if (!baseline && !quietReplacement && (!previous || stamp > previous)) { pending.add(item.event_id); fresh.push(item); }
      seen.set(item.event_id, Math.max(previous || 0, stamp));
    }
    // Deduplication is page-local and bounded; no identity text enters storage.
    if (seen.size > 500) for (const id of seen.keys()) { if (!active.has(id)) seen.delete(id); if (seen.size <= 250) break; }
    if (baseline || quietReplacement) shown = current.map(item => item.event_id);
    else shown = shown.filter(id => active.has(id));
    baseline = false; render();
    if (!pending.size) node('status').textContent = '';
    showToasts(fresh);
    if (fresh.length && (onPage || toasts.children.length)) {
      clearTimeout(announceTimer);
      announceTimer = setTimeout(() => {
        const message = pending.size ? 'New public recommendations available in Happening Now.' : '';
        if (node('status').textContent !== message) node('status').textContent = message;
      }, 300);
    }
    clearTimeout(expiryTimer);
    if (current.length) expiryTimer = setTimeout(() => {
      // Clear the entire group at its earliest expiry; a fresh snapshot can safely reproject it.
      current = current.filter(item => Date.parse(item.expires_at) > now());
      for (const id of pending) if (!current.some(item => item.event_id === id)) pending.delete(id);
      render(); refreshActivity();
    }, Math.max(0, Math.min(...current.map(item => Date.parse(item.expires_at))) - now()));
  };
  // Index count targets once per snapshot and leave unchanged ones untouched, so
  // an update costs one document scan rather than several per loaded event.
  const byEvent = (selector, name) => {
    const index = new Map();
    for (const element of document.querySelectorAll(selector)) {
      const id = element.dataset[name];
      if (!index.has(id)) index.set(id, []);
      index.get(id).push(element);
    }
    return index;
  };
  const setHidden = (element, value) => { if (element.hidden !== value) element.hidden = value; };
  const setCount = (element, count, label) => {
    const number = element.firstElementChild;
    if (element.childNodes.length === 2 && number?.tagName === 'STRONG' && number.textContent === String(count) && element.lastChild.textContent === label) return;
    const strong = document.createElement('strong'); strong.textContent = count;
    element.replaceChildren(strong, document.createTextNode(label));
  };
  const applyCounts = data => {
    setHidden(node('count-status'), true);
    const totals = byEvent('[data-live-total]', 'liveTotal');
    const empties = byEvent('[data-live-empty]', 'liveEmpty');
    const interests = byEvent('[data-interest-count]', 'interestCount');
    const recommendations = byEvent('[data-live-recommendation-count]', 'liveRecommendationCount');
    for (const item of data.items || []) {
      for (const element of totals.get(item.event_id) || []) {
        setCount(element, item.recommendation_count, item.recommendation_count === 1 ? ' recommendation' : ' recommendations');
        // An event's own count stays hidden at zero; its empty state says so instead.
        if (element.hasAttribute('data-live-hide-zero')) setHidden(element, item.recommendation_count === 0);
        delete element.dataset.countsStale;
      }
      for (const element of empties.get(item.event_id) || []) setHidden(element, item.recommendation_count > 0);
      for (const element of interests.get(item.event_id) || []) {
        setCount(element, item.interested_count, item.interested_count === 1 ? ' person interested' : ' people interested');
        setHidden(element, item.interested_count === 0); delete element.dataset.countsStale;
      }
      for (const element of recommendations.get(item.event_id) || []) {
        const label = `${item.recommendation_count} public ${item.recommendation_count === 1 ? 'recommendation' : 'recommendations'}`;
        if (element.textContent !== label) element.textContent = label;
        setHidden(element.closest('[data-live-recommendation-wrap]'), item.recommendation_count === 0);
        const group = element.closest('[data-live-count-group]'); setHidden(group, ![...group.children].some(child => !child.hidden));
        delete element.dataset.countsStale;
      }
    }
  };
  const staleCounts = () => {
    node('count-status').hidden = !ids().length;
    document.querySelectorAll('[data-interest-count], [data-live-recommendation-count], [data-live-total]').forEach(element => { element.dataset.countsStale = 'true'; });
  };
  const fetchJSON = async url => {
    const response = await fetch(url, { cache: 'no-store', signal: AbortSignal.timeout(10000) });
    if (!response.ok) throw new Error(); return response.json();
  };
  const refreshCounts = async () => {
    const turn = ++countsGeneration, values = ids(); if (!values.length || document.hidden || writes) return;
    try {
      for (let start = 0; start < values.length; start += 101) {
        const data = await fetchJSON('/api/v1/events/activity-counts?' + query(values.slice(start, start + 101)));
        if (turn !== countsGeneration) return;
        applyCounts(data);
      }
    }
    catch { if (turn === countsGeneration) staleCounts(); }
  };
  const refreshActivity = async () => {
    if (!allowed()) return;
    const turn = generation;
    try { const data = await fetchJSON(activityURL() + '?' + activityQuery([])); if (turn === generation && allowed()) { if (data.state === 'paused') { stop(); preferences(); } else receive(data); } }
    catch { if (turn === generation) { interrupted(); preferences(); } }
  };
  const interrupted = () => {
    stop(); staleCounts(); text('connection', 'Live activity is reconnecting');
    if (allowed()) { reconnectTimer = setTimeout(connect, backoff * (0.8 + Math.random() * 0.2)); backoff = Math.min(60000, backoff * 2); }
  };
  const connect = () => {
    stop(); if (!allowed()) return;
    text('connection', 'Connecting to live activity…');
    if (!window.EventSource) { refreshActivity(); refreshCounts(); return; }
    const turn = generation; idsKey = ids().join('|');
    stream = new EventSource(activityURL() + '/stream?' + activityQuery(ids().slice(0, 101)));
    lastMessage = performance.now();
    const listen = (type, callback) => stream.addEventListener(type, event => {
      if (turn !== generation || !allowed()) return;
      lastMessage = performance.now(); backoff = 5000;
      try { callback(JSON.parse(event.data)); } catch { interrupted(); }
    });
    listen('activity.snapshot', receive);
    listen('counts.snapshot', data => { ++countsGeneration; applyCounts(data); });
    listen('heartbeat', render); listen('unavailable', interrupted);
    // This account's own Saved/Interested/follow change: the server closes the
    // stream so stale membership is never shown. Clear it as before, then
    // reconnect quietly after a short jitter instead of outage backoff.
    listen('membership.changed', () => {
      stop(); text('connection', 'Updating live activity…');
      reconnectTimer = setTimeout(connect, 250 + Math.random() * 500);
    });
    listen('settings.changed', () => { resume(); });
    stream.onerror = () => { if (turn === generation) { interrupted(); preferences(); } };
  };
  let preferenceTurn = 0;
  const nativeURL = () => {
    if (authenticated) return '/happening-now';
    const q = new URLSearchParams({ location_mode: filters.location.mode });
    if (filters.location.mode === 'city') { q.set('live_city', filters.location.city); q.set('live_country', filters.location.country); }
    filters.category_ids.forEach(id => q.append('live_category_ids', id));
    (filters.artist_ids || []).forEach(id => q.append('live_artist_ids', id));
    (filters.venue_ids || []).forEach(id => q.append('live_venue_ids', id));
    return '/happening-now?' + q;
  };
  const summary = () => {
    if (!onPage) return;
    node('refresh').href = nativeURL();
    if (location.search) history.replaceState(history.state, '', nativeURL());
    const names = filters.category_ids.map(id => catalog.find(c => c.id === id)?.name || id);
    const entities = ['artist', 'venue'].map(kind => (filters[kind + '_ids'] || []).map(id => window.ticketopiaLivePickers?.name(kind, id) || id).join(' or ')).filter(Boolean);
    const connections = { saved: 'Saved', interested: 'Interested', followed_artists: 'Followed artists', followed_venues: 'Followed venues' };
    node('summary').textContent = [filters.location.mode === 'city' ? filters.location.city + ', ' + filters.location.country : 'Everywhere', names.length ? names.join(' or ') : 'All event types', ...entities, filters.connections.sources.map(id => connections[id]).join(' or ') || 'All events'].join(' · ');
  };
  document.addEventListener('ticketopia:live-labels', summary);
  let nativeDraft = onPage && node('filter-details').hasAttribute('data-live-draft');
  const editFilters = value => {
    if (!onPage) return;
    if (nativeDraft) { nativeDraft = false; return; }
    window.ticketopiaLivePickers?.set(value, authenticated);
    node('location').value = value.location.mode;
    node('city').value = value.location.city || ''; node('country').value = value.location.country || '';
    node('connections')?.querySelectorAll('input').forEach(input => { input.checked = value.connections?.sources?.includes(input.value) || false; });
    const fieldset = node('categories');
    fieldset.querySelectorAll('label').forEach(label => label.remove());
    const choices = [...catalog];
    value.category_ids.forEach(id => { if (!choices.some(c => c.id === id)) choices.push({ id, name: 'Selected category: ' + id }); });
    for (const category of choices) {
      const label = document.createElement('label'), input = document.createElement('input'), text = document.createElement('span');
      input.type = 'checkbox'; input.name = 'live_category_ids'; input.value = category.id; input.checked = value.category_ids.includes(category.id);
      text.textContent = category.name; label.append(input, text); fieldset.append(label);
    }
    node('reuse').hidden = !authenticated;
  };
  const draftFilters = () => {
    const sources = [...(node('connections')?.querySelectorAll('input:checked') || [])].map(input => input.value).sort();
    return { ...defaults(), ...window.ticketopiaLivePickers?.get(), location: node('location').value === 'city' ? { mode: 'city', city: node('city').value.trim().replace(/\s+/g, ' '), country: node('country').value.trim().toUpperCase() } : { mode: 'all' }, category_ids: [...node('categories').querySelectorAll('input:checked')].map(input => input.value).sort(), connections: { mode: sources.length ? 'any' : 'all_events', sources } };
  };
  // What the fields last showed of confirmed filters: refreshes and saves leave
  // later, unapplied edits alone instead of replacing them.
  let shownFilters;
  const showFilters = value => { if (!onPage) return; editFilters(value); shownFilters = JSON.stringify(draftFilters()); };
  const fieldsEdited = () => onPage && shownFilters !== undefined && JSON.stringify(draftFilters()) !== shownFilters;
  let browsingCity;
  const browseQuery = new URLSearchParams(location.search);
  if (location.pathname === '/' && browseQuery.get('city') && /^[a-z]{2}$/i.test(browseQuery.get('country') || '')) {
    try { sessionStorage.setItem('ticketopia-live-browsing-city', JSON.stringify({ city: browseQuery.get('city'), country: browseQuery.get('country').toUpperCase() })); } catch {}
  }
  try { browsingCity = JSON.parse(sessionStorage.getItem('ticketopia-live-browsing-city')); } catch {}
  if (onPage) {
  node('filter-details').addEventListener('toggle', async () => {
    if (!node('filter-details').open || catalog.length) return;
    try { const data = await fetchJSON('/api/v1/categories'); const draft = draftFilters(); catalog = data.items; editFilters(draft); summary(); }
    catch { node('filter-error').textContent = 'Event types could not load. Your saved selections remain available; close and reopen to retry.'; }
  });
  node('browsing').hidden = !browsingCity?.city || !browsingCity?.country;
  node('browsing').onclick = () => { if (confirm(`Use ${browsingCity.city}, ${browsingCity.country} for live activity?`)) { node('location').value = 'city'; node('city').value = browsingCity.city; node('country').value = browsingCity.country; } };
  node('reuse').onclick = () => { editFilters(guestFilters); node('filter-error').textContent = 'Browser filters copied. Apply to save them to your account.'; };
  node('filter-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (saving) { node('filter-error').textContent = 'Still applying your filters. Apply again once they finish.'; return; }
    if (!ready) return;
    const next = event.submitter?.value === 'reset' ? defaults() : draftFilters();
    const turn = preferenceTurn, scope = preferenceScope;
    // Fields stay editable while a write is pending; edits made meanwhile are
    // kept, not replaced by the confirmed filters, and need their own Apply.
    const submitted = JSON.stringify(draftFilters());
    const settle = () => {
      if (JSON.stringify(draftFilters()) === submitted) { showFilters(filters); return; }
      node('filter-error').textContent = 'Filters applied. Your newer changes aren\'t applied yet; apply again to save them.';
    };
    saving = true; node('filter-form').setAttribute('aria-busy', 'true'); node('filter-error').textContent = 'Applying filters…';
    try {
      if (authenticated) {
        const me = await fetchJSON('/api/v1/me');
        const response = await fetch('/api/v1/me/live-activity-preferences', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token, 'If-Match': scope }, body: JSON.stringify({ version: 1, enabled, filters: next }), signal: AbortSignal.timeout(10000) });
        if (response.status === 412) { await preferences(); showFilters(filters); throw new Error('Settings changed in another tab. Review the current filters and apply again.'); }
        if (!response.ok) throw new Error('Filters could not be saved. Check the city/country and choose at most 16 event types, artists and venues each, then retry.');
        if (scope !== preferenceScope) return;
        await preferences();
        node('filter-error').textContent = ''; settle();
      } else {
        const data = await fetchJSON('/api/v1/community/activity?filter=' + encodeURIComponent(JSON.stringify(publicFilter(next))));
        if (turn !== preferenceTurn || authenticated) return;
        guestFilters = next; filters = next; node('filter-error').textContent = '';
        try { localStorage.setItem(storageKey, JSON.stringify(publicFilter(next))); } catch { node('filter-error').textContent = 'Applied for this page. Browser storage is unavailable.'; }
        stop(); settle(); summary(); history.replaceState(history.state, '', nativeURL()); receive(data); if (allowed()) connect();
      }
    } catch (error) { node('filter-error').textContent = error.message || 'Filters could not be applied. Your confirmed filters are unchanged.'; }
    finally {
      saving = false; node('filter-form').removeAttribute('aria-busy');
      if (node('filter-error').textContent === 'Applying filters…') node('filter-error').textContent = '';
    }
  });
  // A native guest form is a deliberate choice; carry it into the same browser
  // preference used by toasts. Account settings remain authoritative on sign-in.
  if (node('page').hasAttribute('data-live-guest') && new URLSearchParams(location.search).has('location_mode')) {
    guestFilters = new URLSearchParams(location.search).get('filter_action') === 'reset' ? defaults() : draftFilters();
    try { localStorage.setItem(storageKey, JSON.stringify(publicFilter(guestFilters))); } catch {}
  }
  }
  const preferences = async () => {
    const turn = ++preferenceTurn;
    try {
      const response = await fetch('/api/v1/me/live-activity-preferences', { cache: 'no-store', signal: AbortSignal.timeout(10000) });
      let next, nextRevision = 0, nextAuth = false, nextScope = '', nextFilters = { ...defaults(), ...guestFilters }, nextEnabled = true;
      if (response.status === 401) next = authenticated;
      else if (response.ok) { const data = await response.json(); next = data.notifications_paused === true || !data.enabled; nextEnabled = data.enabled; nextFilters = data.filters; nextRevision = data.revision; nextAuth = true; nextScope = response.headers.get('ETag') || ''; }
      else throw new Error();
      if (turn !== preferenceTurn) return;
      const changed = !ready || paused !== next || revision !== nextRevision || authenticated !== nextAuth || preferenceScope !== nextScope || JSON.stringify(filters) !== JSON.stringify(nextFilters);
      if (authenticated && !nextAuth) { paused = true; stop(); controls(); return; }
      if (preferenceScope.split('-')[1] !== nextScope.split('-')[1]) seen.clear();
      ready = true; paused = next; revision = nextRevision; authenticated = nextAuth; preferenceScope = nextScope; controls();
      enabled = nextEnabled; filters = nextFilters; summary(); if (changed && !fieldsEdited()) showFilters(filters);
      if (changed) { stop(); if (allowed()) connect(); }
    } catch { if (turn === preferenceTurn) { paused = true; stop(); controls(); } }
  };
  if (onPage) {
    node('hide').onclick = () => { hidden = true; try { localStorage.setItem('ticketopia-live-hidden', 'true'); } catch {} controls(); node('show').focus(); };
    node('show').onclick = () => { hidden = false; try { localStorage.setItem('ticketopia-live-hidden', 'false'); } catch {} controls(); node('hide').focus(); };
    updates.onclick = () => { shown = current.map(item => item.event_id); pending.clear(); text('status', ''); render(); for (const id of shown) { const row = [...list.children].find(row => row.dataset.id === id); if (row) list.append(row); } pageFocus(); };
  }
  document.addEventListener('ticketopia:activity-mutation', () => { ++countsGeneration; stop(); refreshCounts(); connect(); });
  document.addEventListener('ticketopia:activity-write-end', () => { writes = Math.max(0, writes - 1); refreshCounts(); if (allowed()) connect(); });
  document.addEventListener('submit', event => {
    if (event.target.matches('[data-interest-form], [data-recommendation-form]') && event.target.getAttribute('aria-busy') !== 'true') { ++writes; ++countsGeneration; stop(); }
  }, true);
  // Only added or removed event targets can change the subscribed IDs; count text
  // updates inside existing targets do not.
  const changesTargets = record => [...record.addedNodes, ...record.removedNodes].some(node => node.nodeType === Node.ELEMENT_NODE
    && (node.matches(eventTargets) || node.querySelector(eventTargets)));
  new MutationObserver(records => {
    if (!records.some(changesTargets)) return;
    clearTimeout(debounceTimer); debounceTimer = setTimeout(() => {
      const key = ids().join('|'); if (key !== idsKey) { idsKey = key; refreshCounts(); if (allowed()) connect(); }
    }, 300);
  }).observe(document.getElementById('main') || document.body, { childList: true, subtree: true });
  const resume = async () => { stop(); await preferences(); refreshCounts(); if (allowed() && !stream) connect(); };
  document.addEventListener('visibilitychange', () => { if (document.hidden) stop(); else resume(); });
  window.addEventListener('pagehide', stop);
  window.addEventListener('pageshow', event => { if (event.persisted) resume(); });
  window.addEventListener('offline', interrupted); window.addEventListener('online', resume);
  window.addEventListener('focus', resume);
  window.addEventListener('storage', event => {
    if (event.key === 'ticketopia-live-hidden') { hidden = event.newValue === 'true'; controls(); resume(); }
    if (event.key === storageKey) { try { guestFilters = event.newValue ? { ...defaults(), ...JSON.parse(event.newValue) } : defaults(); if (!authenticated) resume(); } catch {} }
  });
  setInterval(() => { if (!document.hidden) preferences(); }, 30000);
  setInterval(() => {
    if (document.hidden) return;
    if (!stream || ids().length > 101) refreshCounts();
    if (!window.EventSource) refreshActivity();
    else if (allowed() && !stream && !reconnectTimer) connect();
  }, 15000);
  setInterval(() => { if (stream && performance.now() - lastMessage >= 30000) interrupted(); }, 1000);
  const firstName = document.getElementById('activity-first-name');
  firstName?.addEventListener('input', () => { const preview = document.querySelector('[data-activity-name-preview]'); if (preview) preview.textContent = firstName.value.trim().replace(/\s+/g, ' ') || 'Someone'; });
  preferences(); refreshCounts();
})();
