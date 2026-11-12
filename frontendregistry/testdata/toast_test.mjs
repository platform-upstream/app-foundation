// The store of the registry module `toast` (argv[2]: sources/toast/toast.ts), run by TestToastStore.
// Needs a Node that strips TypeScript types. The region (popover, modal hosting) runs in a browser.
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { setTimeout as sleep } from 'node:timers/promises';

const t = await import(pathToFileURL(process.argv[2]).href);
t.configureToasts({ max: 2, ms: 40, actionMs: 120, errorMs: 80, undoLabel: 'Rückgängig' });

let changes = 0;
const unsubscribe = t.subscribeToasts(() => changes++);

// Roles: errors are alerts, everything else polite status.
t.toast('Gespeichert', { tone: 'success' });
const before = t.getToasts();
assert.equal(t.getToasts(), before, 'the same array until something changes (useSyncExternalStore)');
assert.equal(before[0].role, 'status');
assert.equal(before[0].tone, 'success');
t.clearToasts();
t.toast('Fehlgeschlagen', { tone: 'error' });
assert.equal(t.getToasts()[0].role, 'alert');
t.clearToasts();

// The queue keeps the newest `max`.
t.toast('a', { ms: Infinity });
t.toast('b', { ms: Infinity });
t.toast('c', { ms: Infinity });
assert.deepEqual(t.getToasts().map((x) => x.text), ['b', 'c']);
t.clearToasts();

// Durations: plain, error, with an action; Infinity stays.
t.toast('plain');
t.toast('error', { tone: 'error' });
await sleep(60);
assert.deepEqual(t.getToasts().map((x) => x.text), ['error'], 'the plain one went first');
await sleep(40);
assert.equal(t.getToasts().length, 0);

// Undo: labelled with the app's copy, dismisses before it runs, and gets the longer duration.
let undone = 0;
t.toast('Gelöscht', { undo: () => { assert.equal(t.getToasts().length, 0, 'dismissed before the undo runs'); undone++; } });
const [withUndo] = t.getToasts();
assert.equal(withUndo.action.label, 'Rückgängig');
await sleep(60);
assert.equal(t.getToasts().length, 1, 'an undo toast outlives a plain one');
withUndo.action.run();
assert.equal(undone, 1);

// The returned function dismisses early; dismissing twice is harmless.
const dismiss = t.toast('weg', { ms: Infinity });
dismiss();
dismiss();
assert.equal(t.getToasts().length, 0);

// Pause holds the remaining time and resumes it.
t.toast('pausiert', { ms: 150 });
await sleep(50);
t.pauseToasts(true);
await sleep(200);
assert.equal(t.getToasts().length, 1, 'paused: still there after its time');
t.pauseToasts(false);
await sleep(30);
assert.equal(t.getToasts().length, 1, 'resumed with the remaining ~100 ms');
await sleep(150);
assert.equal(t.getToasts().length, 0);

// A toast shown while paused waits too.
t.pauseToasts(true);
t.toast('später', { ms: 10 });
await sleep(30);
assert.equal(t.getToasts().length, 1);
t.pauseToasts(false);
await sleep(30);
assert.equal(t.getToasts().length, 0);

assert.ok(changes > 10);
unsubscribe();
console.log('toast ok');
