(() => {
  'use strict';
  const forms = () => [...document.querySelectorAll('[data-recommendation-form]')];
  const writing = new Set();
  const drafts = new Map();
  const initialized = new WeakSet();
  const announce = message => { const node = document.querySelector('[data-recommendation-status]'); if (node) node.textContent = message; };
  const key = form => {
    const viewer = form.closest('[data-event-recommendations]')?.dataset.recommendationViewer;
    return viewer ? `ticketopia-recommendation-draft:${viewer}:${form.dataset.recommendationId}` : '';
  };
  const remember = form => {
    const name = key(form); if (!name) return;
    const reason = form.elements.reason.value;
    drafts.set(name, reason);
    try { sessionStorage.setItem(name, reason); } catch { /* In-page drafts still work without browser storage. */ }
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
  document.addEventListener('input', event => { const form = event.target.closest('[data-recommendation-form]'); if (form) remember(form); });
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
    const controller = new AbortController(); const timeout = setTimeout(() => controller.abort(), 12000);
    try {
      const response = await fetch(form.getAttribute('action'), { method: 'POST', body, headers: { Accept: 'application/json' }, signal: controller.signal });
      const data = await response.json();
      if (!response.ok) throw new Error(data.detail || "Your recommendation wasn't saved. Try again.");
      if (data.event_id !== id || typeof data.html !== 'string') throw new Error('The recommendation could not be confirmed. Refresh to check it.');
      const name = key(form); drafts.delete(name); try { sessionStorage.removeItem(name); } catch { /* Storage is optional. */ }
      if (!panel.isConnected) return; // A late write cannot replace another event's context.
      const parsed = new DOMParser().parseFromString(data.html, 'text/html');
      const fresh = parsed.querySelector('[data-event-recommendations]');
      if (!fresh || fresh.dataset.eventRecommendations !== id) throw new Error('Refresh this page to see the confirmed recommendation.');
      panel.replaceWith(fresh);
      if (hadFocus) fresh.querySelector('summary')?.focus({ preventScroll: true });
      announce(data.notice);
    } catch (error) {
      if (form.isConnected) {
        errorNode.textContent = error.name === 'AbortError' ? 'The change could not be confirmed. Try again; repeating the action is safe.' : error.message;
        errorNode.hidden = false;
        announce('');
      }
    } finally {
      clearTimeout(timeout); writing.delete(id);
      form.removeAttribute('aria-busy'); controls.forEach(control => { control.disabled = false; });
      if (hadFocus && trigger.isConnected && document.activeElement === document.body) trigger.focus({ preventScroll: true });
    }
  });
  // Server-rendered previews can arrive after page initialization. Restore only
  // this signed-in account's draft; never automatically publish it.
  new MutationObserver(initialize).observe(document.body, { childList: true, subtree: true });
  initialize();
  window.addEventListener('pageshow', event => {
    if (!event.persisted) return;
    forms().forEach(remember);
    if (document.querySelector('[data-event-recommendations], .community-page, .account-public, .recommendation-list')) location.reload();
  });
})();
