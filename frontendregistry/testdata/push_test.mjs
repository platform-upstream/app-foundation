// Module `push`: sw-push.js in a VM with a fake worker scope, and push.ts's plain functions.
// Started by TestFrontendModuleLogic with the sources directory; exits non-zero on a failure.
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import { load, source } from './ts.mjs';

const ORIGIN = 'https://app.example.org';
// Objects from the VM have its prototypes: compare their JSON.
const plain = (value) => JSON.parse(JSON.stringify(value));
const SCOPE = ORIGIN + '/';

function worker({ badge = true, windows = [] } = {}) {
  const state = { shown: [], badges: [], opened: [], log: [] };
  const navigator = badge
    ? {
        setAppBadge: async (n) => { state.badges.push(n); },
        clearAppBadge: async () => { state.badges.push(0); },
      }
    : {};
  const self = {
    location: { href: SCOPE + 'sw.js', origin: ORIGIN },
    registration: { scope: SCOPE, showNotification: async (title, options) => { state.shown.push({ title, options }); } },
    navigator,
    clients: {
      matchAll: async () => windows,
      openWindow: async (url) => { state.opened.push(url); state.log.push(`open ${url}`); },
    },
  };
  vm.runInContext(readFileSync(source('push/sw-push.js'), 'utf8'), vm.createContext({ self, URL, Promise, Array, Object, Number, Math, JSON }), { filename: 'sw-push.js' });
  return { push: self.FoundationPush, state };
}

const pushEvent = (payload) => {
  const event = { waits: [], waitUntil(p) { this.waits.push(p); } };
  if (payload !== undefined) event.data = { text: () => (typeof payload === 'string' ? payload : JSON.stringify(payload)) };
  return event;
};

// Payload: validated and cut; tag from tag or id; badge counted; fallback title; same-origin URL.
{
  const { push, state } = worker();
  const settings = { fallbackTitle: 'Acme', icon: '/pwa/icon-192.png' };
  await push.handlePush(pushEvent({ title: 'Invoice due', body: 'Pay 42', url: '/invoices/7', tag: 'invoice:7', badge: 3 }), settings);
  assert.deepEqual(plain(state.shown[0]), { title: 'Invoice due', options: { body: 'Pay 42', tag: 'invoice:7', icon: '/pwa/icon-192.png', data: { url: SCOPE + 'invoices/7' } } });
  assert.deepEqual(state.badges, [3]);

  // Console/Paper send the subject as `id`.
  assert.equal(push.parsePush({ id: 'incident-9', title: 't' }).options.tag, 'incident-9');

  for (const raw of [undefined, '', '{broken', '[1,2]', 'null', JSON.stringify({ title: 42, body: {} })]) {
    const parsed = push.parsePush(raw, settings);
    assert.equal(parsed.title, 'Acme', String(raw));
    assert.equal(parsed.options.body, '');
    assert.equal(parsed.options.tag, undefined);
    assert.equal(parsed.options.data.url, SCOPE);
    assert.equal(parsed.badge, undefined);
  }
  assert.equal(push.parsePush(undefined).title, 'Notification');

  const long = push.parsePush({ title: 'x'.repeat(300), body: '😀'.repeat(600), tag: 't'.repeat(200) });
  assert.equal(Array.from(long.title).length, 120);
  assert.ok(long.title.endsWith('…'));
  assert.equal(Array.from(long.options.body).length, 500);
  assert.ok(long.options.body.startsWith('😀') && !long.options.body.includes('�'), 'emoji split');
  assert.equal(long.options.tag.length, 120);
  assert.equal(push.parsePush({ title: '  padded  ' }).title, 'padded');

  for (const [value, want] of [[0, 0], [7.9, 7], ['12', 12], [-1, undefined], [NaN, undefined], ['3a', undefined], [null, undefined], [Infinity, undefined]]) {
    assert.equal(push.parsePush({ badge: value }).badge, want, `badge ${value}`);
  }

  for (const url of ['https://evil.example.org/x', '//evil.example.org/x', '/\\evil.example.org', 'javascript:alert(1)', ' /x', 'https://app.example.org.evil.org/']) {
    assert.equal(push.parsePush({ url }).options.data.url, SCOPE, url);
  }
  assert.equal(push.parsePush({ url: `${ORIGIN}/a?b=1#c` }).options.data.url, `${ORIGIN}/a?b=1#c`);
  assert.equal(push.parsePush({ url: 'notes/3' }).options.data.url, `${SCOPE}notes/3`);
}

