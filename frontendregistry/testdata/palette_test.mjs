// Module `palette`: ranking, recents and the shortcut guard (ported from Console's tests, plus the
// Finance umlaut folding and the Workbench AltGr/IME guard). Started by TestFrontendModuleLogic.
import assert from 'node:assert/strict';
import { load } from './ts.mjs';

const { rank, score, normalize, fuzzy, TIER, readRecents, pushRecent } = await load('palette/palette.ts');
const { createShortcutHandler, isTypingTarget, isApplePlatform } = await load('palette/shortcuts.ts');

const pages = [
  { label: 'Dashboard', detail: 'Workspace', aliases: ['home'] },
  { label: 'Cluster', detail: 'Infrastructure', aliases: ['k8s', 'kubernetes', 'pods'] },
  { label: 'Resources', detail: 'Host', aliases: ['ram', 'cpu', 'memory'] },
  { label: 'Schedules', detail: 'Jobs', aliases: ['cron', 'timers'] },
  { label: 'Backups', detail: 'Cluster', aliases: ['backup', 'velero'] },
  { label: 'Certificates', detail: 'Cluster', aliases: ['cert', 'tls'] },
  { label: 'Cluster events', detail: 'Cluster' },
  { label: 'Host config', detail: 'Host', aliases: ['systemd'] },
  { label: 'Jobs', detail: 'Operate', aliases: ['argo', 'ci'] },
];
const labels = (query, items = pages) => rank(items, query).map((p) => p.label);

// Folding: case, accents, ß, and the Swiss ae/oe/ue spellings.
{
  assert.equal(normalize('  Übersicht '), 'ubersicht');
  assert.equal(normalize('Straße'), 'strasse');
  assert.equal(normalize('STRASSE'), 'strasse');
  assert.equal(normalize('Vertraege'), normalize('Verträge'));
  assert.equal(normalize('Müller'), normalize('Mueller'));
  assert.equal(normalize('Öffnungszeiten'), normalize('Oeffnungszeiten'));
  assert.equal(normalize('Crème brûlée'), 'creme brulee');
  assert.ok(score({ label: 'Übersicht' }, 'uber') >= TIER.prefix);
  assert.ok(score({ label: 'Übersicht' }, 'ueber') >= TIER.prefix);
  assert.ok(score({ label: 'Strassenverkehrsamt' }, 'straße') >= TIER.prefix);
  assert.ok(score({ label: 'Fußball' }, 'fussb') >= TIER.prefix);
  assert.deepEqual(labels('vertrage', [{ label: 'Verträge' }, { label: 'Vertraege alt' }]), ['Verträge', 'Vertraege alt']);
}

// Tiers: exact, prefix, word start, substring.
{
  assert.ok(score({ label: 'Jobs' }, 'jobs') > score({ label: 'Jobsite' }, 'jobs'));
  assert.ok(score({ label: 'Cluster events' }, 'clu') > score({ label: 'All clusters' }, 'clu'));
  assert.ok(score({ label: 'All clusters' }, 'clu') > score({ label: 'Subcluster' }, 'clu'));
  assert.ok(score({ label: 'Subcluster' }, 'clu') >= TIER.substring);
  assert.deepEqual(labels('cluster'), ['Cluster', 'Cluster events', 'Backups', 'Certificates']);
}

// Any word start, and every word of a multi-word query.
{
  assert.equal(labels('events')[0], 'Cluster events');
  assert.equal(labels('host con')[0], 'Host config');
  assert.deepEqual(labels('cl ev'), ['Cluster events']);
}

// Aliases, below a label match.
{
  for (const [query, want] of [['k8s', 'Cluster'], ['pods', 'Cluster'], ['ram', 'Resources'], ['cpu', 'Resources'], ['cron', 'Schedules'], ['backup', 'Backups'], ['tls', 'Certificates'], ['cert', 'Certificates']]) {
    assert.equal(labels(query)[0], want, query);
  }
  assert.deepEqual(labels('k8s'), ['Cluster']);
  assert.deepEqual(labels('kube', [{ label: 'Kubernetes', aliases: [] }, { label: 'Cluster', aliases: ['kubernetes'] }]), ['Kubernetes', 'Cluster']);
}

