// Module `busy`: the gate between a busy flag and its cue (delay, minimum visible time), on a fake
// clock. Started by TestFrontendModuleLogic; the React layer is a thin wrapper around it.
import assert from 'node:assert/strict';
import { load } from './ts.mjs';

const { busyGate } = await load('busy/busy.ts');

function fakeClock() {
  let now = 0;
  let next = 1;
  const timers = new Map();
  return {
    now: () => now,
    setTimeout(run, ms) {
      const id = next++;
      timers.set(id, { at: now + ms, run });
      return id;
    },
    clearTimeout(id) {
      timers.delete(id);
    },
    advance(ms) {
      const end = now + ms;
      for (;;) {
        const due = [...timers].filter(([, t]) => t.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        timers.delete(due[0]);
        now = due[1].at;
        due[1].run();
      }
      now = end;
    },
    pending: () => timers.size,
  };
}

function gate(timing) {
  const clock = fakeClock();
  const changes = [];
  const g = busyGate(timing, (shown) => changes.push([clock.now(), shown]), clock);
  return { g, clock, changes };
}

// A fast answer never shows the cue, and leaves no timer behind.
{
  const { g, clock, changes } = gate({ delay: 300, minVisible: 500 });
  g.set(true);
  clock.advance(299);
  g.set(false);
  clock.advance(1000);
  assert.deepEqual(changes, [], 'busy shorter than the delay shows nothing');
  assert.equal(clock.pending(), 0);
  assert.equal(g.shown, false);
}

// A slow one shows after the delay and stays at least minVisible.
{
  const { g, clock, changes } = gate({ delay: 300, minVisible: 500 });
  g.set(true);
  clock.advance(300);
  assert.equal(g.shown, true);
  clock.advance(100);
  g.set(false);
  assert.equal(g.shown, true, 'held for the minimum');
  clock.advance(399);
  assert.equal(g.shown, true);
  clock.advance(1);
  assert.deepEqual(changes, [[300, true], [800, false]]);
}

// Shown longer than minVisible: hidden at once when busy ends.
{
  const { g, clock, changes } = gate({ delay: 100, minVisible: 200 });
  g.set(true);
  clock.advance(1000);
  g.set(false);
  assert.deepEqual(changes, [[100, true], [1000, false]]);
}

// Busy again while a shown cue waits out its minimum: it stays, without a new delay or flicker.
{
  const { g, clock, changes } = gate({ delay: 100, minVisible: 500 });
  g.set(true);
  clock.advance(100);
  g.set(false);
  clock.advance(200);
  g.set(true);
  clock.advance(1000);
  assert.deepEqual(changes, [[100, true]], 'never hidden in between');
  g.set(false);
  assert.deepEqual(changes, [[100, true], [1300, false]]);
}

// Delay 0 shows at once; repeated values change nothing.
{
  const { g, clock, changes } = gate({ delay: 0, minVisible: 0 });
  g.set(true);
  g.set(true);
  assert.deepEqual(changes, [[0, true]]);
  g.set(false);
  g.set(false);
  assert.deepEqual(changes, [[0, true], [0, false]]);
  assert.equal(clock.pending(), 0);
}

// Negative timings count as 0.
{
  const { g, changes } = gate({ delay: -5, minVisible: -5 });
  g.set(true);
  g.set(false);
  assert.deepEqual(changes, [[0, true], [0, false]]);
}

// Disposed: pending timers go, nothing is reported any more.
{
  const { g, clock, changes } = gate({ delay: 100, minVisible: 0 });
  g.set(true);
  g.dispose();
  clock.advance(500);
  g.set(false);
  assert.deepEqual(changes, []);
  assert.equal(clock.pending(), 0);
}

// The default clock works with real timers.
{
  const changes = [];
  const g = busyGate({ delay: 10, minVisible: 20 }, (shown) => changes.push(shown));
  g.set(true);
  await new Promise((r) => setTimeout(r, 30));
  g.set(false);
  await new Promise((r) => setTimeout(r, 40));
  assert.deepEqual(changes, [true, false]);
}

console.log('busy ok');
