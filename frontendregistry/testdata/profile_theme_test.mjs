// Module `profile`, profile-theme.js: the stored appearance before the first paint and the
// theme-color sync, in a VM with a fake document. Started by TestFrontendModuleLogic.
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
import { source } from './ts.mjs';

const script = readFileSync(source('profile/profile-theme.js'), 'utf8');

function element(attrs = {}) {
  const map = new Map(Object.entries(attrs));
  return {
    style: {},
    getAttribute: (k) => (map.has(k) ? map.get(k) : null),
    setAttribute: (k, v) => map.set(k, String(v)),
    removeAttribute: (k) => map.delete(k),
    has: (k) => map.has(k),
  };
}

function page({ stored = {}, systemDark = false, storage = 'ok', legacyKey, metas = [], matchMediaThrows = false } = {}) {
  const root = element();
  let observer = null;
  const localStorage = {
    getItem(k) {
      if (storage === 'blocked') throw new Error('SecurityError');
      return k in stored ? stored[k] : null;
    },
  };
  const document = {
    documentElement: root,
    currentScript: element(legacyKey ? { 'data-legacy-key': legacyKey } : {}),
    querySelectorAll: () => metas,
  };
  const window = {
    matchMedia: (q) => {
      if (matchMediaThrows) throw new Error('no matchMedia');
      return { matches: q === '(prefers-color-scheme: dark)' && systemDark };
    },
  };
  class MutationObserver {
    constructor(fn) { this.fn = fn; }
    observe(target, options) { observer = { fn: this.fn, target, options }; }
  }
  vm.runInContext(script, vm.createContext({ document, window, localStorage, MutationObserver }), { filename: 'profile-theme.js' });
  return {
    root,
    theme: () => root.getAttribute('data-theme'),
    // What profile.tsx does later: set data-theme, the observer repaints.
    switchTo(theme) { root.setAttribute('data-theme', theme); observer.fn([]); },
    observer: () => observer,
  };
}

// The stored choice wins; auto and nothing follow the system.
for (const [stored, systemDark, want] of [
  [{ 'profile.theme': 'dark' }, false, 'dark'],
  [{ 'profile.theme': 'light' }, true, 'light'],
  [{ 'profile.theme': 'auto' }, true, 'dark'],
  [{ 'profile.theme': 'auto' }, false, 'light'],
  [{}, true, 'dark'],
  [{}, false, 'light'],
  [{ 'profile.theme': 'nonsense' }, true, 'dark'],
]) {
  const p = page({ stored, systemDark });
  assert.equal(p.theme(), want, JSON.stringify({ stored, systemDark }));
  assert.equal(p.root.style.colorScheme, want);
}

// An app's older key counts only while profile.theme is not set.
assert.equal(page({ stored: { 'acme-theme': 'dark' }, legacyKey: 'acme-theme' }).theme(), 'dark');
assert.equal(page({ stored: { 'acme-theme': 'dark', 'profile.theme': 'light' }, legacyKey: 'acme-theme' }).theme(), 'light');
assert.equal(page({ stored: { 'acme-theme': 'dark' } }).theme(), 'light', 'no legacy key named');

// Blocked storage or a missing matchMedia: light, no exception.
assert.equal(page({ storage: 'blocked', systemDark: true }).theme(), 'dark', 'blocked storage still follows the system');
assert.equal(page({ matchMediaThrows: true }).theme(), 'light');

// theme-color: metas with data-light/data-dark follow data-theme, now and later; others are left.
{
  const synced = element({ name: 'theme-color', content: '#ffffff', 'data-light': '#ffffff', 'data-dark': '#16181d', media: '(prefers-color-scheme: light)' });
  const own = element({ name: 'theme-color', content: '#123456' });
  const p = page({ stored: { 'profile.theme': 'dark' }, metas: [synced, own] });
  assert.equal(synced.getAttribute('content'), '#16181d');
  assert.equal(synced.has('media'), false, 'media dropped: the choice may differ from the system');
  assert.equal(own.getAttribute('content'), '#123456');
  assert.deepEqual(JSON.parse(JSON.stringify(p.observer().options)), { attributes: true, attributeFilter: ['data-theme'] });
  assert.equal(p.observer().target, p.root);
  p.switchTo('light');
  assert.equal(synced.getAttribute('content'), '#ffffff');
  p.switchTo('dark');
  assert.equal(synced.getAttribute('content'), '#16181d');
}

console.log('profile-theme ok');
