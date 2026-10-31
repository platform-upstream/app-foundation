// Runs the template service worker (argv[2]) in a VM with fake caches and fetch and checks what it
// handles and stores. Started by TestServiceWorker; exits non-zero on the first failure.
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';

const source = readFileSync(process.argv[2], 'utf8');
const ORIGIN = 'https://app.example.org';
const SCOPE = ORIGIN + '/';

function worker({ query = '', importScripts = () => { throw new Error('404'); }, stores = new Map() } = {}) {
  const listeners = {};
  const key = (request) => (typeof request === 'string' ? request : request.url);
  const cache = (entries) => ({
    async put(request, response) { entries.delete(key(request)); entries.set(key(request), response); },
    async match(request) { return entries.get(key(request)); },
    async keys() { return [...entries.keys()].map((url) => ({ url })); },
    async delete(request) { return entries.delete(key(request)); },
  });
  const caches = {
    async open(name) { if (!stores.has(name)) stores.set(name, new Map()); return cache(stores.get(name)); },
    async keys() { return [...stores.keys()]; },
    async delete(name) { return stores.delete(name); },
  };
  const state = { fetches: [], network: null, imported: [] };
  const self = {
    location: { href: SCOPE + 'sw.js' + query },
    registration: { scope: SCOPE },
    addEventListener: (type, fn) => { (listeners[type] ||= []).push(fn); },
    skipWaiting: async () => {},
    clients: { claim: async () => {} },
  };
  const context = vm.createContext({
    self, caches, URL, Headers, Response, Promise, console,
    location: { origin: ORIGIN },
    importScripts: (name) => { state.imported.push(name); return importScripts(name); },
    fetch: async (request) => {
      state.fetches.push(request.url);
      if (!state.network) throw new TypeError('offline');
      return state.network(request);
    },
  });
  vm.runInContext(source, context, { filename: 'sw.js' });
  const dispatch = (type, event) => { for (const fn of listeners[type] || []) fn(event); return event; };
  return {
    state, stores,
    async fetch(path, { mode = 'cors', method = 'GET', origin = ORIGIN } = {}) {
      let result;
      dispatch('fetch', { request: { url: origin + path, method, mode }, respondWith(p) { result = p; } });
      return result === undefined ? undefined : await result;
    },
    async activate() {
      let wait;
      dispatch('activate', { waitUntil(p) { wait = p; } });
      await wait;
    },
  };
}

function response(body, { cacheControl = '', redirected = false, status = 200 } = {}) {
  const headers = new Headers(cacheControl ? { 'Cache-Control': cacheControl } : {});
  const r = { body, status, ok: status >= 200 && status < 300, redirected, headers };
  r.clone = () => r;
  return r;
}

const encodedScope = encodeURIComponent(SCOPE);

// Cache name: ?name= when valid, else the scope alone; sw-app.js is optional.
{
  const named = worker({ query: '?name=probe' });
  named.state.network = () => response('<html>', { cacheControl: 'no-store' });
  await named.fetch('/', { mode: 'navigate' });
  assert.deepEqual([...named.stores.keys()], [`probe-shell:${encodedScope}:v2`]);
  assert.deepEqual(named.state.imported, ['sw-app.js']);

  for (const query of ['', '?name=Bad%20Name', '?name=-x']) {
    const plain = worker({ query });
    plain.state.network = () => response('<html>');
    await plain.fetch('/', { mode: 'navigate' });
    assert.deepEqual([...plain.stores.keys()], [`foundation-shell:${encodedScope}:v2`], query);
  }

  let loaded = false;
  worker({ importScripts: () => { loaded = true; } });
  assert.ok(loaded, 'sw-app.js hook not loaded');
}

