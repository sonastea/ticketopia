(() => {
  const prefix = 'ticketopia:discussion:';
  const initialized = new WeakSet();
  const memory = new Map();
  const attempts = new WeakMap();
  const confirmedForms = new WeakSet();
  const labels = new WeakMap();
  const motionOK = () => matchMedia('(prefers-reduced-motion: no-preference)').matches;
  // Mirrors the easing helper in app.js.
  const easing = name => getComputedStyle(document.documentElement).getPropertyValue(`--ease-${name}`).trim() || 'ease-out';
  // Briefly highlights a post the reader was just taken to (CSS animates it).
  const arrive = post => {
    if (!post || !motionOK()) return;
    post.removeAttribute('data-arrived');
    void post.offsetWidth;
    post.setAttribute('data-arrived', '');
    post.addEventListener('animationend', () => post.removeAttribute('data-arrived'), {once: true});
  };
  const uncertain = 'This post may already be published. Your draft and reply target are kept. Retry this unchanged draft to confirm it without posting twice.';
  function load(key) {
    if (memory.has(key)) return memory.get(key);
    try { return JSON.parse(sessionStorage.getItem(key)); } catch (_) { return null; }
  }
  // Page memory updates on every keystroke; browser storage is written at most
  // every 400ms while typing and flushed when the page is hidden or left.
  const unsaved = new Map();
  let persistTimer = 0;
  function persist() {
    clearTimeout(persistTimer); persistTimer = 0;
    for (const [key, value] of unsaved) {
      try { sessionStorage.setItem(key, JSON.stringify(value)); } catch (_) { /* In-page drafting still works. */ }
    }
    unsaved.clear();
  }
  function store(form, value, later) {
    const key = prefix + form.dataset.discussionDraft;
    memory.set(key, value); unsaved.set(key, value);
    if (!later) persist();
    else if (!persistTimer) persistTimer = setTimeout(persist, 400);
  }
  function clear(form) {
    const key = prefix + form.dataset.discussionDraft;
    memory.set(key, null); unsaved.delete(key);
    try { sessionStorage.removeItem(key); } catch (_) {}
  }
  function feedback(form, message) {
    const status = form.querySelector('[data-composer-status]');
    if (status) { status.hidden = !message; status.textContent = message; }
  }
  function sync(form) {
    const attempt = attempts.get(form);
    // Read-only retains focus and selection/copy while pending; keyboard behavior
    // still depends on the phone/browser and needs physical-device verification.
    form.elements.body.readOnly = Boolean(attempt) || form.dataset.pending === 'true';
    const discard = form.querySelector('[data-discussion-discard]');
    const submit = form.querySelector('button[type="submit"]');
    if (submit) {
      if (!labels.has(submit)) labels.set(submit, submit.textContent);
      submit.textContent = attempt?.uncertain ? 'Retry unchanged contribution' : labels.get(submit);
    }
    const dirty = form.elements.post_id.value ? form.elements.body.value !== form.elements.body.defaultValue : Boolean(form.elements.body.value);
    if (discard) discard.hidden = Boolean(attempt) || form.dataset.pending === 'true' || !dirty;
  }
  function saveDraft(form, later) {
    if (!form.isConnected || !form.dataset.discussionDraft || confirmedForms.has(form)) return;
    const attempt = attempts.get(form);
    if (form.elements.post_id.value && form.elements.body.value === form.elements.body.defaultValue) {
      clear(form); sync(form); return;
    }
    store(form, {body: form.elements.body.value, key: form.elements.idempotency_key.value, parent: form.elements.parent_id.value, attempt}, later);
    sync(form);
  }
  function save(form) { saveDraft(form, false); }
  function restore() {
    document.querySelectorAll('[data-discussion-draft]').forEach(form => {
      if (initialized.has(form)) return;
      initialized.add(form);
      const draft = load(prefix + form.dataset.discussionDraft);
      if (draft && typeof draft.body === 'string' && draft.body.length <= 16000) {
        form.elements.body.value = draft.body;
        if (/^[A-Za-z0-9_-]{16,128}$/.test(draft.key)) form.elements.idempotency_key.value = draft.key;
        if (draft.body && draft.body !== form.elements.body.defaultValue) feedback(form, 'Draft restored for this contribution. Nothing is published until you submit.');
        // Never restore a retry into a different target or edit action.
        if (draft.attempt && draft.attempt.action === 'create' && !form.elements.post_id.value && draft.parent === form.elements.parent_id.value && draft.attempt.parent_id === draft.parent && draft.attempt.body === draft.body && draft.attempt.idempotency_key === draft.key) {
          attempts.set(form, {...draft.attempt, uncertain: true});
          feedback(form, uncertain);
        }
      }
      sync(form);
    });
  }
  document.addEventListener('input', event => {
    const form = event.target.closest('[data-discussion-draft]');
    if (form) { confirmedForms.delete(form); saveDraft(form, true); keepComposerVisible(); }
  });
  document.addEventListener('visibilitychange', () => { if (document.hidden) persist(); });
  document.addEventListener('click', event => {
    const button = event.target.closest('[data-discard-draft]');
    if (!button) return;
    const form = button.closest('[data-discussion-draft]');
    if (attempts.has(form) || form.dataset.pending === 'true') return;
    clear(form);
    form.elements.body.value = form.elements.post_id.value ? form.elements.body.defaultValue : '';
    if (!form.elements.post_id.value) form.elements.idempotency_key.value = crypto.randomUUID().replaceAll('-', '');
    form.querySelector('[data-discussion-discard]').open = false;
    form.querySelector('[data-discussion-error]').hidden = true;
    feedback(form, 'Draft discarded. Published contributions are unchanged.');
    sync(form);
    form.elements.body.focus();
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

  // Normal document scrolling remains the fallback. Only the focused writing
  // group is nudged when the visual viewport (including a keyboard) changes.
  let viewportFrame, viewportBlockSize;
  function keepComposerVisible() {
    if (viewportFrame) return;
    viewportFrame = requestAnimationFrame(() => {
      viewportFrame = 0;
      const viewport = window.visualViewport;
      // Typing never resizes the viewport. Writing the variable only when it
      // changes keeps keystrokes from invalidating style before the reads below.
      const blockSize = (viewport?.height || innerHeight) + 'px';
      if (blockSize !== viewportBlockSize) {
        viewportBlockSize = blockSize;
        document.documentElement.style.setProperty('--discussion-viewport-block-size', blockSize);
      }
      const textarea = document.activeElement;
      const form = textarea?.closest('.discussion-composer');
      if (!form || textarea.tagName !== 'TEXTAREA') return;
      const toolbar = form.querySelector('.discussion-composer-actions');
      const rect = textarea.getBoundingClientRect();
      const end = toolbar.getBoundingClientRect().bottom;
      // The compact header can stick over the top of the page while scrolling.
      const header = document.querySelector('.compact-header');
      const headerEdge = header && getComputedStyle(header).position === 'sticky' ? Math.max(0, header.getBoundingClientRect().bottom) : 0;
      const viewportTop = viewport?.offsetTop || 0;
      const top = Math.max(viewportTop, headerEdge) + 8;
      let bottom = viewportTop + (viewport?.height || innerHeight) - 8;
      const nav = document.querySelector('.bottom-nav')?.getBoundingClientRect();
      if (nav?.height && nav.top > top && nav.top < bottom) bottom = nav.top - 8;
      // At enlarged text/very short heights, prioritize the field; controls are
      // still reachable by ordinary scrolling, never pinned over the reader.
      const last = end - rect.top <= bottom - top ? end : rect.bottom;
      const delta = rect.top < top ? rect.top - top : last > bottom ? last - bottom : 0;
      if (!delta) return;
      const context = form.closest('#event-context');
      if (context && context.scrollHeight > context.clientHeight && getComputedStyle(context).overflowY === 'auto') context.scrollTop += delta;
      else window.scrollBy(0, delta);
    });
  }
  window.visualViewport?.addEventListener('resize', keepComposerVisible);
  window.addEventListener('resize', keepComposerVisible);
  document.addEventListener('focusin', keepComposerVisible);

  // Standalone conversations refresh in place just like contextual discussions.
  const page = document.querySelector('.discussion-page[data-discussion-viewer]');
  let readTurn = 0;
  let readController;
  function rememberConversation() {
    if (!page) return;
    const active = document.activeElement;
    history.replaceState({...history.state, ticketopiaConversation: {
      scroll: scrollY, draft: active?.closest('[data-discussion-draft]')?.dataset.discussionDraft,
      post: active?.closest('.discussion-post')?.id,
    }}, '', location.href);
  }
  async function readConversation(url, {push = false, confirmation, saved} = {}) {
    const turn = ++readTurn;
    readController?.abort();
    readController = new AbortController();
    const controller = readController;
    const timeout = setTimeout(() => controller.abort(), 12000);
    const status = page.querySelector('[data-conversation-status]');
    const content = page.querySelector('[data-conversation-content]');
    const position = scrollY;
    if (push) rememberConversation();
    document.querySelectorAll('[data-discussion-draft]').forEach(save);
    status.hidden = false;
    status.textContent = confirmation?.notice || 'Loading conversation…';
    content.setAttribute('aria-busy', 'true');
    try {
      const response = await fetch(url, {credentials: 'same-origin', signal: controller.signal});
      if (!response.ok || response.redirected) throw new Error('Conversation unavailable');
      const parsed = new DOMParser().parseFromString(await response.text(), 'text/html');
      if (turn !== readTurn) return;
      const fresh = parsed.querySelector('.discussion-page[data-discussion-viewer]');
      if (!fresh || fresh.dataset.discussionViewer !== page.dataset.discussionViewer || !fresh.querySelector('[data-conversation-content]') || fresh.querySelector('[data-conversation-content] script')) throw new Error('Reload to check your account');
      // Keep input entered while a read was in flight, not just its start snapshot.
      document.querySelectorAll('[data-discussion-draft]').forEach(save);
      const open = content.querySelector('.discussion-editor[open] [data-discussion-draft]')?.dataset.discussionDraft;
      content.replaceChildren(...fresh.querySelector('[data-conversation-content]').childNodes);
      // A reader-chosen conversation fades in; refreshes after a write don't.
      if (push && !confirmation && motionOK()) content.animate({opacity: [0, 1]}, {duration: 160, easing: easing('out')});
      if (push) {
        const state = {...history.state, ticketopiaConversation: null};
        const journey = state.ticketopiaReturn;
        // Match the shared shell's return hierarchy: same-thread history entries
        // still count toward the original collection, not just native documents.
        if (journey && Number.isInteger(journey.steps) && journey.steps > 0 && journey.steps < history.length) {
          state.ticketopiaReturn = {...journey, steps: journey.steps + 1};
        }
        history.pushState(state, '', url);
      }
      else if (confirmation) history.replaceState(history.state, '', url);
      restore();
      if (open && confirmation?.action !== 'edit') {
        const editor = [...content.querySelectorAll('[data-discussion-draft]')].find(form => form.dataset.discussionDraft === open)?.closest('details');
        if (editor) editor.open = true;
      }
      status.textContent = confirmation?.notice || 'Conversation loaded.';
      status.classList.add('sr-only');
      let target;
      if (saved) {
        target = [...content.querySelectorAll('[data-discussion-draft]')].find(form => form.dataset.discussionDraft === saved.draft)?.querySelector('textarea');
        target ||= saved.post ? document.getElementById(saved.post)?.querySelector('button[aria-pressed]') : null;
        if (target?.closest('details')) target.closest('details').open = true;
        scrollTo(0, saved.scroll);
      } else if (confirmation) {
        const post = document.getElementById('post-' + confirmation.post);
        target = confirmation.action === 'helpful' || confirmation.action === 'unhelpful' ? post?.querySelector('button[aria-pressed]')
          : confirmation.action === 'edit' ? post?.querySelector('.discussion-editor > summary') : post;
        if (confirmation.action !== 'create') scrollTo(0, position);
        if (confirmation.action === 'create' || confirmation.action === 'edit') arrive(post);
      } else if (url.hash === '#discussion-composer') target = content.querySelector('#discussion-composer textarea, #discussion-composer a');
      else if (url.hash.startsWith('#post-')) arrive(target = document.getElementById(url.hash.slice(1)));
      else { target = content.querySelector('h2'); scrollTo(0, 0); }
      if (target) {
        if (target.matches('article, h2')) target.tabIndex = -1;
        target.focus({preventScroll: Boolean(saved || (confirmation && confirmation.action !== 'create'))});
      }
      rememberConversation();
    } catch (_) {
      if (turn !== readTurn) return;
      status.classList.remove('sr-only');
      status.textContent = (confirmation?.notice ? confirmation.notice + ' ' : '') + 'The conversation could not refresh. Your other drafts are kept. ';
      const retry = document.createElement('button');
      retry.type = 'button'; retry.className = 'button-quiet'; retry.textContent = 'Retry conversation';
      retry.addEventListener('click', () => readConversation(url, {push, confirmation, saved}));
      const native = document.createElement('a');
      native.href = url.href; native.className = 'text-link'; native.textContent = 'Open conversation';
      status.append(retry, document.createTextNode(' '), native);
    } finally {
      clearTimeout(timeout);
      if (turn === readTurn) content.removeAttribute('aria-busy');
    }
  }
  if (page) {
    document.addEventListener('click', event => {
      if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const link = event.target.closest('[data-thread-link]');
      if (!link || !page.contains(link)) return;
      const url = new URL(link.href);
      if (url.origin !== location.origin || url.pathname !== location.pathname) return;
      event.preventDefault();
      readConversation(url, {push: true});
    });
    window.addEventListener('popstate', () => readConversation(new URL(location.href), {saved: history.state?.ticketopiaConversation}));
    window.addEventListener('pagehide', rememberConversation);
    window.addEventListener('pagehide', () => { ++readTurn; readController?.abort(); });
    document.addEventListener('ticketopia:discussion-saved', event => {
      if (!page.contains(event.target)) return;
      const url = new URL(event.detail.location);
      if (url.pathname !== location.pathname) return;
      event.preventDefault();
      readConversation(url, {confirmation: event.detail});
    });
  }
  document.addEventListener('submit', async event => {
    const form = event.target.closest('[data-discussion-form]');
    if (!form || !window.fetch || !event.submitter) return;
    event.preventDefault();
    if (form.dataset.pending === 'true') return;
    const submitter = event.submitter;
    const submittedTurn = readTurn;
    const data = new URLSearchParams(new FormData(form));
    data.set('action', submitter.value);
    const draft = Boolean(form.dataset.discussionDraft);
    if (draft && submitter.value === 'create') {
      const attempt = attempts.get(form) || {action: 'create', body: data.get('body'), idempotency_key: data.get('idempotency_key'), parent_id: data.get('parent_id')};
      attempts.set(form, attempt);
      for (const name of ['action', 'body', 'idempotency_key', 'parent_id']) data.set(name, attempt[name]);
    }
    if (draft) save(form);
    const error = form.querySelector('[data-discussion-error]');
    if (error) error.hidden = true;
    form.dataset.pending = 'true'; form.setAttribute('aria-busy', 'true'); submitter.disabled = true;
    if (draft) { sync(form); feedback(form, submitter.value === 'create' ? 'Publishing contribution…' : 'Saving changes…'); }
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
      if (typeof result.location !== 'string' || target.origin !== location.origin || !target.pathname.startsWith('/events/')) throw new Error('Refresh this conversation to see the saved change.');
      if (draft) {
        const latest = load(prefix + form.dataset.discussionDraft);
        if (latest?.body === data.get('body') && latest?.key === data.get('idempotency_key')) clear(form);
        attempts.delete(form);
        confirmedForms.add(form);
        if (submitter.value === 'create') {
          form.elements.body.value = '';
          form.elements.idempotency_key.value = crypto.randomUUID().replaceAll('-', '');
        }
        feedback(form, result.notice);
      }
      if (status) status.textContent = result.notice;
      if (form.isConnected && (!page || submittedTurn === readTurn)) {
        const post = submitter.value === 'create' ? target.searchParams.get('post_id') || target.hash.replace('#post-', '') : form.elements.post_id.value;
        const confirmed = new CustomEvent('ticketopia:discussion-saved', {bubbles: true, cancelable: true, detail: {location: target.href, notice: result.notice, action: submitter.value, post}});
        if (form.dispatchEvent(confirmed)) location.assign(target.href);
      }
    } catch (failure) {
      // A structured rejection (except key conflict) proves this attempt did not
      // create a post. Transport/5xx/malformed results keep its immutable payload.
      const rejected = serverError && responseStatus >= 400 && responseStatus < 500 && responseStatus !== 409;
      if (rejected && !attempts.get(form)?.uncertain) attempts.delete(form);
      else if (attempts.has(form)) attempts.get(form).uncertain = true;
      if (form.isConnected && error) {
        error.textContent = rejected ? failure.message : attempts.has(form) ? uncertain : (draft
          ? 'The change couldn’t be confirmed. Your draft is kept; try again.'
          : 'The change couldn’t be confirmed. Refresh to see your latest choice, then try again.');
        error.hidden = false;
        if (responseStatus === 401 || responseStatus === 409) {
          const link = document.createElement('a');
          link.href = responseStatus === 401 ? '/auth/sign-in?return_to=' + encodeURIComponent(form.elements.return_to.value) : form.elements.return_to.value;
          link.className = 'text-link'; link.textContent = responseStatus === 401 ? ' Sign in to continue' : ' Check conversation';
          error.append(link);
        }
      }
      // The inline alert owns failure copy; do not repeat it in a second visible
      // status paragraph. Restored uncertain drafts use the status on reload.
      if (draft) feedback(form, '');
      if (status) status.textContent = 'The change could not be confirmed.';
    } finally {
      clearTimeout(timeout);
      form.dataset.pending = 'false'; form.removeAttribute('aria-busy'); submitter.disabled = false;
      if (draft) save(form);
    }
  });
  document.addEventListener('DOMContentLoaded', () => {
    restore(); keepComposerVisible();
    if (location.hash === '#discussion-composer') document.querySelector('#discussion-composer textarea')?.focus();
  });
  document.addEventListener('htmx:afterSwap', restore);
  document.addEventListener('ticketopia:remember-drafts', () => document.querySelectorAll('[data-discussion-draft]').forEach(save));
  document.addEventListener('ticketopia:section-loaded', restore);
  window.addEventListener('pagehide', () => document.querySelectorAll('[data-discussion-draft]').forEach(save));
  // Only added subtrees that contain a draft form trigger the document scan.
  const addsDraft = record => [...record.addedNodes].some(node => node.nodeType === Node.ELEMENT_NODE
    && (node.matches('[data-discussion-draft]') || node.querySelector('[data-discussion-draft]')));
  new MutationObserver(records => { if (records.some(addsDraft)) restore(); }).observe(document.documentElement, {childList: true, subtree: true});
  window.addEventListener('pageshow', event => {
    if (event.persisted && page) readConversation(new URL(location.href), {saved: history.state?.ticketopiaConversation});
    else restore();
  });
})();
