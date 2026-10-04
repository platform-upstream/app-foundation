const CACHE_PREFIX = 'foundation-shell:' + encodeURIComponent(self.registration.scope) + ':';
const CACHE = CACHE_PREFIX + 'v1';

self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys
        .filter((key) => key.startsWith(CACHE_PREFIX) && key !== CACHE)
        .map((key) => caches.delete(key))))
      .then(() => self.clients.claim()),
  );
});

function excluded(url) {
  return url.pathname.includes('/api/') ||
    url.pathname.includes('/internal/') ||
    url.pathname.includes('/cdn-cgi/');
}

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== location.origin || event.request.method !== 'GET' || excluded(url)) return;

  event.respondWith((async () => {
    const cache = await caches.open(CACHE);
    try {
      const response = await fetch(event.request);
      if (response.ok && !response.redirected) {
        await cache.put(event.request, response.clone());
        const keys = await cache.keys();
        await Promise.all(keys.slice(0, Math.max(0, keys.length - 64)).map((key) => cache.delete(key)));
      }
      return response;
    } catch {
      const cached = await cache.match(event.request);
      if (cached) return cached;
      if (event.request.mode === 'navigate') {
        const shell = await cache.match(self.registration.scope);
        if (shell) return shell;
      }
      return new Response('This app is offline. Please try again later.', {
        status: 503,
        headers: { 'Content-Type': 'text/plain; charset=utf-8' },
      });
    }
  })());
});
