(() => {
  'use strict';
  const forms = () => [...document.querySelectorAll('[data-recommendation-form]')];
  const writing = new Set();
  const drafts = new Map();
  const initialized = new WeakSet();
  const announce = message => { const node = document.querySelector('[data-recommendation-status]'); if (node) node.textContent = message; };
  // Mirrors the Save helpers in app.js.
  const showPending = button => {
    const timer = setTimeout(() => { button.dataset.pending = ''; }, 300);
    return () => { clearTimeout(timer); delete button.dataset.pending; };
  };
  const disconnected = error => error.name === 'TypeError' || !navigator.onLine;
  const connectionMessage = 'Couldn\'t connect. Check your connection and try again; repeating it is safe.';
  const key = form => {
    const viewer = form.closest('[data-event-recommendations]')?.dataset.recommendationViewer;
    return viewer ? `ticketopia-recommendation-draft:${viewer}:${form.dataset.recommendationId}` : '';
  };
  // Page memory updates on every keystroke; browser storage is written at most
  // every 400ms while typing and flushed when the page is hidden or left.
  const unsaved = new Map();
  let persistTimer = 0;
  const persist = () => {
    clearTimeout(persistTimer); persistTimer = 0;
    for (const [name, reason] of unsaved) {
      try { sessionStorage.setItem(name, reason); } catch { /* In-page drafts still work without browser storage. */ }
    }
    unsaved.clear();
  };
  const remember = (form, later = false) => {
    const name = key(form); if (!name) return;
    const reason = form.elements.reason.value;
    drafts.set(name, reason); unsaved.set(name, reason);
    if (!later) persist();
    else if (!persistTimer) persistTimer = setTimeout(persist, 400);
  };
  const initialize = () => {
    for (const form of forms()) {
      if (initialized.has(form)) continue;
      initialized.add(form);
      const name = key(form); if (!name) continue;
      let reason = drafts.get(name);
      try { if (reason === undefined) reason = sessionStorage.getItem(name); } catch { /* Storage is optional. */ }
      if (typeof reason === 'string' && [...reason].length <= 500) {
        form.elements.reason.value = reason;
        form.closest('details').open = true;
      }
    }
  };
  document.addEventListener('input', event => { const form = event.target.closest('[data-recommendation-form]'); if (form) remember(form, true); });
  document.addEventListener('visibilitychange', () => { if (document.hidden) persist(); });
  window.addEventListener('pagehide', persist);
  document.addEventListener('ticketopia:remember-drafts', () => forms().forEach(form => remember(form)));
  document.addEventListener('ticketopia:section-loaded', initialize);
  document.addEventListener('toggle', event => {
    if (!event.target.matches('[data-recommendation-editor]') || !event.target.open) return;
    document.querySelectorAll('[data-recommendation-editor][open]').forEach(editor => { if (editor !== event.target) editor.open = false; });
  }, true);
  document.addEventListener('submit', async event => {
    const form = event.target.closest('[data-recommendation-form]'); if (!form) return;
    event.preventDefault();
    const id = form.dataset.recommendationId;
    if (writing.has(id)) return;
    const panel = form.closest('[data-event-recommendations]');
    const trigger = event.submitter || form.querySelector('button');
    const action = trigger.value || 'set';
    const body = new URLSearchParams(new FormData(form)); body.set('action', action);
    const hadFocus = form.contains(document.activeElement);
    const controls = [...form.querySelectorAll('button, textarea')];
    const errorNode = form.querySelector('[data-recommendation-error]');
    remember(form); writing.add(id);
    errorNode.hidden = true; errorNode.textContent = '';
    form.setAttribute('aria-busy', 'true'); controls.forEach(control => { control.disabled = true; });
    announce(action === 'remove' ? 'Withdrawing recommendation…' : 'Saving public recommendation…');
    const settle = showPending(trigger);
    const controller = new AbortController(); const timeout = setTimeout(() => controller.abort(), 12000);
    let signIn = false;
    try {
      const response = await fetch(form.getAttribute('action'), { method: 'POST', body, headers: { Accept: 'application/json' }, signal: controller.signal });
      const data = await response.json();
      signIn = response.status === 401;
      if (!response.ok) throw new Error(data.detail || "Your recommendation wasn't saved. Try again.");
      if (data.event_id !== id || typeof data.html !== 'string') throw new Error('The recommendation could not be confirmed. Refresh to check it.');
	  document.dispatchEvent(new CustomEvent('ticketopia:activity-mutation', { detail: { id } }));
      const name = key(form);
      if (drafts.get(name) === body.get('reason')) {
        drafts.delete(name); unsaved.delete(name); try { sessionStorage.removeItem(name); } catch { /* Storage is optional. */ }
      }
      if (!panel.isConnected) return; // A late write cannot replace another event's context.
      const parsed = new DOMParser().parseFromString(data.html, 'text/html');
      const fresh = parsed.querySelector('[data-event-recommendations]');
      if (!fresh || fresh.dataset.eventRecommendations !== id) throw new Error('Refresh this page to see the confirmed recommendation.');
      panel.replaceWith(fresh);
      if (hadFocus) fresh.querySelector('summary')?.focus({ preventScroll: true });
      announce(data.notice);
    } catch (error) {
      if (form.isConnected) {
        errorNode.textContent = disconnected(error) ? connectionMessage : ['AbortError', 'SyntaxError'].includes(error.name) ? 'The change could not be confirmed. Try again; repeating the action is safe.' : error.message;
        if (signIn && form.dataset.signInUrl) {
          const link = document.createElement('a');
          link.href = form.dataset.signInUrl;
          link.className = 'text-link'; link.textContent = 'Sign in to continue';
          errorNode.append(document.createTextNode(' Copy your reason before signing in, then return to review and publish. '), link);
        }
        errorNode.hidden = false;
        announce('');
      }
    } finally {
      clearTimeout(timeout); settle(); writing.delete(id);
      document.dispatchEvent(new CustomEvent('ticketopia:activity-write-end'));
      form.removeAttribute('aria-busy'); controls.forEach(control => { control.disabled = false; });
      if (hadFocus && trigger.isConnected && document.activeElement === document.body) trigger.focus({ preventScroll: true });
    }
  });
  // Server-rendered previews can arrive after page initialization. Restore only
  // this signed-in account's draft; never automatically publish it. Only added
  // subtrees that contain a form trigger the document scan.
  const addsForm = record => [...record.addedNodes].some(node => node.nodeType === Node.ELEMENT_NODE
    && (node.matches('[data-recommendation-form]') || node.querySelector('[data-recommendation-form]')));
  new MutationObserver(records => { if (records.some(addsForm)) initialize(); }).observe(document.body, { childList: true, subtree: true });
  initialize();
  window.addEventListener('pageshow', event => {
    if (!event.persisted) return;
    forms().forEach(form => remember(form));
    if (document.querySelector('[data-event-recommendations], .community-page, .account-public, .recommendation-list')) location.reload();
  });
})();
