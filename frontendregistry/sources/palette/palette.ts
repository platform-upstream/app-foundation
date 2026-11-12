// Command palette logic (app-foundation registry module `palette`): ranking and recent picks.
// Registry-owned: do not edit in an application; `frontend-registry check` fails on any change.
// The palette's look, its commands and its copy stay in the application.

export interface Rankable {
  label: string;
  /** The second line (a group, a section, a path). */
  detail?: string;
  /** Other words the item is known by ("k8s" for Cluster, "Rechnung" for Invoices). */
  aliases?: readonly string[];
}

/** Score tiers, best first; within a tier a shorter label wins. */
export const TIER = {
  exact: 1000,
  prefix: 900,
  wordStart: 800,
  allWordStarts: 750,
  alias: 700,
  substring: 600,
  allWords: 500,
  detail: 400,
  fuzzy: 100,
} as const;

/**
 * Lower case without accents, for comparing: "Übersicht" → "ubersicht", "Straße" → "strasse",
 * and the Swiss spellings ae/oe/ue fold like ä/ö/ü ("Vertraege" and "Verträge" → "vertrage").
 */
export function normalize(text: string): string {
  return text
    .toLowerCase()
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .replace(/ß/g, 'ss')
    .replace(/ae/g, 'a')
    .replace(/oe/g, 'o')
    .replace(/ue/g, 'u')
    .trim();
}

const words = (text: string) => text.split(/[^\p{L}\p{N}]+/u).filter(Boolean);

/** Whether `needle`'s letters appear in `hay` in order; the score rewards tight, early runs (1–99, -1 = no). */
export function fuzzy(needle: string, hay: string): number {
  if (!needle) return 0;
  let at = -1;
  let first = -1;
  for (const ch of needle) {
    at = hay.indexOf(ch, at + 1);
    if (at < 0) return -1;
    if (first < 0) first = at;
  }
  const spread = at - first + 1 - needle.length;
  return Math.max(1, 99 - spread * 4 - first);
}

/**
 * The item's score for the query; 0 = no match, an empty query matches everything at 1. Tiers:
 * the whole label, a label that starts with the query, a word of the label that starts with it,
 * every query word starting a label word ("cl ev" → "Cluster events"), an alias, the query inside
 * the label, every query word somewhere in label, detail or aliases, the query in the detail, and
 * last the letters in order with gaps ("crts" → Certificates).
 */
export function score(item: Rankable, query: string): number {
  const q = normalize(query);
  if (!q) return 1;
  const label = normalize(item.label);
  // Shorter labels win ties within a tier: "Jobs" before "Jobs running now".
  const short = Math.max(0, 40 - label.length) / 100;
  if (label === q) return TIER.exact + short;
  if (label.startsWith(q)) return TIER.prefix + short;
  const labelWords = words(label);
  const qWords = words(q);
  if (labelWords.some((w) => w.startsWith(q))) return TIER.wordStart + short;
  if (qWords.length > 1 && qWords.every((qw) => labelWords.some((w) => w.startsWith(qw)))) return TIER.allWordStarts + short;
  const aliases = (item.aliases ?? []).map(normalize);
  if (aliases.some((a) => a === q)) return TIER.alias + 10 + short;
  if (aliases.some((a) => a.startsWith(q))) return TIER.alias + short;
  if (label.includes(q)) return TIER.substring + short;
  const all = normalize([item.label, item.detail ?? '', ...(item.aliases ?? [])].join(' '));
  if (qWords.length > 1 && qWords.every((qw) => all.includes(qw))) return TIER.allWords + short;
  if (all.includes(q)) return TIER.detail + short;
  const f = fuzzy(q.replace(/\s+/g, ''), label);
  return f > 0 && q.length > 1 ? TIER.fuzzy + (f / 100) * 99 : 0;
}

/**
 * The matching items, best first; equal scores keep their given order. `tieBreak` orders items
 * that match equally well (the same tier, as `Math.floor(score)`) before the shorter-label rule:
 * `(a, b) => Number(b.active) - Number(a.active)` puts active items first among equals. When it
 * returns 0, the shorter label and then the given order decide.
 */
export function rank<T extends Rankable>(items: readonly T[], query: string, tieBreak?: (a: T, b: T) => number): T[] {
  return items
    .map((item, index) => ({ item, index, s: score(item, query) }))
    .filter((entry) => entry.s > 0)
    .sort((a, b) => Math.floor(b.s) - Math.floor(a.s) || (tieBreak ? tieBreak(a.item, b.item) : 0) || b.s - a.s || a.index - b.index)
    .map((entry) => entry.item);
}

type ReadStorage = Pick<Storage, 'getItem'>;
type WriteStorage = Pick<Storage, 'getItem' | 'setItem'>;

export interface RecentsOptions<S> {
  /** How many to keep (default 5). */
  max?: number;
  /** Default localStorage; private mode or a full quota is not an error. */
  storage?: S;
}

/** The last picks under `key` (an app-own localStorage key), newest first, as stable ids ("page:/train"). */
export function readRecents(key: string, { max = 5, storage = safeStorage() }: RecentsOptions<ReadStorage> = {}): string[] {
  try {
    const raw = storage?.getItem(key);
    const list: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(list) ? list.filter((id): id is string => typeof id === 'string').slice(0, max) : [];
  } catch {
    return [];
  }
}

/** Puts `id` first under `key`, drops its older copy and keeps `max`; returns the new list. */
export function pushRecent(key: string, id: string, { max = 5, storage = safeStorage() }: RecentsOptions<WriteStorage> = {}): string[] {
  const next = [id, ...readRecents(key, { max, storage }).filter((other) => other !== id)].slice(0, max);
  try {
    storage?.setItem(key, JSON.stringify(next));
  } catch {
    // private mode, quota
  }
  return next;
}

function safeStorage(): Storage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage;
  } catch {
    return undefined;
  }
}
