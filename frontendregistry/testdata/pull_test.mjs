// The logic of the registry module `pull-refresh` (argv[2]: sources/pull-refresh/pull.ts), run by
// TestPullToRefreshLogic. Needs a Node that strips TypeScript types. Real touches run in a browser.
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { setTimeout as sleep } from 'node:timers/promises';

const p = await import(pathToFileURL(process.argv[2]).href);
const { trigger, max } = p.pullSettings();

// Damping: half the finger travel, capped; never for upward or missing movement.
assert.equal(p.pullDistance(40), 20);
assert.equal(p.pullDistance(trigger * 2), trigger);
assert.equal(p.pullDistance(10_000), max);
assert.equal(p.pullDistance(0), 0);
assert.equal(p.pullDistance(-30), 0);
assert.equal(p.pullDistance(Number.NaN), 0);
assert.equal(p.pullDistance(100, { damping: 1, max: 80 }), 80, 'app options count');

// The slop, then a pull only when down and more vertical than sideways.
assert.equal(p.pullIntent(3, 9), 'undecided');
assert.equal(p.pullIntent(4, 24), 'pull');
assert.equal(p.pullIntent(30, 12), 'cancel', 'a sideways swipe (carousel, tabs)');
assert.equal(p.pullIntent(-30, 12), 'cancel');
assert.equal(p.pullIntent(20, 20), 'cancel', 'a diagonal is no pull');
assert.equal(p.pullIntent(0, -20), 'cancel', 'scrolling the page');

// On only for a touch screen running as a Home Screen app.
const win = (queries, standalone) => ({
  matchMedia: (q) => ({ matches: queries.includes(q) }),
  navigator: { standalone },
});
assert.equal(p.pullEnabled(win(['(display-mode: standalone)', '(pointer: coarse)'])), true);
assert.equal(p.pullEnabled(win(['(pointer: coarse)'], true)), true, 'iOS navigator.standalone');
assert.equal(p.pullEnabled(win(['(pointer: coarse)'])), false, 'a browser tab has its own');
assert.equal(p.pullEnabled(win(['(display-mode: standalone)'])), false, 'a desktop install');

// Off while a dialog is open.
assert.equal(p.dialogOpen({ querySelector: () => null }), false);
assert.equal(p.dialogOpen({ querySelector: (s) => (s === 'dialog:modal' ? {} : null) }), true);
assert.equal(p.dialogOpen({ querySelector: (s) => (s === '[aria-modal="true"]' ? {} : null) }), true);
assert.equal(p.dialogOpen({ querySelector: (s) => { if (s.includes(':modal')) throw new SyntaxError(); return s === 'dialog[open]' ? {} : null; } }), true, 'old engines without :modal');

// The gesture on a fake scroller: passive listeners only, one finger, from the top.
globalThis.document = { querySelector: () => null };
function scroller() {
  const listeners = {};
  return {
    scrollTop: 0,
    listeners,
    addEventListener(type, fn, options) {
      assert.equal(options?.passive, true, `${type} must be passive (no preventDefault on the bounce)`);
      listeners[type] = fn;
    },
    removeEventListener(type) { delete listeners[type]; },
    touch(type, points) { this.listeners[type]?.({ touches: points.map(([clientX, clientY]) => ({ clientX, clientY })) }); },
  };
}
const el = scroller();
const states = [];
let refreshed = 0;
let finish;
const detach = p.attachPullToRefresh(el, () => { refreshed++; return new Promise((resolve) => { finish = resolve; }); }, (s) => states.push(s), { enabled: true, minSpin: 30 });

// Sideways first: never a pull, however far down it goes afterwards.
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[130, 112]]);
el.touch('touchmove', [[130, 400]]);
el.touch('touchend', []);
assert.equal(states.length, 0);
assert.equal(refreshed, 0);

// Not from the top: nothing.
el.scrollTop = 50;
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[100, 300]]);
el.touch('touchend', []);
el.scrollTop = 0;
assert.equal(states.length, 0);

// A short pull settles back without refreshing.
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[102, 160]]);
assert.deepEqual(states.at(-1), { pull: 30, ready: false, busy: false });
el.touch('touchend', []);
assert.deepEqual(states.at(-1), { pull: 0, ready: false, busy: false });
assert.equal(refreshed, 0);

// A second finger cancels.
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[100, 200]]);
el.touch('touchmove', [[100, 260], [200, 260]]);
el.touch('touchend', []);
assert.equal(refreshed, 0);
assert.equal(states.at(-1).pull, 0);

// While a dialog is open: nothing.
globalThis.document = { querySelector: (s) => (s === 'dialog:modal' ? {} : null) };
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[100, 400]]);
el.touch('touchend', []);
assert.equal(refreshed, 0);
globalThis.document = { querySelector: () => null };

// Past the trigger: ready, then busy at the trigger until the refresh settles and minSpin passed.
const before = states.length;
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[100, 300]]);
assert.deepEqual(states.at(-1), { pull: 100, ready: true, busy: false });
el.touch('touchend', []);
assert.deepEqual(states.at(-1), { pull: trigger, ready: true, busy: true });
await sleep(0);
assert.equal(refreshed, 1);
el.touch('touchstart', [[100, 100]]);
el.touch('touchmove', [[100, 400]]);
el.touch('touchend', []);
await sleep(0);
assert.equal(refreshed, 1, 'no second refresh while busy');
await sleep(50);
assert.equal(states.at(-1).busy, true, 'still busy: the refresh has not settled');
finish();
await sleep(5);
assert.deepEqual(states.at(-1), { pull: 0, ready: false, busy: false });
assert.ok(states.length > before);

// A failing refresh ends busy too; minSpin holds a fast one.
const el2 = scroller();
const states2 = [];
p.attachPullToRefresh(el2, () => Promise.reject(new Error('offline')), (s) => states2.push(s), { enabled: true, minSpin: 40 });
el2.touch('touchstart', [[0, 0]]);
el2.touch('touchmove', [[0, 200]]);
el2.touch('touchend', []);
await sleep(10);
assert.equal(states2.at(-1).busy, true, 'held for minSpin');
await sleep(50);
assert.equal(states2.at(-1).busy, false);

// Detached: no listeners left; off without standalone touch.
detach();
assert.deepEqual(Object.keys(el.listeners), []);
globalThis.window = win([]);
const el3 = scroller();
p.attachPullToRefresh(el3, () => {}, () => {});
assert.deepEqual(Object.keys(el3.listeners), [], 'not attached in a browser tab');

console.log('pull-refresh ok');
