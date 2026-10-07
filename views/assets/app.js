(() => {
  'use strict';

  const filters = document.getElementById('filters');
  const category = document.getElementById('category');
  const genre = document.getElementById('genre');
  if (category && genre) {
    category.addEventListener('change', () => {
      const options = Array.from(document.querySelectorAll('[data-category-genres]'))
        .find(template => template.dataset.categoryGenres === category.value);
      genre.replaceChildren(new Option('All genres', ''));
      if (options) genre.append(options.content.cloneNode(true));
      genre.disabled = category.value === 'all';
      category.form.elements.genre_category_id.value = category.value;
    });
  }
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

  // Ordinary forms remain the fallback. Enhanced saves preserve selection,
  // filters, focus and loaded results while synchronizing every visible control.
  const saveFeedback = (message, id, origin, error = false) => {
    const forms = saveForms(id);
    forms.forEach((form, index) => {
      const target = form.closest('[data-event-actions]')?.querySelector('[data-save-feedback]');
      if (!target) return;
      target.hidden = false;
      target.textContent = message;
      target.dataset.error = String(error);
      const announce = origin ? form === origin : index === 0;
      if (announce) target.setAttribute('role', 'status');
      else target.removeAttribute('role');
      target.setAttribute('aria-live', announce ? 'polite' : 'off');
    });
    if (!forms.length) {
      const target = document.getElementById('save-feedback');
      if (target) target.textContent = message;
    }
  };
  const saveForms = id => [...document.querySelectorAll('[data-save-form]')]
    .filter(form => !id || form.dataset.saveId === id);
  const updateSave = (id, saved, csrf) => {
    for (const form of saveForms(id)) {
      form.elements.action.value = saved ? 'remove' : 'save';
      if (csrf) form.elements.csrf_token.value = csrf;
      const button = form.querySelector('button');
      const label = saved ? 'Remove from Saved' : 'Save';
      button.dataset.saved = String(saved);
      button.setAttribute('aria-label', label + ' — ' + form.dataset.eventName);
      button.querySelector('span').textContent = label;
    }
  };
  let saveStateTurn = 0;
  const refreshSavedState = async () => {
    const ids = [...new Set(saveForms().map(form => form.dataset.saveId))];
    if (!ids.length) return;
    const turn = ++saveStateTurn;
    try {
      // Batch loaded rows; never make per-card SQL/provider requests.
      for (let offset = 0; offset < ids.length; offset += 100) {
        const params = new URLSearchParams();
        ids.slice(offset, offset + 100).forEach(id => params.append('event_id', id));
        const response = await fetch('/api/v1/me/saved-events/states?' + params);
        if (turn !== saveStateTurn) return;
        if (response.status === 401) {
          const state = { ...history.state };
          delete state.ticketopiaResults;
          history.replaceState(state, '', location.href);
          location.reload();
          return;
        }
        if (!response.ok) throw new Error('Saved state unavailable');
        const data = await response.json();
        if (turn !== saveStateTurn) return;
        ids.slice(offset, offset + 100).forEach(id => updateSave(id, Boolean(data.states[id]), data.csrf_token));
      }
    } catch {
      if (turn !== saveStateTurn) return;
      saveForms().forEach(form => { form.querySelector('button').disabled = true; });
      saveFeedback('Saved state is unavailable. Refresh this page to try again.');
    }
  };
  const saving = new Set();
  document.addEventListener('submit', async event => {
    const form = event.target.closest('[data-save-form]');
    if (!form) return;
    event.preventDefault();
    const id = form.dataset.saveId;
    if (saving.has(id)) return;
    saving.add(id);
    ++saveStateTurn;
    const trigger = form.querySelector('button');
    const hadFocus = document.activeElement === trigger;
    const controls = saveForms(id);
    controls.forEach(item => { item.setAttribute('aria-busy', 'true'); item.querySelector('button').disabled = true; });
    saveFeedback('Updating your private save…', id, form);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 12000);
    try {
      const response = await fetch(form.getAttribute('action'), {
        method: 'POST', body: new URLSearchParams(new FormData(form)),
        headers: { Accept: 'application/json' }, signal: controller.signal,
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.detail || 'Your save could not be updated. Try again.');
      updateSave(data.event_id, data.saved);
      saveFeedback(data.saved ? 'Event saved privately.' : 'Event removed from Saved.', id, form);
      if (location.pathname === '/saved') {
        const row = form.closest('.event-row');
        const adjacent = row?.nextElementSibling || row?.previousElementSibling;
        history.replaceState({ ...history.state, ticketopiaSavedAction: {
          focus: data.saved ? id : adjacent?.dataset.eventId || '',
          notice: form.dataset.eventName + (data.saved ? ' saved privately.' : ' removed from Saved.'),
        } }, '', location.href);
        location.reload();
      }
    } catch (error) {
      saveFeedback(error.name === 'AbortError' ? 'The save could not be confirmed. Try again; repeating it is safe.' : error.message, id, form, true);
    } finally {
      clearTimeout(timeout);
      saving.delete(id);
      controls.forEach(item => { item.removeAttribute('aria-busy'); item.querySelector('button').disabled = false; });
      if (hadFocus && trigger.isConnected && document.activeElement === document.body) trigger.focus({ preventScroll: true });
    }
  });
  if (location.pathname === '/saved' && history.state?.ticketopiaSavedAction) {
    const action = history.state.ticketopiaSavedAction;
    const state = { ...history.state };
    delete state.ticketopiaSavedAction;
    history.replaceState(state, '', location.href);
    const row = [...document.querySelectorAll('.event-row')].find(item => item.dataset.eventId === action.focus)
      || document.querySelector('.event-row');
    const target = row?.querySelector('[data-save-form] button') || document.querySelector('.destination-state a[href="/"]');
    const notice = document.getElementById('saved-notice');
    if (notice) notice.textContent = action.notice;
    requestAnimationFrame(() => target?.focus());
  }
  window.addEventListener('pageshow', event => {
    // Revalidate credentials and private state when returning from browser cache.
    if (event.persisted) refreshSavedState();
  });

  // Sidebar collapse and navigation customization (desktop shell).
  const sidebar = document.querySelector('[data-sidebar]');
  if (sidebar) {
    const navOrder = sidebar.querySelector('[data-nav-order]');
    const status = sidebar.querySelector('[data-nav-status]');
    const collapseKey = 'ticketopia-sidebar-collapsed-v1';
    const orderKey = 'ticketopia-nav-order-v1';
    const announce = message => {
      if (status) status.textContent = message;
    };

    const toggle = sidebar.querySelector('[data-sidebar-toggle]');
    if (toggle) {
      toggle.hidden = false;
      let collapsed = false;
      try {
        collapsed = localStorage.getItem(collapseKey) === 'true';
      } catch { /* Collapse simply starts expanded when storage is unavailable. */ }
      const applyCollapse = value => {
        document.body.classList.toggle('nav-collapsed', value);
        toggle.setAttribute('aria-expanded', String(!value));
        const label = value ? 'Expand sidebar' : 'Collapse sidebar';
        toggle.setAttribute('aria-label', label);
        toggle.title = label;
      };
      applyCollapse(collapsed);
      toggle.addEventListener('click', () => {
        collapsed = !collapsed;
        applyCollapse(collapsed);
        try {
          localStorage.setItem(collapseKey, String(collapsed));
        } catch { /* The toggle still works for this page view. */ }
        announce(collapsed ? 'Sidebar collapsed.' : 'Sidebar expanded.');
      });
    }

    if (navOrder) {
      const groupElements = () => [...navOrder.querySelectorAll('[data-nav-group]')];
      const itemElements = group => [...group.querySelectorAll('[data-nav-item]')];
      const initialDefault = {
        groups: groupElements().map(group => group.dataset.navGroup),
        items: Object.fromEntries(groupElements().map(group => [group.dataset.navGroup, itemElements(group).map(item => item.dataset.navItem)])),
      };
      // Keep surviving choices in order and append newly introduced destinations.
      // Duplicate or non-string IDs indicate a corrupt preference, not an order.
      const reconcileIDs = (saved, defaults) => {
        if (!Array.isArray(saved) || saved.some(id => typeof id !== 'string') || new Set(saved).size !== saved.length) return null;
        const known = saved.filter(id => defaults.includes(id));
        return [...known, ...defaults.filter(id => !known.includes(id))];
      };
      const readOrder = () => {
        let saved;
        try {
          saved = JSON.parse(localStorage.getItem(orderKey));
        } catch {
          return null;
        }
        if (!saved || !saved.items || typeof saved.items !== 'object' || Array.isArray(saved.items)) return null;
        const groups = reconcileIDs(saved.groups, initialDefault.groups);
        if (!groups) return null;
        const items = {};
        for (const group of initialDefault.groups) {
          items[group] = reconcileIDs(saved.items[group] ?? [], initialDefault.items[group]);
          if (!items[group]) return null;
        }
        return { groups, items };
      };
      const applyOrder = order => {
        const groupsById = Object.fromEntries(groupElements().map(group => [group.dataset.navGroup, group]));
        for (const id of order.groups) navOrder.append(groupsById[id]);
        for (const id of order.groups) {
          const itemsById = Object.fromEntries(itemElements(groupsById[id]).map(item => [item.dataset.navItem, item]));
          for (const itemId of order.items[id]) groupsById[id].append(itemsById[itemId]);
        }
      };
      const currentOrder = () => ({
        groups: groupElements().map(group => group.dataset.navGroup),
        items: Object.fromEntries(groupElements().map(group => [group.dataset.navGroup, itemElements(group).map(item => item.dataset.navItem)])),
      });
      const persistOrder = () => {
        try {
          localStorage.setItem(orderKey, JSON.stringify(currentOrder()));
          return true;
        } catch {
          return false;
        }
      };
      const savedOrder = readOrder();
      if (savedOrder) applyOrder(savedOrder);

      const moveItem = (item, direction) => {
        const sibling = direction === 'up' ? item.previousElementSibling : item.nextElementSibling;
        if (!sibling || !sibling.matches('[data-nav-item]')) {
          announce('Already at the ' + (direction === 'up' ? 'top' : 'bottom') + ' of this section.');
          return;
        }
        if (direction === 'up') item.parentElement.insertBefore(item, sibling);
        else item.parentElement.insertBefore(sibling, item);
        announce(item.querySelector('.nav-label').textContent + ' moved ' + direction + '.');
      };
      const moveGroup = (group, direction) => {
        const sibling = direction === 'up' ? group.previousElementSibling : group.nextElementSibling;
        if (!sibling || !sibling.matches('[data-nav-group]')) {
          announce('This section is already ' + (direction === 'up' ? 'first' : 'last') + '.');
          return;
        }
        if (direction === 'up') navOrder.insertBefore(group, sibling);
        else navOrder.insertBefore(sibling, group);
        announce(group.querySelector('.nav-group-label').textContent + ' section moved ' + direction + '.');
      };
      sidebar.addEventListener('click', event => {
        const itemButton = event.target.closest('[data-move-item]');
        if (itemButton) {
          moveItem(itemButton.closest('[data-nav-item]'), itemButton.dataset.moveItem);
          itemButton.focus({ preventScroll: true });
          return;
        }
        const groupButton = event.target.closest('[data-move-group]');
        if (groupButton) {
          moveGroup(groupButton.closest('[data-nav-group]'), groupButton.dataset.moveGroup);
          groupButton.focus({ preventScroll: true });
        }
      });

      const customize = sidebar.querySelector('[data-nav-customize]');
      const actions = sidebar.querySelector('[data-nav-customize-actions]');
      if (customize && actions) {
        customize.hidden = false;
        let customizing = false;
        let snapshot = null;
        const setCustomizing = value => {
          customizing = value;
          sidebar.dataset.customizing = String(value);
          customize.setAttribute('aria-pressed', String(value));
          actions.hidden = !value;
          if (toggle) toggle.disabled = value;
          if (value) navOrder.setAttribute('tabindex', '0');
          else navOrder.removeAttribute('tabindex');
          for (const handle of sidebar.querySelectorAll('[data-nav-drag], [data-nav-group-drag]')) {
            handle.draggable = value;
          }
        };
        customize.addEventListener('click', () => {
          if (customizing) return;
          snapshot = currentOrder();
          setCustomizing(true);
          announce('Customizing navigation. Use the move buttons or drag items to reorder.');
        });
        actions.querySelector('[data-nav-customize-done]').addEventListener('click', () => {
          const saved = persistOrder();
          setCustomizing(false);
          announce(saved ? 'Navigation order saved in this browser.' : 'Navigation reordered for this page. Browser storage is unavailable, so it cannot be remembered.');
          customize.focus();
        });
        const cancelCustomization = (restoreFocus = true) => {
          if (snapshot) applyOrder(snapshot);
          setCustomizing(false);
          announce('Navigation changes canceled.');
          if (restoreFocus) customize.focus();
        };
        actions.querySelector('[data-nav-customize-cancel]').addEventListener('click', () => cancelCustomization());
        actions.querySelector('[data-nav-customize-reset]').addEventListener('click', () => {
          applyOrder(initialDefault);
          announce('Default order restored. Choose Done to save, or Cancel to keep your previous order.');
        });
        sidebar.addEventListener('keydown', event => {
          if (customizing && event.key === 'Escape') {
            event.preventDefault();
            cancelCustomization();
          }
        });
        matchMedia('(min-width: 72rem)').addEventListener('change', event => {
          if (!event.matches && customizing) cancelCustomization(false);
        });

        let dragged = null;
        const clearDropMarks = () => {
          for (const el of sidebar.querySelectorAll('.drop-before, .drop-after')) {
            el.classList.remove('drop-before', 'drop-after');
          }
        };
        sidebar.addEventListener('dragstart', event => {
          if (!customizing) {
            event.preventDefault();
            return;
          }
          const itemHandle = event.target.closest('[data-nav-drag]');
          const groupHandle = event.target.closest('[data-nav-group-drag]');
          if (!itemHandle && !groupHandle) {
            event.preventDefault();
            return;
          }
          dragged = itemHandle
            ? { type: 'item', el: itemHandle.closest('[data-nav-item]') }
            : { type: 'group', el: groupHandle.closest('[data-nav-group]') };
          dragged.el.classList.add('nav-dragging');
          event.dataTransfer.effectAllowed = 'move';
          event.dataTransfer.setData('text/plain', 'ticketopia-navigation');
          try {
            event.dataTransfer.setDragImage(dragged.el, 16, 16);
          } catch { /* A default drag image is fine. */ }
        });
        sidebar.addEventListener('dragover', event => {
          if (!dragged) return;
          const selector = dragged.type === 'item' ? '[data-nav-item]' : '[data-nav-group]';
          const over = event.target.closest(selector);
          clearDropMarks();
          if (!over || over === dragged.el) return;
          if (dragged.type === 'item' && over.parentElement !== dragged.el.parentElement) return;
          if (dragged.type === 'group' && over.parentElement !== navOrder) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = 'move';
          const rect = over.getBoundingClientRect();
          const before = event.clientY < rect.top + rect.height / 2;
          over.classList.add(before ? 'drop-before' : 'drop-after');
        });
        sidebar.addEventListener('drop', event => {
          if (!dragged) return;
          const selector = dragged.type === 'item' ? '[data-nav-item]' : '[data-nav-group]';
          const over = event.target.closest(selector);
          if (over && over !== dragged.el && over.parentElement === dragged.el.parentElement && (over.classList.contains('drop-before') || over.classList.contains('drop-after'))) {
            event.preventDefault();
            const before = over.classList.contains('drop-before');
            over.parentElement.insertBefore(dragged.el, before ? over : over.nextElementSibling);
            const name = dragged.type === 'item'
              ? dragged.el.querySelector('.nav-label').textContent
              : dragged.el.querySelector('.nav-group-label').textContent + ' section';
            announce(name + ' moved.');
          }
          clearDropMarks();
        });
        sidebar.addEventListener('dragend', () => {
          if (dragged) dragged.el.classList.remove('nav-dragging');
          dragged = null;
          clearDropMarks();
        });
      }
    }
  }

  for (const name of ['htmx:responseError', 'htmx:sendError', 'htmx:timeout']) {
    document.body.addEventListener(name, () => {
      const message = document.getElementById('load-error');
      if (message) message.textContent = 'More events could not be loaded. Please try again shortly.';
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
        feedback.textContent = "We couldn't detect your city. Enter a city above to find events.";
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
  const previewTemplate = name => document.getElementById('preview-' + name + '-template').content.cloneNode(true);
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
  const detailURL = (id, section = 'overview', recommend = false) => {
    const url = new URL('/events/' + encodeURIComponent(id), location.origin);
    url.searchParams.set('return_to', workspace.dataset.resultsUrl);
    if (section !== 'overview') url.searchParams.set('section', section);
    if (recommend) url.searchParams.set('recommend', 'true');
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
      csrf: document.querySelector('[data-save-form] input[name="csrf_token"]')?.value || '',
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
    const csrf = document.querySelector('[data-save-form] input[name="csrf_token"]')?.value || '';
    if ((saved.csrf || '') !== csrf) return; // Never restore another session's private controls.
    list.innerHTML = saved.list;
    document.getElementById('pagination').outerHTML = saved.pagination;
    window.htmx?.process(list);
    window.htmx?.process(document.getElementById('pagination'));
    refreshSavedState();
    document.dispatchEvent(new Event('ticketopia:state-restored'));
    markSelection(new URL(location.href).searchParams.get('selected_event'));
    requestAnimationFrame(() => {
      scrollTo(0, saved.scroll);
      const row = [...list.children].find(item => item.dataset.eventId === saved.focus);
      row?.querySelector('.event-link')?.focus({ preventScroll: true });
    });
  };
  window.addEventListener('pageshow', restoreResults);

  const showError = (url, retry) => {
    context.replaceChildren(previewTemplate('error'));
    const button = context.querySelector('[data-preview-retry]');
    button.hidden = false;
    button.addEventListener('click', retry);
    const link = context.querySelector('[data-preview-open]');
    link.href = url.href;
  };
  const select = async (id, section, push = true, recommend = false) => {
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
      if (recommend) next.searchParams.set('recommend', 'true');
      else next.searchParams.delete('recommend');
      history.pushState({ ...history.state }, '', next);
    }
    markSelection(id);
    const url = detailURL(id, section, recommend);
    context.setAttribute('aria-busy', 'true');
    context.replaceChildren(previewTemplate('loading'));
    status.textContent = 'Loading event preview.';
    const timeout = setTimeout(() => currentRequest.abort(), 12000);
    try {
      const response = await fetch(url, { signal: currentRequest.signal, headers: { 'X-Ticketopia-Panel': 'true' } });
      if (!response.ok) throw new Error('Event unavailable');
      const html = await response.text();
      if (turn !== generation) return;
      context.innerHTML = html;
      context.scrollTop = 0;
      if (recommend) context.querySelector('[data-recommendation-form] textarea')?.focus();
      else if (panelHadFocus) context.querySelector('.section-nav a[aria-current="page"]')?.focus({ preventScroll: true });
      status.textContent = 'Event preview loaded: ' + context.querySelector('.context-heading h2').textContent;
    } catch {
      if (turn === generation) {
        showError(url, () => select(id, section, false, recommend));
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
      url.searchParams.delete('recommend');
      history.pushState({ ...history.state }, '', url);
    }
    markSelection('');
    context.replaceChildren(previewTemplate('empty'));
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
    select(id, url.searchParams.get('section') || 'overview', true, url.searchParams.get('recommend') === 'true');
  });
  addEventListener('popstate', () => {
    const params = new URL(location.href).searchParams;
    const id = params.get('selected_event');
    if (id) select(id, params.get('section') || 'overview', false, params.get('recommend') === 'true');
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
