// Ticketopia's only offline responsibility: when a page navigation can't reach
// the network, show the offline page instead of the browser's error. Nothing
// else is cached; pages (many private and no-store), the API, live activity and
// assets always come from the network. The server replaces the version with a
// hash of the offline page, so changing that page installs a new worker.
'use strict';

const VERSION = '__OFFLINE_VERSION__';
const PREFIX = 'ticketopia-offline-';
const CACHE = PREFIX + VERSION;
const OFFLINE_URL = '/offline';

self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE);
    await cache.add(new Request(OFFLINE_URL, { cache: 'reload' }));
    // Where the static routing API is supported, other requests skip starting
    // the worker entirely.
    await event.addRoutes?.({ condition: { not: { requestMode: 'navigate' } }, source: 'network' }).catch(() => {});
    await self.skipWaiting();
  })());
});

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    for (const name of await caches.keys()) {
      if (name.startsWith(PREFIX) && name !== CACHE) await caches.delete(name);
    }
    // The navigation request starts while the worker boots, so it adds no
    // network latency.
    await self.registration.navigationPreload?.enable();
    await self.clients.claim();
  })());
});

self.addEventListener('fetch', event => {
  if (event.request.mode !== 'navigate') return;
  event.respondWith((async () => {
    try {
      return (await event.preloadResponse) || await fetch(event.request);
    } catch {
      return (await caches.match(OFFLINE_URL, { cacheName: CACHE })) || Response.error();
    }
  })());
});