// Declarative Web Push: the fields under notification, navigate and app_badge.
{
  const parsed = worker().push.parsePush({ web_push: 8030, notification: { title: 'Ready', body: 'Edition', navigate: '/today', tag: 'edition', app_badge: '5' } });
  assert.deepEqual(plain(parsed), { title: 'Ready', options: { body: 'Edition', tag: 'edition', data: { url: SCOPE + 'today' } }, badge: 5 });
}

// Badge: zero clears, none leaves it; a worker without the Badging API or a refusal still shows.
{
  const { push, state } = worker();
  await push.handlePush(pushEvent({ title: 'a', badge: 0 }));
  await push.handlePush(pushEvent({ title: 'b' }));
  assert.deepEqual(state.badges, [0]);

  const bare = worker({ badge: false });
  await bare.push.handlePush(pushEvent({ title: 'c', badge: 4 }));
  assert.equal(bare.state.shown.length, 1);

  const refusing = worker();
  refusing.state.badges = null; // setAppBadge now throws (badges.push on null)
  const event = pushEvent({ title: 'd', badge: 2 });
  await refusing.push.handlePush(event);
  assert.equal(refusing.state.shown.length, 1);
  assert.equal(event.waits.length, 1, 'waitUntil');
}

// Click: an open window is focused first, then navigated; else a new window opens; never off-origin.
{
  const log = [];
  const client = (url, { controlled = true } = {}) => ({
    url,
    async focus() { log.push(`focus ${url}`); return this; },
    async navigate(target) { log.push(`navigate ${target}`); if (!controlled) throw new TypeError('not controlled'); return this; },
  });
  const click = (data) => ({ closed: false, waits: [], notification: { data, close() { this.closed = true; } }, waitUntil(p) { this.waits.push(p); } });

  const { push } = worker({ windows: [client('https://other.example.org/'), client(SCOPE + 'home')] });
  const event = click({ url: SCOPE + 'invoices/7' });
  await push.handleClick(event);
  assert.ok(event.notification.closed);
  assert.deepEqual(log, [`focus ${SCOPE}home`, `navigate ${SCOPE}invoices/7`]);

  log.length = 0;
  await push.handleClick(click({ url: 'https://evil.example.org/' }));
  assert.deepEqual(log, [`focus ${SCOPE}home`, `navigate ${SCOPE}`]);

  log.length = 0;
  await worker({ windows: [client(SCOPE + 'x')] }).push.handleClick(click({ url: SCOPE + 'x' }));
  assert.deepEqual(log, [`focus ${SCOPE}x`], 'already there: no navigation');

  log.length = 0;
  await worker({ windows: [client(SCOPE + 'x', { controlled: false })] }).push.handleClick(click({ url: '/y' }));
  assert.deepEqual(log, [`focus ${SCOPE}x`, `navigate ${SCOPE}y`], 'a rejected navigate is swallowed');

  const none = worker({ windows: [client('https://other.example.org/')] });
  await none.push.handleClick(click(null));
  assert.deepEqual(none.state.opened, [SCOPE]);
  const fresh = worker();
  await fresh.push.handleClick(click({ url: '//evil.example.org/' }));
  assert.deepEqual(fresh.state.opened, [SCOPE]);
}

// Page side: badge, VAPID key bytes, early states.
{
  const { setAppBadge, applicationServerKey, pushStateFrom } = await load('push/push.ts');
  const calls = [];
  const nav = { setAppBadge: async (n) => { calls.push(n); }, clearAppBadge: async () => { calls.push('clear'); } };
  setAppBadge(4.7, nav);
  setAppBadge(0, nav);
  setAppBadge(NaN, nav);
  setAppBadge(2, {});
  setAppBadge(2, undefined);
  setAppBadge(1, { setAppBadge: () => Promise.reject(new Error('denied')) });
  setAppBadge(1, { setAppBadge: () => { throw new Error('sync'); } });
  assert.deepEqual(calls, [4, 'clear']);

  const key = applicationServerKey('BPx-_w');
  assert.deepEqual([...key], [0x04, 0xfc, 0x7e, 0xff]);
  assert.equal(key.buffer.byteLength, 4);

  const env = { serviceWorker: true, pushManager: true, standalone: false, ios: false, permission: 'default' };
  assert.equal(pushStateFrom(env), null);
  assert.equal(pushStateFrom({ ...env, ios: true }), 'needs-install');
  assert.equal(pushStateFrom({ ...env, ios: true, standalone: true }), null);
  assert.equal(pushStateFrom({ ...env, pushManager: false }), 'unsupported');
  assert.equal(pushStateFrom({ ...env, permission: 'unsupported' }), 'unsupported');
  assert.equal(pushStateFrom({ ...env, permission: 'denied' }), 'denied');
}

console.log('push ok');
