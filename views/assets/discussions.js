(() => {
  const prefix = 'ticketopia:discussion:';
  const initialized = new WeakSet();
  const memory = new Map();
  function load(key) {
    try { return JSON.parse(sessionStorage.getItem(key)) || memory.get(key); }
    catch (_) { return memory.get(key); }
  }
  function save(form) {
    if (!form.dataset.discussionDraft) return;
    const key = prefix + form.dataset.discussionDraft;
    const value = {body: form.elements.body.value, key: form.elements.idempotency_key.value, parent: form.elements.parent_id.value};
    memory.set(key, value);
    try { sessionStorage.setItem(key, JSON.stringify(value)); } catch (_) { /* In-page drafting still works. */ }
  }
  function restore() {
    document.querySelectorAll('[data-discussion-draft]').forEach(form => {
      if (initialized.has(form)) return;
      initialized.add(form);
      const draft = load(prefix + form.dataset.discussionDraft);
      if (draft && typeof draft.body === 'string' && draft.body.length <= 16000) {
        form.elements.body.value = draft.body;
        if (/^[A-Za-z0-9_-]{16,128}$/.test(draft.key)) form.elements.idempotency_key.value = draft.key;
      }
    });
  }
  document.addEventListener('input', event => {
    const form = event.target.closest('[data-discussion-draft]');
    if (form) save(form);
  });
  document.addEventListener('toggle', event => {
    if (!event.target.matches('.discussion-editor')) return;
    if (event.target.open) {
      document.querySelectorAll('.discussion-editor[open]').forEach(other => {
        if (other !== event.target) { other.querySelectorAll('[data-discussion-draft]').forEach(save); other.open = false; }
      });
    }
    const reply = document.querySelector('.discussion-reply-composer');
    if (reply) reply.hidden = Boolean(document.querySelector('.discussion-editor[open]'));
  }, true);
  document.addEventListener('submit', async event => {
    const form = event.target.closest('[data-discussion-form]');
    if (!form || !window.fetch || !event.submitter) return;
    event.preventDefault();
    if (form.dataset.pending === 'true') return;
    save(form);
    const submitter = event.submitter;
    const data = new URLSearchParams(new FormData(form));
    data.set('action', submitter.value);
    const error = form.querySelector('[data-discussion-error]');
    if (error) error.hidden = true;
    form.dataset.pending = 'true'; form.setAttribute('aria-busy', 'true'); submitter.disabled = true;
	const textarea = form.querySelector('textarea');
	if (textarea) textarea.disabled = true;
    const status = document.querySelector('[data-discussion-status]');
    if (status) status.textContent = 'Saving contribution…';
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 12000);
    let responseStatus = 0;
    let serverError = false;
    try {
      const response = await fetch(form.getAttribute('action'), {method: 'POST', credentials: 'same-origin', headers: {'Accept': 'application/json', 'Content-Type': 'application/x-www-form-urlencoded'}, body: data, signal: controller.signal});
      responseStatus = response.status;
      const result = await response.json();
      if (!response.ok) {
        serverError = typeof result.detail === 'string';
        throw new Error(result.detail || 'The change could not be confirmed.');
      }
      const target = new URL(result.location, location.origin);
      if (target.origin !== location.origin || !target.pathname.startsWith('/')) throw new Error('Refresh this conversation to see the saved change.');
      if (form.dataset.discussionDraft) {
        const key = prefix + form.dataset.discussionDraft;
        memory.delete(key); try { sessionStorage.removeItem(key); } catch (_) {}
        initialized.delete(form);
        form.elements.body.value = '';
      }
      if (status) status.textContent = result.notice;
      // Native navigation avoids replacing a different selected event with a late response.
      if (form.isConnected) {
        const confirmed = new CustomEvent('ticketopia:discussion-saved', {bubbles: true, cancelable: true, detail: {location: target.href, notice: result.notice, action: submitter.value, post: form.elements.post_id.value}});
        if (form.dispatchEvent(confirmed)) location.assign(target.href);
      }
    } catch (failure) {
      if (form.isConnected && error) {
        const validationError = serverError && responseStatus >= 400 && responseStatus < 500;
        error.textContent = validationError ? failure.message : (form.dataset.discussionDraft
          ? 'The change couldn’t be confirmed. Your draft is kept; try again.'
          : 'The change couldn’t be confirmed. Refresh to see your latest choice, then try again.');
        error.hidden = false;
        if (responseStatus === 401) {
          const link = document.createElement('a');
          link.href = '/auth/sign-in?return_to=' + encodeURIComponent(form.elements.return_to.value);
          link.className = 'text-link'; link.textContent = ' Sign in to continue';
          error.append(link);
        }
        if (responseStatus === 409 && form.elements.idempotency_key) {
          const button = document.createElement('button');
          button.type = 'button'; button.className = 'text-link'; button.textContent = 'Use a new key for this contribution';
          button.addEventListener('click', () => {
            form.elements.idempotency_key.value = crypto.randomUUID().replaceAll('-', '');
            save(form); error.textContent = 'Review your draft, then publish it as a new contribution.';
          }, {once: true});
          error.append(document.createTextNode(' '), button);
        }
      }
      if (status) status.textContent = 'The change could not be confirmed. Your draft is kept.';
    } finally {
      clearTimeout(timeout);
      form.dataset.pending = 'false'; form.removeAttribute('aria-busy'); submitter.disabled = false;
	  if (textarea) textarea.disabled = false;
    }
  });
  document.addEventListener('DOMContentLoaded', () => {
    restore();
    if (location.hash === '#discussion-composer') document.querySelector('#discussion-composer textarea')?.focus();
  });
  document.addEventListener('htmx:afterSwap', restore);
  new MutationObserver(restore).observe(document.documentElement, {childList: true, subtree: true});
  window.addEventListener('pageshow', event => { if (event.persisted && document.querySelector('.discussion-page')) location.reload(); else restore(); });
})();
