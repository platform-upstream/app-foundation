// Service worker installed by the frontend registry's template. Replace this file with the new
// release's copy when app-foundation changes it; put the app's own worker code (push,
// notificationclick, share target, extra cache rules) in public/sw-app.js, which is loaded below.
//
// It caches exactly two things: the app shell (navigations, network first, the cached shell only
// when the network fails) and the hashed Vite files under /assets/ (immutable, cache first).
// Everything else goes to the network untouched: /api/, /internal/, /cdn-cgi/ and /pwa/ (the
// manifest and icons revalidate anyway) are never handled, and responses marked
// `Cache-Control: no-store` or `private` are never stored. The one exception is the shell document:
// app-foundation's web.Handler serves it `no-store` to keep it out of HTTP caches, so a navigation
// is stored unless it is `private` (mark per-user HTML `private` to keep it out of this cache).

// sw.js?name=<app> names the cache (registerServiceWorker({ name })); without it the scope does.
const APP_NAME = (() => {
  const name = new URL(self.location.href).searchParams.get('name');
  return name && /^[a-z0-9][a-z0-9-]{0,62}$/.test(name) ? name : 'foundation';
})();
const SCOPE = encodeURIComponent(self.registration.scope);
const CACHE = APP_NAME + '-shell:' + SCOPE + ':v2';
const SHELL = self.registration.scope;
const MAX_ENTRIES = 64;

// App-owned extension point: public/sw-app.js, if it exists. Its listeners are registered before
// the ones below; a fetch listener there that calls event.respondWith must also call
// event.stopImmediatePropagation() so this worker leaves the request alone.
try {
  importScripts('sw-app.js');
} catch {
  // No sw-app.js (404) or it failed to load: the template worker runs on its own.
}

self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
  // Drops this scope's older caches, including those of earlier releases and other names.
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys
        .filter((key) => key !== CACHE && key.includes('-shell:' + SCOPE + ':'))
        .map((key) => caches.delete(key))))
      .then(() => self.clients.claim()),
  );
});

function excluded(url) {
  return ['/api/', '/internal/', '/cdn-cgi/', '/pwa/'].some((part) => url.pathname.includes(part));
}

function storable(response, navigation) {
  if (!response.ok || response.redirected) return false;
  const control = (response.headers.get('Cache-Control') || '').toLowerCase();
  if (/(^|[\s,])private\b/.test(control)) return false;
  return navigation || !/(^|[\s,])no-store\b/.test(control);
}

async function trim(cache) {
  const keys = (await cache.keys()).filter((key) => key.url !== SHELL);
  await Promise.all(keys.slice(0, Math.max(0, keys.length - MAX_ENTRIES)).map((key) => cache.delete(key)));
}

function offline() {
  return new Response('This app is offline. Please try again later.', {
    status: 503,
    headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' },
  });
}

async function navigate(request) {
  const cache = await caches.open(CACHE);
  try {
    const response = await fetch(request);
    if (storable(response, true)) await cache.put(SHELL, response.clone());
    return response;
  } catch {
    return (await cache.match(SHELL)) || offline();
  }
}

async function asset(request) {
  const cache = await caches.open(CACHE);
  const cached = await cache.match(request);
  if (cached) return cached;
  try {
    const response = await fetch(request);
    if (storable(response, false)) {
      await cache.put(request, response.clone());
      await trim(cache);
    }
    return response;
  } catch {
    return offline();
  }
}

self.addEventListener('fetch', (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (url.origin !== location.origin || request.method !== 'GET' || excluded(url)) return;
  if (request.mode === 'navigate') event.respondWith(navigate(request));
  else if (url.pathname.includes('/assets/')) event.respondWith(asset(request));
});
