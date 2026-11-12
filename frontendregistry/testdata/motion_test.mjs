// Loads the motion module (argv[2], TypeScript run through Node's type stripping) with React and
// React DOM stubbed, and checks its browser-independent parts: the reduced-motion query, the view
// transition fallbacks, the indicator style and animationsSettled. The hooks are exercised in a
// scratch app (MIGRATING.md). Started by TestMotion; exits non-zero on the first failure.
import { registerHooks } from 'node:module';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';

const flushed = [];
const stubs = {
  react: 'export const useEffect = () => {}; export const useLayoutEffect = () => {}; export const useState = (v) => [typeof v === \'function\' ? v() : v, () => {}]; export const useSyncExternalStore = (s, get) => get();',
  'react-dom': 'export const flushSync = (fn) => { globalThis.__flushed.push(true); return fn(); };',
};
globalThis.__flushed = flushed;
registerHooks({
  resolve(specifier, context, next) {
    if (specifier in stubs) return { url: 'stub:' + specifier, shortCircuit: true };
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url.startsWith('stub:')) return { format: 'module', source: stubs[url.slice(5)], shortCircuit: true };
    return next(url, context);
  },
});

const motion = await import(pathToFileURL(process.argv[2]).href);

let reduce = false;
globalThis.matchMedia = (query) => ({ matches: query === '(prefers-reduced-motion: reduce)' && reduce, addEventListener() {}, removeEventListener() {} });

// prefersReducedMotion follows the media query and is false without matchMedia.
assert.equal(motion.prefersReducedMotion(), false);
reduce = true;
assert.equal(motion.prefersReducedMotion(), true);
reduce = false;

// viewTransition: without the API, under reduced motion and on a hidden page it only runs the update.
const fakeDocument = (extra = {}) => ({ visibilityState: 'visible', ...extra });
let ran = 0;
globalThis.document = fakeDocument();
await motion.viewTransition(() => ran++);
assert.equal(ran, 1, 'plain update without startViewTransition');

let started = 0;
const startViewTransition = (cb) => { started++; cb(); return { finished: Promise.resolve() }; };
globalThis.document = fakeDocument({ startViewTransition });
reduce = true;
await motion.viewTransition(() => ran++);
assert.equal(started, 0, 'no transition under reduced motion');
assert.equal(ran, 2);
reduce = false;

globalThis.document = fakeDocument({ startViewTransition, visibilityState: 'hidden' });
await motion.viewTransition(() => ran++);
assert.equal(started, 0, 'no transition on a hidden page');
assert.equal(ran, 3);

// With the API, the update runs inside the transition, flushed synchronously.
globalThis.document = fakeDocument({ startViewTransition });
flushed.length = 0;
await motion.viewTransition(() => ran++);
assert.equal(started, 1);
assert.equal(ran, 4);
assert.equal(flushed.length, 1, 'update is flushed with flushSync inside the transition');

// A skipped transition (finished rejects) still resolves; a throwing start falls back to the update.
globalThis.document = fakeDocument({ startViewTransition: (cb) => { cb(); return { finished: Promise.reject(new Error('skipped')) }; } });
await motion.viewTransition(() => ran++);
assert.equal(ran, 5);
globalThis.document = fakeDocument({ startViewTransition: () => { throw new Error('InvalidStateError'); } });
await motion.viewTransition(() => ran++);
assert.equal(ran, 6);

// indicatorStyle per axis, hidden without a box.
const box = { x: 10, y: 20, w: 30, h: 40 };
assert.deepEqual(motion.indicatorStyle(null), { visibility: 'hidden' });
assert.deepEqual(motion.indicatorStyle(box), { transform: 'translateX(10px)', width: 30 });
assert.deepEqual(motion.indicatorStyle(box, 'y'), { transform: 'translateY(20px)', height: 40 });
assert.deepEqual(motion.indicatorStyle(box, 'box'), { transform: 'translate(10px, 20px)', width: 30, height: 40 });
assert.equal(motion.sameBox(box, { ...box }), true);
assert.equal(motion.sameBox(box, { ...box, w: 31 }), false);
assert.equal(motion.sameBox(null, null), true);
assert.equal(motion.sameBox(box, null), false);

// animationsSettled waits for finite animations, ignores infinite ones and gives up after maxMs.
const animation = (iterations, finished) => ({ effect: { getTiming: () => ({ iterations }) }, finished });
let resolveFinite;
const finite = new Promise((resolve) => { resolveFinite = resolve; });
let settled = false;
const pending = motion.animationsSettled({ getAnimations: () => [animation(1, finite), animation(Infinity, new Promise(() => {}))] }, 10_000).then(() => { settled = true; });
await new Promise((resolve) => setTimeout(resolve, 20));
assert.equal(settled, false, 'waits for the finite animation');
resolveFinite();
await pending;
assert.equal(settled, true);
await motion.animationsSettled({ getAnimations: () => [animation(Infinity, new Promise(() => {}))] }); // nothing finite: at once
const start = Date.now();
await motion.animationsSettled({ getAnimations: () => [animation(1, new Promise(() => {}))] }, 30);
assert.ok(Date.now() - start < 1000, 'maxMs caps the wait');
await motion.animationsSettled({}); // no getAnimations: at once

// once: 'session': the first call opens a window for everything mounting with the first view;
// once the page is idle, later calls say no.
{
  const idle = [];
  globalThis.requestIdleCallback = (fn, options) => { assert.equal(options.timeout, 1500); idle.push(fn); };
  motion.resetSessionEntrance();
  assert.equal(motion.sessionEntranceOpen(), true, 'the first view plays');
  assert.equal(motion.sessionEntranceOpen(), true, 'its siblings in the same task play too');
  assert.equal(idle.length, 1, 'one window');
  idle.shift()();
  assert.equal(motion.sessionEntranceOpen(), false, 'a later view does not');
  assert.equal(idle.length, 0);
  motion.resetSessionEntrance();
  delete globalThis.requestIdleCallback;
  assert.equal(motion.sessionEntranceOpen(), true);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(motion.sessionEntranceOpen(), false, 'without requestIdleCallback the window closes after the task');
}

// useEntrance (React stubbed: useState runs its initializer, effects never run): with once 'session'
// a component mounting after the first view starts out done; the default and a number still enter.
{
  const ref = { current: null };
  motion.resetSessionEntrance();
  globalThis.requestIdleCallback = () => {};
  assert.equal(motion.useEntrance(ref, true, { once: 'session' }), true, 'first view enters');
  globalThis.requestIdleCallback = (fn) => fn();
  motion.resetSessionEntrance();
  motion.sessionEntranceOpen();
  assert.equal(motion.useEntrance(ref, true, { once: 'session' }), false, 'later view does not');
  assert.equal(motion.useEntrance(ref, true), true, 'default: once per mount');
  assert.equal(motion.useEntrance(ref, true, 800), true, 'maxMs as a number still works');
  assert.equal(motion.useEntrance(ref, false, { once: 'session' }), false, 'nothing before ready');
  reduce = true;
  motion.resetSessionEntrance();
  assert.equal(motion.useEntrance(ref, true, { once: 'session' }), false, 'reduced motion never enters');
  reduce = false;
  delete globalThis.requestIdleCallback;
}

console.log('motion ok');
