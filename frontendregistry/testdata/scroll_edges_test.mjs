// Module `scroll-edges`: when a sideways scroller has more before and after the view.
// Started by TestFrontendModuleLogic; the hook itself is exercised in the acceptance install.
import assert from 'node:assert/strict';
import { load } from './ts.mjs';

const { scrollEdges } = await load('scroll-edges/scroll-edges.ts');

assert.deepEqual(scrollEdges(0, 300, 300), { start: false, end: false }, 'fits');
assert.deepEqual(scrollEdges(0, 300, 301), { start: false, end: false }, 'within the 2 px slack');
assert.deepEqual(scrollEdges(0, 300, 900), { start: false, end: true }, 'at the start');
assert.deepEqual(scrollEdges(300, 300, 900), { start: true, end: true }, 'in the middle');
assert.deepEqual(scrollEdges(600, 300, 900), { start: true, end: false }, 'at the end');
assert.deepEqual(scrollEdges(599.5, 300, 900), { start: true, end: false }, 'subpixel end');
assert.deepEqual(scrollEdges(1.5, 300, 900), { start: false, end: true }, 'subpixel start');
assert.deepEqual(scrollEdges(-300, 300, 900), { start: true, end: true }, 'right-to-left');

console.log('scroll-edges ok');