// Detail below the label; fuzzy last, tight runs first.
{
  const s = score({ label: 'Backups', detail: 'Cluster' }, 'cluster');
  assert.ok(s < TIER.substring && s > 0);
  assert.ok(fuzzy('crts', 'certificates') > 0);
  assert.equal(fuzzy('xyz', 'certificates'), -1);
  assert.ok(fuzzy('cer', 'certificates') > fuzzy('cer', 'cxxexxrtificates'));
  assert.deepEqual(labels('crtf'), ['Certificates']);
  const f = score({ label: 'Certificates' }, 'crtf');
  assert.ok(f > 0 && f < TIER.detail);
}

// No match for a stray letter; stable order for empty queries and ties; shorter label first.
{
  assert.equal(score({ label: 'Dashboard' }, 'z'), 0);
  assert.deepEqual(labels('qq'), []);
  assert.deepEqual(labels(''), pages.map((p) => p.label));
  assert.deepEqual(labels('one', [{ label: 'Gamma one' }, { label: 'Alpha one' }]), ['Gamma one', 'Alpha one']);
  assert.deepEqual(labels('job', [{ label: 'Jobs running now' }, { label: 'Jobs' }]), ['Jobs', 'Jobs running now']);
}

// Recents: newest first, no duplicates, bounded, per key, broken storage is no error.
{
  const memory = (initial = {}) => {
    const data = new Map(Object.entries(initial));
    return { getItem: (k) => data.get(k) ?? null, setItem: (k, v) => void data.set(k, v) };
  };
  const storage = memory();
  pushRecent('acme-recents', 'page:/train', { storage });
  pushRecent('acme-recents', 'app:omni', { storage });
  assert.deepEqual(pushRecent('acme-recents', 'page:/train', { storage }), ['page:/train', 'app:omni']);
  assert.deepEqual(readRecents('acme-recents', { storage }), ['page:/train', 'app:omni']);
  assert.deepEqual(readRecents('other', { storage }), []);
  for (let i = 0; i < 8; i++) pushRecent('many', `job:${i}`, { storage });
  assert.deepEqual(readRecents('many', { storage }), ['job:7', 'job:6', 'job:5', 'job:4', 'job:3']);
  pushRecent('three', 'a', { storage, max: 2 });
  pushRecent('three', 'b', { storage, max: 2 });
  assert.deepEqual(pushRecent('three', 'c', { storage, max: 2 }), ['c', 'b']);
  assert.deepEqual(readRecents('k', { storage: memory({ k: '{not json' }) }), []);
  assert.deepEqual(readRecents('k', { storage: memory({ k: '[1, "page:/", null]' }) }), ['page:/']);
  assert.deepEqual(readRecents('k', { storage: undefined }), []);
  assert.deepEqual(readRecents('k'), [], 'no localStorage in Node');
  assert.deepEqual(pushRecent('k', 'page:/', { storage: { getItem: () => null, setItem: () => { throw new Error('quota'); } } }), ['page:/']);
}

// Typing targets.
{
  const el = (tagName, extra = {}) => ({ tagName, closest: () => null, ...extra });
  assert.ok(isTypingTarget(el('INPUT')));
  assert.ok(isTypingTarget(el('INPUT', { type: 'search' })));
  assert.ok(isTypingTarget(el('input', { type: 'email' })));
  assert.ok(isTypingTarget(el('TEXTAREA')));
  assert.ok(isTypingTarget(el('SELECT')));
  assert.ok(isTypingTarget(el('DIV', { isContentEditable: true })));
  for (const type of ['checkbox', 'radio', 'button', 'submit', 'range', 'color', 'file']) assert.ok(!isTypingTarget(el('INPUT', { type })), type);
  assert.ok(!isTypingTarget(el('BUTTON')));
  assert.ok(!isTypingTarget(null));
  assert.ok(!isTypingTarget({}));
}

