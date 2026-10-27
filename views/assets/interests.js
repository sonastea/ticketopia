(() => {
  'use strict';
  const forms = id => [...document.querySelectorAll('[data-interest-form]')]
    .filter(form => !id || form.dataset.interestId === id);
  const writing = new Set();
  let stateTurn = 0;
  let defaultVisibility = document.querySelector('[data-interest-default]')?.dataset.interestDefault || 'private';
  // Mirrors the easing helper in app.js.
  const easing = name => getComputedStyle(document.documentElement).getPropertyValue(`--ease-${name}`).trim() || 'ease-out';
  const label = visibility => visibility === 'public' ? 'Public' : 'Private';
  const announce = message => {
    const status = document.querySelector('[data-interest-status]');
    if (status) status.textContent = message;
  };
  // Mirrors the Save helpers in app.js.
  const showPending = button => {
    const timer = setTimeout(() => { button.dataset.pending = ''; }, 300);
    return () => { clearTimeout(timer); delete button.dataset.pending; };
  };
  const disconnected = error => error.name === 'TypeError' || !navigator.onLine;
  const connectionMessage = 'Couldn\'t connect. Check your connection and try again; repeating it is safe.';
  const feedbackOrigins = new WeakMap();
  const clearFeedback = () => {
    document.querySelectorAll('[data-interest-feedback]').forEach(target => {
      target.hidden = true;
      target.querySelector('[data-interest-message]').textContent = '';
    });
  };
  const feedback = (form, message, origin, signIn = false) => {
    clearFeedback();
    const target = form?.closest('[data-interest-privacy], [data-event-actions]')?.querySelector('[data-interest-feedback]');
    if (!target) { announce(message); return; }
    announce('');
    feedbackOrigins.set(target, origin || form.querySelector('button'));
    target.hidden = false;
    target.querySelector('[data-interest-message]').textContent = message;
    if (signIn && form.dataset.signInUrl) {
      const link = document.createElement('a');
      link.href = form.dataset.signInUrl;
      link.className = 'text-link'; link.textContent = 'Sign in to continue';
      target.querySelector('[data-interest-message]').append(document.createTextNode(' '), link);
    }
  };
  const update = (id, state, csrf) => {
    const visibility = state.visibility || defaultVisibility;
    for (const form of forms(id)) {
      if (csrf) form.elements.csrf_token.value = csrf;
      form.elements.visibility.value = visibility;
      if (!form.hasAttribute('data-interest-edit')) {
        form.elements.action.value = state.interested ? 'remove' : 'set';
        const button = form.querySelector('button');
        if (state.interested && button.getAttribute('aria-pressed') !== 'true' && matchMedia('(prefers-reduced-motion: no-preference)').matches) {
          button.querySelector('.icon')?.animate?.({ scale: [.6, 1.2, 1] }, { duration: 320, easing: easing('out') });
        }
        button.setAttribute('aria-pressed', String(state.interested));
        const text = form.dataset.interestCollection === 'true' && state.interested ? `Remove interest · ${label(visibility)}` : `Interested · ${label(visibility)}`;
        button.setAttribute('aria-label', `${text} — ${state.interested ? 'Remove' : 'Mark'} interest for ${form.dataset.eventName}`);
        button.querySelector('span').textContent = text;
      }
    }
    for (const node of document.querySelectorAll('[data-interest-count]')) {
      if (node.dataset.interestCount !== id) continue;
      const number = document.createElement('strong'); number.textContent = state.count;
      node.replaceChildren(number, document.createTextNode(state.count === 1 ? ' person interested' : ' people interested'));
      node.hidden = state.count === 0;
    }
    for (const details of document.querySelectorAll('[data-interest-details]')) {
      if (details.dataset.interestDetails !== id) continue;
      const privacy = details.querySelector('[data-interest-privacy]');
      if (privacy) {
        if (state.interested && privacy.hidden && matchMedia('(prefers-reduced-motion: no-preference)').matches) {
          privacy.animate?.({ opacity: [0, 1] }, { duration: 160, easing: easing('out') });
        }
        privacy.hidden = !state.interested; if (!state.interested) privacy.open = false;
      }
    }
  };
  const refresh = async () => {
    clearFeedback();
    announce('');
    const ids = [...new Set(forms().map(form => form.dataset.interestId))];
    if (!ids.length) return;
    const turn = ++stateTurn;
    try {
      for (let offset = 0; offset < ids.length; offset += 100) {
        const params = new URLSearchParams();
        ids.slice(offset, offset + 100).forEach(id => params.append('event_id', id));
        const response = await fetch('/api/v1/me/event-interests/states?' + params);
        if (turn !== stateTurn) return;
        if (response.status === 401) {
          location.reload();
          return;
        }
        if (!response.ok) throw new Error('Interest state is unavailable. Refresh this page to try again.');
        const data = await response.json();
        if (turn !== stateTurn) return;
        defaultVisibility = data.default_visibility;
        ids.slice(offset, offset + 100).forEach(id => update(id, data.states[id], data.csrf_token));
      }
    } catch (error) {
      if (turn !== stateTurn) return;
      forms().forEach(form => { form.querySelector('button').disabled = true; });
      feedback(forms()[0], error.message);
    }
  };
  // Public participant reads are independent of the owner response. A failed
  // refresh hides old identities rather than retaining a revoked public choice.
  const participantTurns = new Map();
  const refreshParticipants = async id => {
    const lists = [...document.querySelectorAll('[data-public-participants]')].filter(node => node.dataset.publicParticipants === id);
    if (!lists.length) return;
    const turn = (participantTurns.get(id) || 0) + 1;
    participantTurns.set(id, turn);
    lists.forEach(node => { node.replaceChildren(); node.textContent = 'Refreshing public participants…'; });
    try {
      const response = await fetch('/api/v1/events/' + encodeURIComponent(id) + '/interested-users', { signal: AbortSignal.timeout(10000) });
      if (!response.ok) throw new Error();
      const data = await response.json();
      if (participantTurns.get(id) !== turn) return;
      for (const node of lists) {
        if (!node.isConnected) continue;
        const list = document.createElement('ul'); list.className = 'public-interest-list';
        for (const item of data.items) {
          const entry = document.createElement('li'); const link = document.createElement('a');
          link.href = '/users/' + item.profile.id; link.className = 'text-link'; link.textContent = item.profile.display_name;
          entry.append(link); list.append(entry);
        }
        node.replaceChildren(list);
        if (!data.items.length) node.append(document.createTextNode('No public participants on this page.'));
        if (data.next_cursor) {
          const more = document.createElement('a'); more.className = 'text-link';
          const query = new URLSearchParams({ cursor: data.next_cursor });
          if (node.dataset.participantsReturn && node.dataset.participantsReturn !== '/') query.set('return_to', node.dataset.participantsReturn);
          more.href = '/events/' + encodeURIComponent(id) + '/interested-users?' + query;
          more.textContent = 'See more public participants'; node.append(more);
        }
      }
    } catch {
      if (participantTurns.get(id) === turn) lists.forEach(node => { node.textContent = "Public participants couldn't load. Refresh to try again."; });
    }
  };
  document.addEventListener('click', event => {
    const dismiss = event.target.closest('[data-interest-dismiss]');
    if (!dismiss) return;
    const target = dismiss.closest('[data-interest-feedback]');
    const origin = feedbackOrigins.get(target);
    // Move focus before hiding the focused dismiss button.
    if (origin?.isConnected && !origin.disabled) origin.focus({ preventScroll: true });
    else {
      const details = target.closest('[data-interest-privacy]');
      const fallback = details?.querySelector('summary') || document.getElementById('main');
      if (fallback) {
        if (fallback.id === 'main') fallback.setAttribute('tabindex', '-1');
        fallback.focus({ preventScroll: true });
      }
    }
    target.hidden = true;
    target.querySelector('[data-interest-message]').textContent = '';
  });
  document.addEventListener('submit', async event => {
    const form = event.target.closest('[data-interest-form]');
    if (!form) return;
    event.preventDefault();
    const id = form.dataset.interestId;
    if (writing.has(id)) return;
    writing.add(id); ++stateTurn;
    const body = new URLSearchParams(new FormData(form));
    const csrf = body.get('csrf_token');
    const trigger = event.submitter || form.querySelector('button');
    const hadFocus = form.contains(document.activeElement);
    let focusTarget = trigger;
    const controls = forms(id);
    controls.forEach(node => { node.setAttribute('aria-busy', 'true'); node.querySelectorAll('button, select').forEach(control => { control.disabled = true; }); });
    clearFeedback();
    announce('Updating event interest…');
    const settle = showPending(trigger);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 12000);
    let signIn = false;
    try {
      const response = await fetch(form.getAttribute('action'), {
        method: 'POST', body, headers: { Accept: 'application/json' }, signal: controller.signal,
      });
      const data = await response.json();
      signIn = response.status === 401;
      if (!response.ok) throw new Error(data.detail || 'Interest could not be updated. Try again.');
      if (!forms(id).some(item => item.elements.csrf_token.value === csrf)) return;
      update(data.event_id, data.state);
      document.dispatchEvent(new CustomEvent('ticketopia:activity-mutation', { detail: { id } }));
      clearFeedback();
      announce(data.state.interested ? `Interest saved as ${data.state.visibility}.` : `${form.dataset.eventName}: your interest was removed.`);
      if (form.hasAttribute('data-interest-edit')) {
        const privacy = form.closest('[data-interest-privacy]');
        privacy.open = false;
        focusTarget = privacy.querySelector('summary');
        if (hadFocus) focusTarget.focus({ preventScroll: true });
      }
      // The confirmed write re-enables controls now; the public list follows.
      refreshParticipants(id);
      if (location.pathname === '/me/interests' && !data.state.interested) {
        announce(''); // The visible collection notice is the single announcement.
        document.dispatchEvent(new CustomEvent('ticketopia:collection-action', { detail: {
          form, notice: `${form.dataset.eventName}: your interest was removed.`,
        } }));
      }
    } catch (error) {
      if (form.isConnected && form.elements.csrf_token.value === csrf) feedback(form, disconnected(error) ? connectionMessage : ['AbortError', 'SyntaxError'].includes(error.name) ? 'Interest could not be confirmed. Try again; repeating the action is safe.' : error.message, trigger, signIn);
    } finally {
      clearTimeout(timeout); settle(); writing.delete(id);
      document.dispatchEvent(new CustomEvent('ticketopia:activity-write-end'));
      controls.forEach(node => { node.removeAttribute('aria-busy'); node.querySelectorAll('button, select').forEach(control => { control.disabled = false; }); });
      if (hadFocus && focusTarget.isConnected && document.activeElement === document.body) focusTarget.focus({ preventScroll: true });
    }
  });
  document.addEventListener('ticketopia:viewer-changed', () => { ++stateTurn; });
  window.addEventListener('pageshow', event => {
    if (!event.persisted) return;
    if (!forms().length && document.querySelector('[data-interest-count], [data-public-participants], .account-public')) location.reload();
    else { refresh(); document.querySelectorAll('[data-public-participants]').forEach(node => refreshParticipants(node.dataset.publicParticipants)); }
  });
})();
