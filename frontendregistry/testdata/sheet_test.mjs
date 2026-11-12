// The logic of the registry module `sheet` (argv[2]: sources/sheet/sheet.ts), run by TestSheetLogic.
// Needs a Node that strips TypeScript types. The dialog, focus and drag run in a browser (docs).
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';

const { sheetDragRelease, dragVelocity, lockScroll } = await import(pathToFileURL(process.argv[2]).href);

// Letting go: past the threshold, or a flick, closes; a slow short drag settles back.
assert.equal(sheetDragRelease(81, 0), 'close');
assert.equal(sheetDragRelease(80, 0), 'settle');
assert.equal(sheetDragRelease(40, 0.8), 'close');
assert.equal(sheetDragRelease(15, 2), 'settle', 'a tap-sized movement is never a flick');
assert.equal(sheetDragRelease(60, 0.3), 'settle');
assert.equal(sheetDragRelease(120, 0, 150), 'settle', 'the app threshold counts');

// Velocity comes from the last 100 ms, not from the whole drag: a slow start and a fast end flicks.
assert.equal(dragVelocity([]), 0);
assert.equal(dragVelocity([{ t: 0, y: 0 }]), 0);
const slowThenFast = [{ t: 0, y: 0 }, { t: 400, y: 10 }, { t: 450, y: 40 }, { t: 500, y: 70 }];
assert.ok(dragVelocity(slowThenFast) > 0.5, `velocity ${dragVelocity(slowThenFast)}`);
assert.ok(70 / 500 < 0.5, 'the average over the whole drag would have missed the flick');
assert.ok(dragVelocity([{ t: 0, y: 0 }, { t: 50, y: 40 }, { t: 400, y: 42 }]) < 0.1, 'a pause before letting go is no flick');

// The lock: on .app-shell-main by default (never body), ref-counted, restores the inline styles.
function element({ scrollbar = 0 } = {}) {
  const attributes = new Map();
  return {
    style: { overflow: 'auto', scrollbarGutter: '' },
    offsetWidth: 400 + scrollbar, clientWidth: 400, scrollHeight: 2000, clientHeight: 800,
    setAttribute: (name, value) => attributes.set(name, value),
    removeAttribute: (name) => attributes.delete(name),
    hasAttribute: (name) => attributes.has(name),
  };
}
const main = element({ scrollbar: 15 });
const body = element();
globalThis.document = {
  querySelector: (selector) => (selector === '.app-shell-main' ? main : null),
  scrollingElement: body,
};
const first = lockScroll();
const second = lockScroll();
assert.equal(main.style.overflow, 'hidden');
assert.equal(main.style.scrollbarGutter, 'stable', 'a classic scrollbar keeps its gutter');
assert.ok(main.hasAttribute('data-scroll-locked'));
assert.equal(body.style.overflow, 'auto', 'body is not the scroller in the shell');
first();
first();
assert.equal(main.style.overflow, 'hidden', 'released twice by one caller: the other still holds it');
second();
assert.equal(main.style.overflow, 'auto');
assert.equal(main.style.scrollbarGutter, '');
assert.ok(!main.hasAttribute('data-scroll-locked'));

// Without the shell: the document scroller; an explicit scroller wins.
globalThis.document.querySelector = () => null;
const release = lockScroll();
assert.equal(body.style.overflow, 'hidden');
assert.equal(body.style.scrollbarGutter, '', 'no classic scrollbar, no gutter');
release();
assert.equal(body.style.overflow, 'auto');
const own = element();
lockScroll(own)();
assert.equal(own.style.overflow, 'auto');

console.log('sheet ok');
