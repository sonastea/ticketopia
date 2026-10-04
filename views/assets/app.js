(() => {
  'use strict';

  const filters = document.getElementById('filters');
  document.addEventListener('click', event => {
    const link = event.target.closest('[data-open-filters]');
    if (!link || !filters) return;
    event.preventDefault();
    filters.open = true;
    document.getElementById('city').focus();
  });
  document.addEventListener('invalid', event => {
    const disclosure = event.target.closest('details');
    if (disclosure) disclosure.open = true;
  }, true);
  document.addEventListener('error', event => {
    if (event.target.matches?.('.event-image img')) event.target.hidden = true;
  }, true);
  for (const image of document.querySelectorAll('.event-image img')) {
    if (image.complete && !image.naturalWidth) image.hidden = true;
  }

  for (const name of ['htmx:responseError', 'htmx:sendError', 'htmx:timeout']) {
    document.body.addEventListener(name, () => {
      const message = document.getElementById('load-error');
      if (message) message.textContent = 'More shows could not be loaded. Please try again shortly.';
    });
  }
  document.body.addEventListener('htmx:beforeRequest', () => {
    const message = document.getElementById('load-error');
    if (message) message.textContent = '';
  });

  const detect = document.getElementById('detect-city');
  if (detect) {
    const feedback = document.getElementById('location-feedback');
    const key = 'ticketopia-browser-city-v1';
    const valid = value => value && typeof value.city === 'string' && value.city.trim().length > 0 && new TextEncoder().encode(value.city.trim()).length <= 120 && /^[A-Z]{2}$/.test(value.country_code);
    document.getElementById('browser-location').hidden = false;
    detect.addEventListener('click', async () => {
      detect.disabled = true;
      detect.textContent = 'Detecting city…';
      feedback.textContent = 'Estimating your city from your public IP address.';
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 4000);
      try {
        let city;
        try {
          const cached = JSON.parse(localStorage.getItem(key));
          if (valid(cached) && cached.expires > Date.now() && cached.expires <= Date.now() + 86400000) city = cached;
        } catch { /* Location still works when storage is unavailable. */ }
        if (!city) {
          const response = await fetch('https://ipwho.is/?fields=success,city,country_code', { signal: controller.signal, credentials: 'omit', referrerPolicy: 'no-referrer' });
          if (!response.ok) throw new Error('Lookup unavailable');
          city = await response.json();
          if (!city.success || !valid(city)) throw new Error('City unavailable');
          try {
            localStorage.setItem(key, JSON.stringify({ city: city.city.trim(), country_code: city.country_code, expires: Date.now() + 86400000 }));
          } catch { /* An editable city does not require persistence. */ }
        }
        document.getElementById('city').value = city.city.trim();
        detect.form.elements.country.value = city.country_code;
        feedback.textContent = 'Estimated city: ' + city.city.trim() + ', ' + city.country_code + '. You can change it before searching.';
        document.getElementById('city').focus();
      } catch {
        feedback.textContent = "We couldn't detect your city. Enter a city above to find shows.";
      } finally {
        clearTimeout(timeout);
        detect.disabled = false;
        detect.textContent = 'Detect my city';
      }
    });
  }

  // Carry the originating history entry through full-page event section links.
  // The one-use handoff is accepted only by the exact next same-origin page.
  const returnKey = 'ticketopia-event-return-v1';
  const stageReturn = (url, journey) => {
    try {
      sessionStorage.setItem(returnKey, JSON.stringify({ from: location.href, to: url.href, journey }));
    } catch { /* The normal return link still preserves filters without storage. */ }
  };
  const validJourney = journey => {
    try {
      const origin = new URL(journey.origin);
      return origin.origin === location.origin && origin.pathname === '/' && Number.isInteger(journey.steps) && journey.steps > 0 && journey.steps < history.length;
    } catch { return false; }
  };

  const workspace = document.querySelector('[data-results-url]');
  if (!workspace) {
    const back = document.querySelector('[data-return-results]');
    if (!back) return;
    try {
      const pending = JSON.parse(sessionStorage.getItem(returnKey));
      sessionStorage.removeItem(returnKey);
      if (pending?.from === document.referrer && pending.to === location.href && validJourney(pending.journey)) {
        history.replaceState({ ...history.state, ticketopiaReturn: pending.journey }, '', location.href);
      }
    } catch { /* Direct links and unavailable storage use the server return URL. */ }
    if (!validJourney(history.state?.ticketopiaReturn) && document.referrer) {
      const immediate = { origin: document.referrer, steps: 1 };
      if (validJourney(immediate)) history.replaceState({ ...history.state, ticketopiaReturn: immediate }, '', location.href);
    }
    back.addEventListener('click', event => {
      const journey = history.state?.ticketopiaReturn;
      if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || !validJourney(journey)) return;
      event.preventDefault();
      history.go(-journey.steps);
    });
    document.addEventListener('click', event => {
      if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const link = event.target.closest('.section-nav a');
      if (!link) return;
      const url = new URL(link.href);
      if (url.href === location.href) {
        event.preventDefault();
        return;
      }
      const journey = history.state?.ticketopiaReturn;
      if (validJourney(journey)) stageReturn(url, { ...journey, steps: journey.steps + 1 });
    });
    return;
  }

  const context = document.getElementById('event-context');
  const emptyContext = context.querySelector('.context-empty')?.outerHTML;
  const status = document.getElementById('selection-status');
  const desktop = matchMedia('(min-width: 62rem)');
  let request;
  let generation = 0;
  let lastFocusID = new URL(location.href).searchParams.get('selected_event') || '';
  let resultsScroll = scrollY;

  const markSelection = id => {
    for (const row of document.querySelectorAll('.event-row')) {
      const selected = row.dataset.eventId === id;
      row.dataset.selected = String(selected);
      const title = row.querySelector('.event-link');
      if (selected) title?.setAttribute('aria-current', 'true');
      else title?.removeAttribute('aria-current');
    }
    workspace.classList.toggle('has-selection', Boolean(id));
  };
  const detailURL = (id, section = 'overview') => {
    const url = new URL('/events/' + encodeURIComponent(id), location.origin);
    url.searchParams.set('return_to', workspace.dataset.resultsUrl);
    if (section !== 'overview') url.searchParams.set('section', section);
    return url;
  };
  const rememberResults = () => {
    const list = document.getElementById('event-list');
    const pagination = document.getElementById('pagination');
    if (!list || !pagination || list.innerHTML.length > 2000000) return;
    history.replaceState({ ...history.state, ticketopiaResults: {
      url: workspace.dataset.resultsUrl,
      list: list.innerHTML,
      pagination: pagination.outerHTML,
      scroll: scrollY,
      focus: lastFocusID,
    } }, '', location.href);
  };
  const leaveResults = link => {
    rememberResults();
    const url = new URL(link.href);
    url.searchParams.set('return_to', workspace.dataset.resultsUrl);
    link.href = url.href;
    stageReturn(url, { origin: location.href, steps: 1 });
  };
  const restoreResults = () => {
    const saved = history.state?.ticketopiaResults;
    const list = document.getElementById('event-list');
    if (!list || !saved || saved.url !== workspace.dataset.resultsUrl) return;
    list.innerHTML = saved.list;
    document.getElementById('pagination').outerHTML = saved.pagination;
    window.htmx?.process(list);
    window.htmx?.process(document.getElementById('pagination'));
    markSelection(new URL(location.href).searchParams.get('selected_event'));
    requestAnimationFrame(() => {
      scrollTo(0, saved.scroll);
      const row = [...list.children].find(item => item.dataset.eventId === saved.focus);
      row?.querySelector('.event-link')?.focus({ preventScroll: true });
    });
  };
  window.addEventListener('pageshow', restoreResults);

  const showError = (url, retry) => {
    context.replaceChildren();
    const box = document.createElement('div');
    box.className = 'context-inner feedback';
    box.setAttribute('role', 'alert');
    const heading = document.createElement('h2');
    heading.textContent = 'Event preview unavailable';
    const message = document.createElement('p');
    message.textContent = "We couldn't load this event. Try again or open its full page.";
    const button = document.createElement('button');
    button.className = 'action';
    button.textContent = 'Retry';
    button.addEventListener('click', retry);
    const link = document.createElement('a');
    link.className = 'text-link';
    link.href = url.href;
    link.textContent = 'Open event';
    const close = document.createElement('a');
    close.className = 'text-link';
    close.href = workspace.dataset.resultsUrl;
    close.dataset.closeContext = '';
    close.textContent = 'Back to results';
    box.append(heading, message, button, link, close);
    context.append(box);
  };
  const select = async (id, section, push = true) => {
    const panelHadFocus = context.contains(document.activeElement);
    request?.abort();
    request = new AbortController();
    const currentRequest = request;
    const turn = ++generation;
    if (push) {
      resultsScroll = scrollY;
      rememberResults();
      const next = new URL(location.href);
      next.searchParams.set('selected_event', id);
      if (section !== 'overview') next.searchParams.set('section', section);
      else next.searchParams.delete('section');
      history.pushState({ ...history.state }, '', next);
    }
    markSelection(id);
    const url = detailURL(id, section);
    context.setAttribute('aria-busy', 'true');
    context.innerHTML = '<div class="context-loading"><p>Loading event…</p><div class="skeleton-art" aria-hidden="true"></div><div class="skeleton-line" aria-hidden="true"></div><div class="skeleton-line" aria-hidden="true"></div></div>';
    status.textContent = 'Loading event preview.';
    const timeout = setTimeout(() => currentRequest.abort(), 12000);
    try {
      const response = await fetch(url, { signal: currentRequest.signal, headers: { 'X-Ticketopia-Panel': 'true' } });
      if (!response.ok) throw new Error('Event unavailable');
      const html = await response.text();
      if (turn !== generation) return;
      context.innerHTML = html;
      context.scrollTop = 0;
      if (panelHadFocus) context.querySelector('.section-nav a[aria-current="page"]')?.focus({ preventScroll: true });
      status.textContent = 'Event preview loaded: ' + context.querySelector('.context-heading h2').textContent;
    } catch {
      if (turn === generation) {
        showError(url, () => select(id, section, false));
        if (panelHadFocus) context.querySelector('button')?.focus({ preventScroll: true });
        status.textContent = 'Event preview could not be loaded.';
      }
    } finally {
      clearTimeout(timeout);
      if (turn === generation) context.removeAttribute('aria-busy');
    }
  };
  const closeContext = (push = true) => {
    ++generation;
    request?.abort();
    context.removeAttribute('aria-busy');
    if (push) {
      const url = new URL(location.href);
      url.searchParams.delete('selected_event');
      url.searchParams.delete('section');
      history.pushState({ ...history.state }, '', url);
    }
    markSelection('');
    if (emptyContext) context.innerHTML = emptyContext;
    else context.innerHTML = '<div class="context-empty"><h2>A closer look</h2><p>Select a show to see its dates, venue, and ticket details here.</p></div>';
    status.textContent = 'Event preview closed.';
    const trigger = [...document.querySelectorAll('.event-link')].find(link => link.dataset.id === lastFocusID);
    trigger?.focus({ preventScroll: true });
    if (!desktop.matches) scrollTo(0, resultsScroll);
  };
  document.addEventListener('click', event => {
    if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const close = event.target.closest('[data-close-context]');
    if (close) {
      event.preventDefault();
      closeContext();
      return;
    }
    const link = event.target.closest('[data-event-link], #event-context [data-panel-link]');
    if (!link) {
      const open = event.target.closest('#event-context a');
      if (open) {
        const url = new URL(open.href);
        if (url.origin === location.origin && url.pathname.startsWith('/events/')) leaveResults(open);
      }
      return;
    }
    const url = new URL(link.href);
    const id = link.dataset.id || decodeURIComponent(url.pathname.split('/').pop());
    lastFocusID = id;
    if (!desktop.matches && link.hasAttribute('data-event-link')) {
      leaveResults(link);
      return;
    }
    event.preventDefault();
    select(id, url.searchParams.get('section') || 'overview');
  });
  addEventListener('popstate', () => {
    const params = new URL(location.href).searchParams;
    const id = params.get('selected_event');
    if (id) select(id, params.get('section') || 'overview', false);
    else closeContext(false);
  });
  desktop.addEventListener('change', () => {
    if (!workspace.classList.contains('has-selection')) return;
    if (!desktop.matches) {
      resultsScroll = scrollY;
      scrollTo(0, 0);
      context.focus({ preventScroll: true });
    } else {
      scrollTo(0, resultsScroll);
    }
  });
})();
