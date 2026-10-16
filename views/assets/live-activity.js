(() => {
  'use strict';
  const root = document.querySelector('[data-live-activity]');
  if (!root) return;
  const node = name => root.querySelector(`[data-live-${name}]`);
  const details = node('details'), list = node('list'), updates = node('updates');
  let hidden = false, paused = true, ready = false, stream, generation = 0, countsGeneration = 0;
  let reconnectTimer, debounceTimer, expiryTimer, announceTimer, lastMessage = 0, backoff = 5000, idsKey = '';
  let current = [], shown = [], baseline = true, serverAt = 0, receivedAt = 0, writes = 0;
  let authenticated = false, revision = 0, preferenceScope = '';
  const activityURL = () => authenticated ? '/api/v1/me/community/activity' : '/api/v1/community/activity';
  const activityQuery = values => { const q = query(values); if (authenticated) q.set('preferences_revision', revision); return q; };
  const seen = new Map(), pending = new Set();
  try { hidden = localStorage.getItem('ticketopia-live-hidden') === 'true'; } catch { /* Page memory remains usable. */ }
  const ids = () => [...new Set([...document.querySelectorAll('[data-event-id], [data-interest-count], [data-event-recommendations], [data-live-total]')]
    .filter(element => !root.contains(element))
    .map(element => element.dataset.eventId || element.dataset.interestCount || element.dataset.eventRecommendations || element.dataset.liveTotal))].sort();
  const query = values => { const q = new URLSearchParams(); values.forEach(id => q.append('event_id', id)); return q; };
  const allowed = () => ready && !hidden && !paused && !document.hidden && navigator.onLine && !writes;
  const now = () => serverAt + performance.now() - receivedAt;
  const clear = () => {
    if (list.contains(document.activeElement)) details.querySelector('summary').focus({ preventScroll: true });
    current = []; shown = []; pending.clear(); list.replaceChildren();
    node('badge').textContent = ''; updates.hidden = true; node('status').textContent = '';
    clearTimeout(expiryTimer);
    clearTimeout(announceTimer);
  };
  const stop = () => { ++generation; stream?.close(); stream = null; clearTimeout(reconnectTimer); reconnectTimer = undefined; clear(); baseline = true; };
  const controls = () => {
    root.hidden = false; details.hidden = hidden || paused;
    node('show').hidden = !hidden || paused; node('paused').hidden = !paused;
  };
  const linkURL = id => {
    const url = new URL('/events/' + encodeURIComponent(id), location.origin);
    url.searchParams.set('return_to', location.pathname + location.search);
    return url.pathname + url.search;
  };
  const render = () => {
    const eligible = new Map(current.map(item => [item.event_id, item]));
    // Reconcile existing rows in place: redactions/removals are never buffered.
    for (const row of [...list.children]) {
      if (!shown.includes(row.dataset.id) || !eligible.has(row.dataset.id)) {
        if (row.contains(document.activeElement)) details.querySelector('summary').focus({ preventScroll: true });
        row.remove();
      }
    }
    for (const id of shown) {
      const item = eligible.get(id); if (!item) continue;
      let row = [...list.children].find(child => child.dataset.id === id);
      if (!row) {
        row = document.createElement('li'); row.dataset.id = id;
        const attribution = document.createElement('span'); attribution.dataset.liveAttribution = '';
        const link = document.createElement('a'); link.className = 'text-link'; link.dataset.eventLink = ''; link.dataset.liveEventLink = ''; link.dataset.id = id; link.href = linkURL(id);
        const meta = document.createElement('span'); meta.className = 'field-note'; meta.dataset.liveMeta = '';
        row.append(attribution, link, meta); list.append(row);
      }
      row.querySelector('[data-live-attribution]').textContent = item.recent_recommendations > 1 ? `${item.recent_recommendations} people recommended ` : `${item.actor_first_name || 'Someone'} recommended `;
      row.querySelector('a').textContent = item.event_title;
      const minutes = Math.max(0, Math.floor((now() - Date.parse(item.latest_recommended_at)) / 60000));
      row.querySelector('[data-live-meta]').textContent = ` · ${item.city ? item.city + ' · ' : ''}${minutes ? minutes + (minutes === 1 ? ' minute ago' : ' minutes ago') : 'just now'}`;
    }
    node('badge').textContent = pending.size ? `${pending.size} new` : '';
    updates.hidden = !details.open || !pending.size;
    updates.textContent = `Show ${pending.size} new updates`;
    node('connection').textContent = current.length ? 'Recent public recommendations · all cities' : 'No recent recommendations. Explore Community.';
  };
  const receive = data => {
    if (!Array.isArray(data.items) || !Number.isFinite(Date.parse(data.as_of))) throw new Error();
    if (!baseline && Date.parse(data.as_of) < serverAt) return;
    serverAt = Date.parse(data.as_of); receivedAt = performance.now();
    current = data.items.filter(item => Date.parse(item.expires_at) > serverAt).slice(0, 20);
    const active = new Set(current.map(item => item.event_id));
    let newlyAvailable = false;
    for (const id of pending) if (!active.has(id)) pending.delete(id);
    for (const item of current) {
      const previous = seen.get(item.event_id);
      const stamp = Date.parse(item.latest_recommended_at);
      if (!baseline && (!previous || stamp > previous)) { pending.add(item.event_id); newlyAvailable = true; }
      seen.set(item.event_id, Math.max(previous || 0, stamp));
    }
    // Deduplication is page-local and bounded; no identity text enters storage.
    if (seen.size > 500) for (const id of seen.keys()) { if (!active.has(id)) seen.delete(id); if (seen.size <= 250) break; }
    if (!details.open || baseline) shown = current.map(item => item.event_id);
    else shown = shown.filter(id => active.has(id));
    baseline = false; render();
    if (!pending.size) node('status').textContent = '';
    if (newlyAvailable) {
      clearTimeout(announceTimer);
      announceTimer = setTimeout(() => {
        const message = pending.size ? `${pending.size} new live activity updates available.` : '';
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
  const applyCounts = data => {
    node('count-status').hidden = true;
    for (const item of data.items || []) {
      for (const element of document.querySelectorAll('[data-live-total]')) {
        if (element.dataset.liveTotal !== item.event_id) continue;
        const number = document.createElement('strong'); number.textContent = item.recommendation_count;
        element.replaceChildren(number, document.createTextNode(item.recommendation_count === 1 ? ' recommendation' : ' recommendations'));
        delete element.dataset.countsStale;
      }
      for (const element of document.querySelectorAll('[data-interest-count]')) {
        if (element.dataset.interestCount !== item.event_id) continue;
        const count = document.createElement('strong'); count.textContent = item.interested_count;
        element.replaceChildren(count, document.createTextNode(item.interested_count === 1 ? ' person interested' : ' people interested'));
        element.hidden = item.interested_count === 0; delete element.dataset.countsStale;
      }
      for (const element of document.querySelectorAll('[data-live-recommendation-count]')) {
        if (element.dataset.liveRecommendationCount !== item.event_id) continue;
        element.textContent = `${item.recommendation_count} public ${item.recommendation_count === 1 ? 'recommendation' : 'recommendations'}`;
        element.closest('[data-live-recommendation-wrap]').hidden = item.recommendation_count === 0;
        const group = element.closest('[data-live-count-group]'); group.hidden = ![...group.children].some(child => !child.hidden);
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
    stop(); staleCounts(); node('connection').textContent = 'Live activity is reconnecting';
    if (allowed()) { reconnectTimer = setTimeout(connect, backoff * (0.8 + Math.random() * 0.2)); backoff = Math.min(60000, backoff * 2); }
  };
  const connect = () => {
    stop(); if (!allowed()) return;
    node('connection').textContent = 'Connecting to live activity…';
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
    listen('settings.changed', () => { resume(); });
    stream.onerror = () => { if (turn === generation) { interrupted(); preferences(); } };
  };
  let preferenceTurn = 0;
  const preferences = async () => {
    const turn = ++preferenceTurn;
    try {
      const response = await fetch('/api/v1/me/live-activity-preferences', { cache: 'no-store', signal: AbortSignal.timeout(10000) });
      let next, nextRevision = 0, nextAuth = false, nextScope = '';
      if (response.status === 401) next = authenticated;
      else if (response.ok) { const data = await response.json(); next = data.notifications_paused === true || !data.enabled; nextRevision = data.revision; nextAuth = true; nextScope = response.headers.get('ETag') || ''; }
      else throw new Error();
      if (turn !== preferenceTurn) return;
      const changed = !ready || paused !== next || revision !== nextRevision || authenticated !== nextAuth || preferenceScope !== nextScope;
      if (authenticated && !nextAuth) { paused = true; stop(); controls(); return; }
      if (preferenceScope.split('-')[1] !== nextScope.split('-')[1]) seen.clear();
      ready = true; paused = next; revision = nextRevision; authenticated = nextAuth; preferenceScope = nextScope; controls();
      if (changed) { stop(); if (allowed()) connect(); }
    } catch { if (turn === preferenceTurn) { paused = true; stop(); controls(); } }
  };
  node('hide').onclick = () => { hidden = true; try { localStorage.setItem('ticketopia-live-hidden', 'true'); } catch {} stop(); controls(); node('show').focus(); };
  node('show').onclick = () => { hidden = false; try { localStorage.setItem('ticketopia-live-hidden', 'false'); } catch {} controls(); details.querySelector('summary').focus(); connect(); };
  updates.onclick = () => { shown = current.map(item => item.event_id); pending.clear(); list.replaceChildren(); render(); details.querySelector('summary').focus(); };
  details.addEventListener('toggle', () => { if (details.open) { shown = current.map(item => item.event_id); pending.clear(); render(); } });
  document.addEventListener('ticketopia:activity-mutation', () => { ++countsGeneration; stop(); refreshCounts(); connect(); });
  document.addEventListener('ticketopia:activity-write-end', () => { writes = Math.max(0, writes - 1); refreshCounts(); if (allowed()) connect(); });
  document.addEventListener('submit', event => {
    if (event.target.matches('[data-interest-form], [data-recommendation-form]') && event.target.getAttribute('aria-busy') !== 'true') { ++writes; ++countsGeneration; stop(); }
  }, true);
  new MutationObserver(() => {
    clearTimeout(debounceTimer); debounceTimer = setTimeout(() => {
      const key = ids().join('|'); if (key !== idsKey) { idsKey = key; refreshCounts(); if (allowed()) connect(); }
    }, 300);
  }).observe(document.getElementById('main') || document.body, { childList: true, subtree: true });
  const resume = async () => { stop(); await preferences(); refreshCounts(); if (allowed()) connect(); };
  document.addEventListener('visibilitychange', () => { if (document.hidden) stop(); else resume(); });
  window.addEventListener('pagehide', stop);
  window.addEventListener('pageshow', event => { if (event.persisted) resume(); });
  window.addEventListener('offline', interrupted); window.addEventListener('online', resume);
  window.addEventListener('focus', resume);
  window.addEventListener('storage', event => { if (event.key === 'ticketopia-live-hidden') { hidden = event.newValue === 'true'; controls(); resume(); } });
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
