(() => {
  'use strict';

  // Native timing forms are authoritative. Enhancement only reveals Custom and
  // previews the target; it never changes consent or promises a scheduled send.
  for (const form of document.querySelectorAll('[data-reminder-timing]')) {
    const choice = form.elements.timing;
    const custom = form.elements.lead_minutes;
    const field = form.querySelector('.reminder-custom');
    const preview = form.querySelector('[data-reminder-preview]');
    const update = () => {
      const isCustom = choice.value === 'custom';
      field.hidden = !isCustom;
      custom.disabled = !isCustom;
      custom.required = isCustom;
      const value = isCustom ? custom.value : choice.value;
      const lead = Number(value);
      if (!['30', '60', '1440', 'custom'].includes(choice.value) || !/^\d+$/.test(value) || !Number.isInteger(lead) || lead < 1 || lead > 10080) {
        preview.textContent = 'Choose 1–10080 whole minutes to preview the target.';
        return;
      }
      if (['cancelled', 'canceled', 'postponed'].includes(form.dataset.eventStatus)) {
        preview.textContent = 'The event is cancelled or postponed; delivery is withheld.';
        return;
      }
      if (!form.dataset.saleAt) {
        preview.textContent = 'Pending a known public-sale time; no target is scheduled yet.';
        return;
      }
      const sale = new Date(form.dataset.saleAt);
      const now = Date.now();
      if (sale.getTime() <= now) {
        preview.textContent = 'Public tickets are already on sale; no late reminder will be sent.';
        return;
      }
      const due = new Date(sale.getTime() - lead * 60000);
      if (due.getTime() <= now) {
        preview.textContent = 'This lead time has already elapsed. A future sale can be eligible immediately after saving, subject to preferences and fresh evidence.';
        return;
      }
      try {
        const time = new Intl.DateTimeFormat(undefined, {timeZone: form.dataset.timezone,
          year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZoneName: 'short'}).format(due);
        preview.textContent = `Target: ${time} (${form.dataset.timezone}). Delivery depends on preferences, fresh event details and email service availability.`;
      } catch {
        preview.textContent = `Target: ${due.toISOString()} (UTC). Delivery depends on preferences, fresh event details and email service availability.`;
      }
    };
    choice.addEventListener('change', update);
    custom.addEventListener('input', update);
    update();
  }
  if (document.querySelector('.reminders-page, .account-preferences')) {
    window.addEventListener('pageshow', event => {
      if (event.persisted) location.reload(); // Revalidate owner/session-specific forms.
    });
  }

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
  const saveFeedback = (message, id, origin, error = false, signIn = false) => {
    const forms = saveForms(id);
    forms.forEach((form, index) => {
      const target = form.closest('[data-event-actions]')?.querySelector('[data-save-feedback]');
      if (!target) return;
      target.hidden = false;
      target.textContent = message;
      if (signIn && form.dataset.signInUrl) {
        const link = document.createElement('a');
        link.href = form.dataset.signInUrl;
        link.className = 'text-link'; link.textContent = 'Sign in to continue';
        target.append(document.createTextNode(' '), link);
      }
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
    for (const reminder of document.querySelectorAll('[data-reminder-id]')) {
      if (reminder.dataset.reminderId === id) reminder.hidden = !saved;
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
  // Save privacy is explained on the first Save in this browser rather than
  // above every result list. Storage failure falls back to page memory.
  const savePrivacyKey = 'ticketopia-save-privacy-seen';
  let savePrivacySeen = false;
  const savedMessage = () => {
    let seen = savePrivacySeen;
    savePrivacySeen = true;
    try {
      seen = seen || localStorage.getItem(savePrivacyKey) === 'true';
      localStorage.setItem(savePrivacyKey, 'true');
    } catch { /* Page memory still prevents repeating it on this page. */ }
    return seen ? 'Event saved privately.' : 'Event saved privately. Only you can see your Saved events; saving doesn\'t publish interest or reserve tickets.';
  };
  const focusCollectionControl = target => {
    if (!target) return;
    target.focus({ preventScroll: true });
    target.scrollIntoView({ block: 'nearest' });
    // Enlarged labels can make the sticky navigation taller than the root's
    // ordinary scroll padding. Keep the restored action above its actual edge.
    const nav = document.querySelector('.bottom-nav')?.getBoundingClientRect();
    const edge = nav?.width ? nav.top : innerHeight;
    const rect = target.getBoundingClientRect();
    if (rect.bottom > edge) scrollBy(0, rect.bottom - edge + 16);
  };
  // Keep surviving collection rows mounted: removal should not refill the page,
  // reset its reading position, or discard focus elsewhere during a slow write.
  document.addEventListener('ticketopia:collection-action', event => {
    const { form, notice, selected = false } = event.detail;
    const row = form.closest('.event-row');
    const adjacent = row?.nextElementSibling || row?.previousElementSibling;
    const shouldFocus = row?.contains(document.activeElement) || document.activeElement === document.body;
    const anchorTop = adjacent?.getBoundingClientRect().top;
    const isSaved = location.pathname === '/saved';
    const control = isSaved ? '[data-save-form] button' : '.interest-button';
    const noticeTarget = document.querySelector(isSaved ? '#saved-notice' : '[data-interest-notice]');
    if (!selected && adjacent) {
      row.remove();
      if (noticeTarget) noticeTarget.textContent = notice;
      scrollBy(0, adjacent.getBoundingClientRect().top - anchorTop);
      if (shouldFocus) {
        const button = adjacent.querySelector(control);
        focusCollectionControl(button?.disabled ? adjacent.querySelector('.event-link') : button);
      }
      return;
    }
    // The server owns empty/error guidance and pagination. Only reload when no
    // rows survive (or a stale collection row is explicitly saved again).
    history.replaceState({ ...history.state, ticketopiaCollectionAction: {
      path: location.pathname, notice, focus: selected ? row?.dataset.eventId : '',
      shouldFocus, scrollY,
    } }, '', location.href);
    location.reload();
  });
  const collectionAction = history.state?.ticketopiaCollectionAction;
  if (collectionAction?.path === location.pathname) {
    const state = { ...history.state };
    delete state.ticketopiaCollectionAction;
    history.replaceState(state, '', location.href);
    const isSaved = location.pathname === '/saved';
    const notice = document.querySelector(isSaved ? '#saved-notice' : '[data-interest-notice]');
    if (notice) notice.textContent = collectionAction.notice;
    requestAnimationFrame(() => {
      scrollTo(0, collectionAction.scrollY);
      if (!collectionAction.shouldFocus) return;
      const row = [...document.querySelectorAll('.event-row')].find(item => item.dataset.eventId === collectionAction.focus)
        || document.querySelector('.event-row');
      const target = row?.querySelector(isSaved ? '[data-save-form] button' : '.interest-button')
        || document.querySelector('[data-collection-recovery]')
        || document.querySelector('.destination-state a[href="/"], [data-interest-empty]')
        || document.querySelector('#main h1');
      if (target?.tagName === 'H1') target.tabIndex = -1;
      focusCollectionControl(target);
    });
  }
  document.addEventListener('submit', async event => {
    const form = event.target.closest('[data-save-form]');
    if (!form) return;
    event.preventDefault();
    const id = form.dataset.saveId;
    if (saving.has(id)) return;
    saving.add(id);
    ++saveStateTurn;
    const trigger = form.querySelector('button');
    const csrf = form.elements.csrf_token.value;
    const hadFocus = document.activeElement === trigger;
    const controls = saveForms(id);
    controls.forEach(item => { item.setAttribute('aria-busy', 'true'); item.querySelector('button').disabled = true; });
    saveFeedback('Updating your private save…', id, form);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 12000);
    let signIn = false;
    try {
      const response = await fetch(form.getAttribute('action'), {
        method: 'POST', body: new URLSearchParams(new FormData(form)),
        headers: { Accept: 'application/json' }, signal: controller.signal,
      });
      const data = await response.json();
      signIn = response.status === 401;
      if (!response.ok) throw new Error(data.detail || 'Your save could not be updated. Try again.');
      if (!saveForms(id).some(item => item.elements.csrf_token.value === csrf)) return;
      updateSave(data.event_id, data.saved);
      saveFeedback(data.saved ? savedMessage() : 'Event removed from Saved.', id, form);
      if (location.pathname === '/saved') {
        document.dispatchEvent(new CustomEvent('ticketopia:collection-action', { detail: {
          form, selected: data.saved,
          notice: form.dataset.eventName + (data.saved ? ' saved privately.' : ' removed from Saved.'),
        } }));
      }
    } catch (error) {
      if (saveForms(id).some(item => item.elements.csrf_token.value === csrf)) saveFeedback(['AbortError', 'TypeError', 'SyntaxError'].includes(error.name) ? 'The save could not be confirmed. Try again; repeating it is safe.' : error.message, id, form, true, signIn);
    } finally {
      clearTimeout(timeout);
      saving.delete(id);
      controls.forEach(item => { item.removeAttribute('aria-busy'); item.querySelector('button').disabled = false; });
      if (hadFocus && trigger.isConnected && document.activeElement === document.body) trigger.focus({ preventScroll: true });
    }
  });
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
  const collectionPaths = new Set(['/saved', '/me/interests', '/radar', '/happening-now']);
  const isCollectionPath = path => collectionPaths.has(path) || path === '/community' || path === '/community/discussions' || /^\/users\/[^/]+$/.test(path);
  const validJourney = journey => {
    try {
      const origin = new URL(journey.origin);
      return origin.origin === location.origin && (origin.pathname === '/' || isCollectionPath(origin.pathname)) && Number.isInteger(journey.steps) && journey.steps > 0 && journey.steps < history.length;
    } catch { return false; }
  };

  const workspace = document.querySelector('[data-results-url]');
  // One request generation owns full selections and narrow same-event reads.
  let request;
  let generation = 0;
  let renderedURL = location.href;
  const sectionNames = { overview: 'Overview', discussion: 'Questions & tips', community: 'Recommendations' };
  const sectionHost = () => document.querySelector('[data-event-shell]');
  const sectionRegion = () => sectionHost()?.querySelector('[data-event-section-region]');
  const rememberSection = () => {
    const region = sectionRegion();
    if (!region || renderedURL !== location.href) return;
    const active = document.activeElement;
    history.replaceState({ ...history.state, ticketopiaSection: {
      url: location.href, documentScroll: scrollY,
      scroll: document.getElementById('event-context')?.scrollTop || 0,
      section: active?.dataset.eventSection || '',
      draft: active?.closest('[data-discussion-draft]')?.dataset.discussionDraft || '',
      recommendation: Boolean(active?.closest('[data-recommendation-form]')),
      focus: sectionHost().contains(active) ? active.id : '',
    } }, '', location.href);
  };
  let scrollFrame;
  const recordSectionPosition = () => {
    if (scrollFrame) return;
    scrollFrame = requestAnimationFrame(() => { scrollFrame = 0; rememberSection(); });
  };
  addEventListener('scroll', recordSectionPosition, true);
  document.addEventListener('focusin', recordSectionPosition);
  addEventListener('pagehide', rememberSection);
  // Reconcile server facts/permissions without unmounting unchanged controls.
  // Native disclosure state belongs to the reader, not to each fragment read.
  const reconcile = (old, fresh) => {
    if (old.isEqualNode(fresh) && !old.querySelector?.('input[type="hidden"]')) {
      // Enhanced actions change live hidden-input values without changing their
      // serialized attributes. Fresh return/CSRF/action values still win.
      if (old.matches?.('input[type="hidden"]')) old.value = fresh.value;
      return;
    }
    if (old.nodeType !== fresh.nodeType || old.nodeName !== fresh.nodeName) {
      old.replaceWith(fresh.cloneNode(true));
      return;
    }
    if (old.nodeType === Node.TEXT_NODE) { old.textContent = fresh.textContent; return; }
    if (old.nodeType !== Node.ELEMENT_NODE) return;
    for (const attr of [...old.attributes]) {
      if (old.matches('details') && attr.name === 'open') continue;
      if (!fresh.hasAttribute(attr.name)) old.removeAttribute(attr.name);
    }
    for (const attr of fresh.attributes) {
      if (old.matches('details') && attr.name === 'open') continue;
      if (old.getAttribute(attr.name) !== attr.value) old.setAttribute(attr.name, attr.value);
    }
    if (old.matches('input[type="hidden"]')) old.value = fresh.value;
    const children = [...fresh.childNodes];
    for (let i = 0; i < children.length; i++) {
      if (!old.childNodes[i]) old.append(children[i].cloneNode(true));
      else reconcile(old.childNodes[i], children[i]);
    }
    while (old.childNodes.length > children.length) old.lastChild.remove();
  };
  const openRecommendation = host => {
    const editor = host.querySelector('[data-recommendation-editor]');
    if (editor) editor.open = true;
    (editor?.querySelector('textarea') || host.querySelector('[data-event-recommendations] a[href^="/auth/sign-in"]'))?.focus();
  };
  const sectionError = (region, section, url, next, retry, retain) => {
    const message = region.querySelector('[data-section-status]');
    message.classList.remove('sr-only');
    message.hidden = false;
    message.replaceChildren(document.createTextNode(sectionNames[section] + ' could not be loaded.' + (retain ? ' Still showing ' + sectionNames[region.dataset.section] + '. ' : ' ')));
    const button = document.createElement('button');
    button.type = 'button'; button.className = 'button-quiet'; button.textContent = 'Retry';
    button.dataset.sectionRetry = ''; button.addEventListener('click', retry);
    const link = document.createElement('a');
    link.href = url.href; link.className = 'text-link'; link.textContent = 'Open ' + sectionNames[section];
    message.append(button, document.createTextNode(' '), link);
    if (!retain) {
      region.querySelector('[data-section-body]').replaceChildren();
      region.dataset.section = section;
      region.dataset.sectionThread = '';
      region.dataset.actionReturn = next.pathname + next.search;
      for (const input of sectionHost().querySelectorAll('input[name="return_to"]')) input.value = region.dataset.actionReturn;
      if (region.dataset.presentation === 'context') {
        sectionHost().dataset.contextSection = section;
        sectionHost().dataset.contextThread = '';
      }
      const recommend = sectionHost().querySelector('[data-recommend-intent]');
      if (recommend) recommend.hidden = section === 'community';
      for (const item of sectionHost().querySelectorAll('[data-event-section]')) {
        if (item.dataset.eventSection === section) item.setAttribute('aria-current', 'page');
        else item.removeAttribute('aria-current');
      }
    }
  };
  const switchSection = async (url, next, push = true, recommend = false, confirmation = null, force = false) => {
    const host = sectionHost();
    const region = sectionRegion();
    const section = url.pathname.includes('/discussions') ? 'discussion' : url.searchParams.get('section') || 'overview';
    const panel = region.dataset.presentation === 'context';
    const activated = document.activeElement;
    const saved = !push && !confirmation && history.state?.ticketopiaSection?.url === location.href ? history.state.ticketopiaSection : null;
    request?.abort();
    const turn = ++generation;
    if (!force && push && region.dataset.section === section && !region.dataset.sectionThread && !url.pathname.includes('/discussions') && !region.querySelector('[data-section-retry]') && (!recommend || new URL(location.href).searchParams.get('recommend') === 'true')) {
      region.removeAttribute('aria-busy');
      region.querySelector('[data-section-status]').hidden = true;
      if (recommend) openRecommendation(host);
      return;
    }
    if (push) rememberSection();
    // Existing draft mechanisms also retain programmatic reply/key changes and
    // in-memory drafts when browser storage is blocked.
    document.dispatchEvent(new Event('ticketopia:remember-drafts'));
    request = new AbortController();
    const currentRequest = request;
    const timeout = setTimeout(() => currentRequest.abort(), 12000);
    const message = region.querySelector('[data-section-status]');
    const previousSection = region.dataset.section;
    message.hidden = false;
    message.classList.remove('sr-only');
    message.textContent = 'Loading ' + sectionNames[section] + '. Still showing ' + sectionNames[previousSection] + '.';
    region.setAttribute('aria-busy', 'true');
    try {
      const headers = { 'X-Ticketopia-Section': 'true' };
      if (panel) headers['X-Ticketopia-Panel'] = 'true';
      const response = await fetch(url, { signal: currentRequest.signal, headers, credentials: 'same-origin' });
      if (turn !== generation || !host.isConnected) return;
      if (response.status === 401 || response.redirected) {
        for (const form of host.querySelectorAll('[data-save-form], [data-interest-form], [data-discussion-form], [data-recommendation-form]')) {
          const link = document.createElement('a');
          link.href = url.href; link.className = 'text-link'; link.textContent = 'Reload to check your account';
          form.replaceWith(link);
        }
        host.querySelectorAll('[data-reminder-id]').forEach(link => link.remove());
      }
      if (!response.ok || response.redirected || !response.headers.get('Content-Type')?.includes('text/html')) throw new Error('Section unavailable');
      const parsed = new DOMParser().parseFromString(await response.text(), 'text/html');
      if (turn !== generation || !host.isConnected) return;
      const fresh = parsed.body.querySelector(':scope > [data-event-section-region]');
      const metadata = parsed.body.querySelector(':scope > template[data-event-shell-state]');
      const shell = metadata?.content.querySelector('[data-event-shell]');
      const thread = url.pathname.includes('/discussions/') ? decodeURIComponent(url.pathname.split('/')[4]) : '';
      if (parsed.body.children.length !== 2 || !fresh || !shell?.hasAttribute('data-viewer-key') || fresh.dataset.eventId !== host.dataset.eventShell || shell.dataset.eventShell !== host.dataset.eventShell || fresh.dataset.section !== section || fresh.dataset.presentation !== region.dataset.presentation || fresh.dataset.sectionThread !== thread || !fresh.dataset.actionReturn || fresh.querySelector('script')) throw new Error('Unrecognized section');
      for (const selector of ['[data-shell-facts]', '.event-primary-actions', '.event-section-nav', '.event-participation', '.event-provenance']) {
        const old = host.querySelector(selector), replacement = shell.querySelector(selector);
        if (!old || !replacement) throw new Error('Missing event controls');
      }
      // Reconcile all sensitive state, including signed-out controls, fresh CSRF,
      // permissions and warnings. No private fragment HTML enters browser storage.
      if (host.dataset.viewerKey !== shell.dataset.viewerKey) {
        ++saveStateTurn;
        document.dispatchEvent(new Event('ticketopia:viewer-changed'));
        // Loaded result rows belong to the previous session too. Withhold their
        // private controls until an ordinary, fresh results read reconciles them.
        for (const group of document.querySelectorAll('[data-event-actions], [data-interest-details]')) {
          if (host.contains(group)) continue;
          const reload = document.createElement('a');
          reload.href = workspace?.dataset.resultsUrl || location.href;
          reload.className = 'text-link'; reload.textContent = 'Reload account controls';
          group.replaceChildren(reload);
        }
        const state = { ...history.state }; delete state.ticketopiaResults;
        history.replaceState(state, '', location.href);
      }
      host.dataset.viewerKey = shell.dataset.viewerKey;
      for (const selector of ['[data-shell-facts]', '.event-primary-actions', '.event-section-nav', '.event-participation', '.event-provenance']) reconcile(host.querySelector(selector), shell.querySelector(selector));
      if (!panel) reconcile(host.querySelector('.event-identity > .event-image'), shell.querySelector('.event-image'));
      region.querySelector('[data-section-body]').replaceChildren(...fresh.querySelector('[data-section-body]').childNodes);
      Object.assign(region.dataset, fresh.dataset);
      if (panel) {
        host.dataset.contextSection = section;
        host.dataset.contextThread = thread;
      }
      if (push) {
        const state = { ...history.state };
        delete state.ticketopiaSection; delete state.ticketopiaPanel;
        if (validJourney(state.ticketopiaReturn)) state.ticketopiaReturn = { ...state.ticketopiaReturn, steps: state.ticketopiaReturn.steps + 1 };
        history.pushState(state, '', next);
      }
      renderedURL = location.href;
      window.htmx?.process(host);
      document.dispatchEvent(new Event('ticketopia:section-loaded'));
      message.hidden = false;
      message.classList.add('sr-only');
      message.textContent = confirmation?.notice || sectionNames[section] + ' loaded.';
      await new Promise(resolve => requestAnimationFrame(() => {
        if (turn !== generation) { resolve(); return; }
        if (saved) {
          const context = document.getElementById('event-context');
          if (context) context.scrollTop = saved.scroll;
          scrollTo(0, saved.documentScroll);
          let target = [...host.querySelectorAll('[data-discussion-draft]')].find(form => form.dataset.discussionDraft === saved.draft)?.querySelector('textarea');
          if (saved.recommendation) target = host.querySelector('[data-recommendation-form] textarea');
          target ||= [...host.querySelectorAll('[data-event-section]')].find(link => link.dataset.eventSection === saved.section);
          target ||= saved.focus ? document.getElementById(saved.focus) : null;
          if (target) {
            const disclosure = target.closest('details'); if (disclosure) disclosure.open = true;
            target.focus({ preventScroll: true });
          }
        } else if (recommend) openRecommendation(host);
        else if (confirmation) {
          const post = [...region.querySelectorAll('.discussion-post')].find(item => item.id === 'post-' + confirmation.post);
          let target = post;
          if (confirmation.action === 'helpful' || confirmation.action === 'unhelpful') target = post?.querySelector('button[aria-pressed]');
          else if (confirmation.action === 'edit') target = post?.querySelector('.discussion-editor > summary');
          if (target) { if (target === post) target.tabIndex = -1; target.focus({ preventScroll: true }); }
          if (confirmation.action !== 'create') {
            if (panel) document.getElementById('event-context').scrollTop = confirmation.scroll;
            scrollTo(0, confirmation.documentScroll);
          }
        } else if (url.hash === '#discussion-composer') (region.querySelector('#discussion-composer textarea') || region.querySelector('#discussion-composer a[href^="/auth/sign-in"]'))?.focus();
        else {
          // Persistent links usually keep focus themselves. Do not reclaim it
          // after the reader has already moved to a newly loaded control.
          if (activated.isConnected && host.contains(activated) && document.activeElement === document.body) activated.focus({ preventScroll: true });
          const nav = host.querySelector('.event-section-nav');
          const rect = nav.getBoundingClientRect();
          const bounds = panel && matchMedia('(min-width: 62rem)').matches ? document.getElementById('event-context').getBoundingClientRect() : { top: 0, bottom: innerHeight };
          if (rect.bottom < bounds.top || rect.top > bounds.bottom) nav.scrollIntoView({ block: 'nearest' });
        }
        rememberSection();
        resolve();
      }));
    } catch {
      if (turn !== generation || !host.isConnected) return;
      sectionError(region, section, url, next, () => switchSection(url, next, push, recommend, confirmation, true), push || Boolean(confirmation));
    } finally {
      clearTimeout(timeout);
      if (turn === generation) region.removeAttribute('aria-busy');
    }
  };
  if (!workspace) {
    if (isCollectionPath(location.pathname)) {
      document.addEventListener('click', event => {
        if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        const link = event.target.closest('[data-event-link]');
        if (!link) return;
        // Artwork links leave the tab order, so they restore focus to the title.
        history.replaceState({ ...history.state, ticketopiaCollection: {
          url: location.href, scroll: scrollY, focus: link.dataset.id,
          linkIndex: [...document.querySelectorAll('[data-event-link]:not([tabindex="-1"])')].filter(item => item.dataset.id === link.dataset.id).indexOf(link),
        } }, '', location.href);
        stageReturn(new URL(link.href), { origin: location.href, steps: 1 });
      });
      window.addEventListener('pageshow', () => {
        const saved = history.state?.ticketopiaCollection;
        if (saved?.url !== location.href) return;
        requestAnimationFrame(() => {
          scrollTo(0, saved.scroll);
          const links = [...document.querySelectorAll('[data-event-link]:not([tabindex="-1"])')].filter(link => link.dataset.id === saved.focus);
          (links[saved.linkIndex] || links.find(link => link.matches('.event-link')) || links[0])?.focus({ preventScroll: true });
        });
      });
    }
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
      const link = event.target.closest('a[href]');
      if (!link) return;
      const url = new URL(link.href);
      const host = sectionHost();
      if (host && host.contains(link) && link.matches('[data-panel-link]') && url.origin === location.origin && url.pathname === location.pathname) {
        event.preventDefault();
        switchSection(url, url, true, url.searchParams.get('recommend') === 'true');
        return;
      }
      if (url.href === location.href) {
        event.preventDefault();
        return;
      }
      const journey = history.state?.ticketopiaReturn;
      if (url.origin === location.origin && url.pathname.startsWith('/events/') && validJourney(journey)) stageReturn(url, { ...journey, steps: journey.steps + 1 });
    });
    addEventListener('popstate', () => {
      if (!sectionRegion()) return;
      const url = new URL(location.href);
      switchSection(url, url, false, url.searchParams.get('recommend') === 'true', null, true);
    });
    addEventListener('pageshow', event => {
      if (event.persisted && sectionRegion()) {
        const url = new URL(location.href);
        switchSection(url, url, false, url.searchParams.get('recommend') === 'true', null, true);
      }
    });
    document.addEventListener('ticketopia:discussion-saved', event => {
      if (!sectionRegion()?.contains(event.target)) return;
      const url = new URL(location.href);
      const target = new URL(event.detail.location, location.origin);
      if (event.detail.action === 'create' && target.pathname.includes('/discussions/')) return;
      event.preventDefault();
      switchSection(url, url, false, false, { ...event.detail, documentScroll: scrollY }, true);
    });
    return;
  }

  const context = document.getElementById('event-context');
  const previewTemplate = name => document.getElementById('preview-' + name + '-template').content.cloneNode(true);
  const status = document.getElementById('selection-status');
  const desktop = matchMedia('(min-width: 62rem)');
  let lastFocusID = new URL(location.href).searchParams.get('selected_event') || '';
  let resultsScroll = history.state?.ticketopiaResults?.scroll ?? scrollY;

  const rememberPanel = () => {
    const active = document.activeElement;
    history.replaceState({ ...history.state, ticketopiaPanel: {
      url: location.href, scroll: context.scrollTop, documentScroll: scrollY,
      draft: active?.closest('[data-discussion-draft]')?.dataset.discussionDraft || '',
    } }, '', location.href);
  };
  const restorePanel = saved => {
    context.scrollTop = saved.scroll;
    if (!desktop.matches) scrollTo(0, saved.documentScroll);
    const draft = [...context.querySelectorAll('[data-discussion-draft]')].find(form => form.dataset.discussionDraft === saved.draft);
    if (draft) {
      const disclosure = draft.closest('details');
      if (disclosure) disclosure.open = true;
      draft.querySelector('textarea')?.focus({ preventScroll: true });
    }
  };

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
      scroll: desktop.matches || !workspace.classList.contains('has-selection') ? scrollY : resultsScroll,
      focus: lastFocusID,
      csrf: document.querySelector('[data-save-form] input[name="csrf_token"]')?.value || '',
    } }, '', location.href);
  };
  const leaveResults = link => {
    rememberPanel();
    rememberResults();
    const url = new URL(link.href);
    url.searchParams.set('return_to', workspace.classList.contains('has-selection') ? location.pathname + location.search : workspace.dataset.resultsUrl);
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
      if (workspace.classList.contains('has-selection') && !desktop.matches) return;
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
  const select = async (id, section, push = true, recommend = false, conversation = null, confirmation = null) => {
    const next = new URL(location.href);
    next.searchParams.set('selected_event', id);
    if (section !== 'overview') next.searchParams.set('section', section);
    else next.searchParams.delete('section');
    if (recommend) next.searchParams.set('recommend', 'true');
    else next.searchParams.delete('recommend');
    for (const key of ['selected_thread', 'reply_to', 'discussion_cursor']) next.searchParams.delete(key);
    if (conversation) {
      const parts = conversation.pathname.split('/');
      if (parts[4]) next.searchParams.set('selected_thread', decodeURIComponent(parts[4]));
      if (conversation.searchParams.has('reply_to')) next.searchParams.set('reply_to', conversation.searchParams.get('reply_to'));
      if (conversation.searchParams.has('cursor')) next.searchParams.set('discussion_cursor', conversation.searchParams.get('cursor'));
    }
    const url = conversation || detailURL(id, section, recommend);
    url.searchParams.set('return_to', workspace.dataset.resultsUrl);
    if (sectionHost()?.dataset.eventShell === id) {
      if (push) { rememberPanel(); rememberResults(); }
      await switchSection(url, next, push, recommend, confirmation, !push || Boolean(confirmation));
      markSelection(new URL(location.href).searchParams.get('selected_event'));
      return;
    }
    const panelHadFocus = context.contains(document.activeElement);
    const savedPanel = !push && history.state?.ticketopiaPanel?.url === location.href ? history.state.ticketopiaPanel : null;
    request?.abort();
    request = new AbortController();
    const currentRequest = request;
    const turn = ++generation;
    if (push) {
      if (desktop.matches || !workspace.classList.contains('has-selection')) resultsScroll = scrollY;
      rememberPanel();
      rememberResults();
    }
    // Reveal the initial/different-event loading and recovery surface on compact
    // layouts; its URL is still committed only after a validated response.
    markSelection(id);
    context.setAttribute('aria-busy', 'true');
    context.replaceChildren(previewTemplate('loading'));
    status.textContent = 'Loading event preview.';
    const timeout = setTimeout(() => currentRequest.abort(), 12000);
    try {
      const response = await fetch(url, { signal: currentRequest.signal, headers: { 'X-Ticketopia-Panel': 'true' } });
      if (!response.ok || response.redirected) throw new Error('Event unavailable');
      const html = await response.text();
      if (turn !== generation) return;
      const parsed = new DOMParser().parseFromString(html, 'text/html');
      const fresh = parsed.body.querySelector(':scope > [data-event-shell]');
      if (parsed.body.children.length !== 1 || fresh?.dataset.eventShell !== id || fresh.dataset.contextSection !== section) throw new Error('Unrecognized event');
      if (push) {
        const state = { ...history.state };
        delete state.ticketopiaPanel; delete state.ticketopiaSection;
        history.pushState(state, '', next);
      }
      renderedURL = location.href;
      markSelection(id);
      context.replaceChildren(fresh);
      window.htmx?.process(context);
      context.scrollTop = 0;
      const heading = context.querySelector('.context-heading h2');
      heading.setAttribute('tabindex', '-1');
      if (confirmation) {
        const post = [...context.querySelectorAll('.discussion-post')].find(item => item.id === 'post-' + confirmation.post);
        let target = heading;
        if (confirmation.action === 'helpful' || confirmation.action === 'unhelpful') target = post?.querySelector('button[aria-pressed]') || heading;
        else if (confirmation.action === 'edit') target = post?.querySelector('.discussion-editor > summary') || heading;
        else if (confirmation.action === 'remove' && post) { post.tabIndex = -1; target = post; }
        if (confirmation.action !== 'create') {
          context.scrollTop = confirmation.scroll;
          if (!desktop.matches) scrollTo(0, confirmation.documentScroll);
        }
        target.focus({ preventScroll: true });
      } else if (savedPanel) {
        restorePanel(savedPanel);
      } else if (conversation?.hash === '#discussion-composer') {
        const composer = context.querySelector('#discussion-composer textarea')
          || context.querySelector('#discussion-composer a[href^="/auth/sign-in"]')
          || heading;
        composer.focus();
      } else if (recommend) openRecommendation(context);
      // A chosen event takes focus on every layout so screen readers land on what
      // was selected; history restores keep focus where the browser left it.
      else if (push || panelHadFocus || !desktop.matches) {
        if (!desktop.matches) scrollTo(0, 0);
        heading.focus({ preventScroll: true });
      }
      status.textContent = confirmation?.notice || 'Event preview loaded: ' + context.querySelector('.context-heading h2').textContent;
    } catch {
      if (turn === generation) {
        showError(url, () => select(id, section, push, recommend, conversation, confirmation));
        if (panelHadFocus || !desktop.matches) context.querySelector('[data-preview-retry]')?.focus();
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
      rememberSection();
      rememberPanel();
      const url = new URL(location.href);
      url.searchParams.delete('selected_event');
      url.searchParams.delete('section');
      url.searchParams.delete('recommend');
      for (const key of ['selected_thread', 'reply_to', 'discussion_cursor']) url.searchParams.delete(key);
      history.pushState({ ...history.state }, '', url);
    }
    renderedURL = location.href;
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
    const thread = event.target.closest('#event-context [data-thread-link], .event-row [data-thread-link]');
    if (thread) {
      event.preventDefault();
      const url = new URL(thread.href);
      if (thread.closest('.event-row')) lastFocusID = decodeURIComponent(url.pathname.split('/')[2]);
      select(decodeURIComponent(url.pathname.split('/')[2]), 'discussion', true, false, url);
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
    event.preventDefault();
    select(id, url.searchParams.get('section') || 'overview', true, url.searchParams.get('recommend') === 'true');
  });
  const restoreSelection = () => {
    const params = new URL(location.href).searchParams;
    const id = params.get('selected_event');
    let conversation = null;
    if (id && (params.has('selected_thread') || params.has('discussion_cursor'))) {
      conversation = new URL('/events/' + encodeURIComponent(id) + '/discussions' + (params.has('selected_thread') ? '/' + encodeURIComponent(params.get('selected_thread')) : ''), location.origin);
      if (params.has('reply_to')) conversation.searchParams.set('reply_to', params.get('reply_to'));
      if (params.has('discussion_cursor')) conversation.searchParams.set('cursor', params.get('discussion_cursor'));
    }
    if (id) select(id, params.get('section') || 'overview', false, params.get('recommend') === 'true', conversation);
    else closeContext(false);
  };
  addEventListener('popstate', restoreSelection);
  addEventListener('pagehide', () => { rememberPanel(); rememberResults(); });
  document.addEventListener('ticketopia:discussion-saved', event => {
    if (!context.contains(event.target)) return;
    event.preventDefault();
    const url = new URL(event.detail.location, location.origin);
    const confirmation = { ...event.detail, scroll: context.scrollTop, documentScroll: scrollY };
    if (url.pathname.includes('/discussions')) select(decodeURIComponent(url.pathname.split('/')[2]), 'discussion', event.detail.action === 'create', false, url, confirmation);
    else restoreSelection();
  });
  window.addEventListener('pageshow', event => {
    if (event.persisted && workspace.classList.contains('has-selection')) restoreSelection();
    else if (workspace.classList.contains('has-selection') && history.state?.ticketopiaPanel?.url === location.href) {
      requestAnimationFrame(() => restorePanel(history.state.ticketopiaPanel));
    }
  });
  desktop.addEventListener('change', () => {
    if (!workspace.classList.contains('has-selection')) return;
    if (!desktop.matches) {
      resultsScroll = scrollY;
      scrollTo(0, 0);
      if (!context.contains(document.activeElement)) context.focus({ preventScroll: true });
    } else {
      scrollTo(0, resultsScroll);
    }
  });
})();