// Shortcuts: go sequences, single keys, guards.
{
  const setup = ({ dialogOpen = () => false, apple = false, available = () => true } = {}) => {
    let clock = 1_000;
    const calls = [];
    const record = (name, result) => () => { calls.push(name); return result; };
    const handle = createShortcutHandler({
      keys: { '/': record('search'), r: record('refresh'), '?': record('help'), x: record('x', false) },
      go: { i: () => (available('/inbox') ? (calls.push('go /inbox'), true) : false), d: record('go /') },
      primary: { k: record('palette') },
      apple,
      dialogOpen,
      now: () => clock,
    });
    const body = { tagName: 'BODY', closest: () => null };
    const press = (key, extra = {}) => handle({ key, target: body, ...extra });
    return { calls, press, handle, tick: (ms) => { clock += ms; } };
  };

  let t = setup();
  assert.equal(t.press('g'), true);
  assert.equal(t.press('i'), true);
  assert.equal(t.press('g'), true);
  assert.equal(t.press('D'), true, 'go letter is case-insensitive');
  assert.deepEqual(t.calls, ['go /inbox', 'go /']);

  t = setup();
  t.press('g');
  t.tick(1501);
  assert.equal(t.press('d'), false, 'g forgotten after the timeout');
  t.press('g'); t.press('d'); t.press('d');
  assert.deepEqual(t.calls, ['go /'], 'a g sequence only once');

  t = setup({ available: () => false });
  t.press('g');
  assert.equal(t.press('i'), false, 'a go action that does not apply');
  t = setup();
  t.press('g');
  assert.equal(t.press('r'), true, 'a key after g that is no target does its own job');
  assert.deepEqual(t.calls, ['refresh']);

  t = setup();
  assert.equal(t.press('/'), true);
  assert.equal(t.press('?', { shiftKey: true }), true, 'shift is how ? is typed');
  assert.equal(t.press('x'), false, 'an action returning false leaves the key');
  assert.equal(t.press('q'), false);
  assert.deepEqual(t.calls, ['search', 'help', 'x']);

  t = setup();
  const input = { tagName: 'INPUT', type: 'text', closest: () => null };
  const inDialog = { tagName: 'BUTTON', closest: (s) => (s.includes('dialog') ? {} : null) };
  for (const [why, key, extra] of [
    ['ctrl', 'r', { ctrlKey: true }],
    ['meta', 'r', { metaKey: true }],
    ['alt', 'r', { altKey: true }],
    ['AltGr as Ctrl+Alt (Swiss AltGr+2 = @)', '@', { ctrlKey: true, altKey: true }],
    ['AltGraph state', '@', { getModifierState: (k) => k === 'AltGraph' }],
    ['IME composition', 'r', { isComposing: true }],
    ['IME keyCode 229', 'r', { keyCode: 229 }],
    ['handled already', 'r', { defaultPrevented: true }],
    ['typed into a field', 'r', { target: input }],
    ['inside a dialog', 'r', { target: inDialog }],
  ]) {
    assert.equal(t.press(key, extra), false, why);
  }
  t.press('g');
  assert.equal(t.press('d', { target: input }), false, 'a field ends the go sequence');
  assert.equal(t.press('d'), false);
  t.press('g');
  t.press('d', { ctrlKey: true });
  assert.equal(t.press('d'), false, 'a modifier ends the go sequence');
  assert.deepEqual(t.calls, []);

  t = setup({ dialogOpen: () => true });
  assert.equal(t.press('r'), false, 'an open modal dialog has the keyboard');

  // Primary combinations: Ctrl elsewhere, ⌘ on Apple; in fields too; never with Alt or the other modifier.
  t = setup();
  assert.equal(t.press('k', { ctrlKey: true }), true);
  assert.equal(t.press('K', { ctrlKey: true, shiftKey: true }), true);
  assert.equal(t.press('k', { ctrlKey: true, target: input }), true);
  assert.equal(t.press('k', { metaKey: true }), false);
  assert.equal(t.press('k', { ctrlKey: true, altKey: true }), false, 'AltGr+k');
  assert.equal(t.press('k', { ctrlKey: true, target: inDialog }), false);
  assert.equal(t.press('j', { ctrlKey: true }), false);
  assert.deepEqual(t.calls, ['palette', 'palette', 'palette']);
  t = setup({ apple: true });
  assert.equal(t.press('k', { metaKey: true }), true);
  assert.equal(t.press('k', { ctrlKey: true }), false);
  assert.equal(t.press('k', { metaKey: true, ctrlKey: true }), false);

  // Without go targets, g is an ordinary key.
  const plain = createShortcutHandler({ keys: { g: () => true }, now: () => 5 });
  assert.equal(plain({ key: 'g', target: null }), true);

  assert.ok(isApplePlatform({ platform: 'MacIntel' }));
  assert.ok(isApplePlatform({ platform: 'iPhone' }));
  assert.ok(isApplePlatform({ userAgentData: { platform: 'macOS' } }));
  assert.ok(!isApplePlatform({ platform: 'Win32' }));
  assert.ok(!isApplePlatform(undefined));
}

console.log('palette ok');