// Never handled: other origins, non-GET, /api/, /internal/, /cdn-cgi/, /pwa/, ordinary files.
{
  const w = worker();
  w.state.network = () => response('x');
  for (const [path, options] of [
    ['/api/notes', {}], ['/api/notes', { mode: 'navigate' }], ['/internal/mail', {}],
    ['/cdn-cgi/access/logout', { mode: 'navigate' }], ['/pwa/icon-180.png', {}],
    ['/pwa/manifest.webmanifest', {}], ['/sw-app.js', {}], ['/favicon.ico', {}],
    ['/assets/app.js', { method: 'POST' }], ['/assets/app.js', { origin: 'https://cdn.example.org' }],
  ]) {
    assert.equal(await w.fetch(path, options), undefined, `${path} ${JSON.stringify(options)} was handled`);
  }
  assert.equal(w.state.fetches.length, 0);
  assert.equal(w.stores.size, 0);
}

// Navigations: network first, stored as the one shell entry (even no-store), never private or
// redirected responses; offline falls back to the shell, else a 503.
{
  const w = worker({ query: '?name=probe' });
  const entries = () => [...(w.stores.get(`probe-shell:${encodedScope}:v2`) || new Map()).keys()];

  assert.equal((await w.fetch('/notes', { mode: 'navigate' })).status, 503, 'offline without shell');

  w.state.network = () => response('private', { cacheControl: 'no-store, private' });
  await w.fetch('/notes', { mode: 'navigate' });
  w.state.network = () => response('redirect', { redirected: true });
  await w.fetch('/notes', { mode: 'navigate' });
  w.state.network = () => response('error', { status: 500 });
  await w.fetch('/notes', { mode: 'navigate' });
  assert.deepEqual(entries(), []);

  w.state.network = () => response('shell', { cacheControl: 'no-store' });
  assert.equal((await w.fetch('/notes/7', { mode: 'navigate' })).body, 'shell');
  assert.deepEqual(entries(), [SCOPE]);

  w.state.network = null;
  assert.equal((await w.fetch('/other', { mode: 'navigate' })).body, 'shell');
}

// /assets/: cache first, stored unless no-store or private, bounded to 64 entries besides the shell.
{
  const w = worker();
  const store = () => w.stores.get(`foundation-shell:${encodedScope}:v2`);
  w.state.network = (request) => response('asset ' + request.url);
  assert.equal((await w.fetch('/assets/app-1.js')).body, `asset ${ORIGIN}/assets/app-1.js`);
  w.state.network = null;
  assert.equal((await w.fetch('/assets/app-1.js')).body, `asset ${ORIGIN}/assets/app-1.js`, 'cache first');
  assert.equal((await w.fetch('/assets/missing.js')).status, 503);

  w.state.network = () => response('secret', { cacheControl: 'no-store' });
  await w.fetch('/assets/no-store.js');
  w.state.network = () => response('secret', { cacheControl: 'max-age=60, private' });
  await w.fetch('/assets/private.js');
  assert.ok(!store().has(`${ORIGIN}/assets/no-store.js`) && !store().has(`${ORIGIN}/assets/private.js`));

  w.state.network = () => response('shell');
  await w.fetch('/', { mode: 'navigate' });
  w.state.network = (request) => response(request.url);
  for (let i = 0; i < 80; i++) await w.fetch(`/assets/chunk-${i}.js`);
  assert.equal(store().size, 65);
  assert.ok(store().has(SCOPE), 'shell trimmed');
  assert.ok(store().has(`${ORIGIN}/assets/chunk-79.js`) && !store().has(`${ORIGIN}/assets/chunk-0.js`));
}

// Activation drops this scope's older caches (earlier versions and names), not other scopes'.
{
  const other = encodeURIComponent(ORIGIN + '/preview/');
  const stores = new Map([
    [`foundation-shell:${encodedScope}:v1`, new Map()],
    [`probe-shell:${encodedScope}:v1`, new Map()],
    [`probe-shell:${encodedScope}:v2`, new Map()],
    [`probe-shell:${other}:v2`, new Map()],
    ['app-data', new Map()],
  ]);
  await worker({ query: '?name=probe', stores }).activate();
  assert.deepEqual([...stores.keys()].sort(), [`probe-shell:${encodedScope}:v2`, `probe-shell:${other}:v2`, 'app-data'].sort());
}

console.log('sw.js ok');
