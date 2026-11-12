// Short messages after an action (app-foundation registry module `toast`). Registry-owned: do not
// edit in an application. A store outside React, so data code can speak without a component;
// React apps render it with ./ToastRegion.js, other apps subscribe and render it themselves inside
// an element wired with toastRegion(). How a toast looks, and every word in it, is the app's.

export type ToastTone = 'info' | 'success' | 'warning' | 'error';

export interface ToastAction {
  label: string;
  run: () => void;
}

export interface ToastOptions {
  /** A button in the toast; pressing it dismisses the toast first. */
  action?: ToastAction;
  /** Shorthand for an action labelled with `undoLabel` (configureToasts). */
  undo?: () => void;
  tone?: ToastTone;
  /** A second line. */
  detail?: string;
  /** How long it stays, in ms; Infinity until dismissed. Default from configureToasts. */
  ms?: number;
}

export interface Toast {
  id: number;
  text: string;
  detail?: string;
  tone: ToastTone;
  /** Its action, already wrapped to dismiss the toast before it runs. */
  action?: ToastAction;
  /** `alert` (announced at once) for errors, `status` (polite) for everything else. */
  role: 'status' | 'alert';
  dismiss: () => void;
}

export interface ToastConfig {
  /** Toasts shown at once; a new one beyond this drops the oldest (default 3). */
  max: number;
  /** Plain toast (default 4000 ms). */
  ms: number;
  /** With an action or undo: long enough to reach it (default 7000 ms). */
  actionMs: number;
  /** Errors stay a little longer (default 7000 ms). */
  errorMs: number;
  /** The undo button's label, the app's copy (default 'Undo'). */
  undoLabel: string;
}

const config: ToastConfig = { max: 3, ms: 4000, actionMs: 7000, errorMs: 7000, undoLabel: 'Undo' };

/** Sets the app's limits and undo label, once at startup. */
export function configureToasts(values: Partial<ToastConfig>): void {
  Object.assign(config, values);
}

interface Entry {
  toast: Toast;
  remaining: number;
  started: number;
  timer?: ReturnType<typeof setTimeout>;
}

let entries: Entry[] = [];
let list: readonly Toast[] = [];
let next = 1;
let paused = false;
const listeners = new Set<() => void>();

function emit() {
  list = entries.map((entry) => entry.toast);
  listeners.forEach((listener) => listener());
}

function start(entry: Entry) {
  if (paused || !Number.isFinite(entry.remaining)) return;
  entry.started = Date.now();
  entry.timer = setTimeout(() => dismissToast(entry.toast.id), Math.max(0, entry.remaining));
}

function stop(entry: Entry) {
  if (entry.timer === undefined) return;
  clearTimeout(entry.timer);
  entry.timer = undefined;
  entry.remaining -= Date.now() - entry.started;
}

/** Shows `text` and returns the function that takes it away early. */
export function toast(text: string, options: ToastOptions = {}): () => void {
  const id = next++;
  const tone = options.tone ?? 'info';
  const dismiss = () => dismissToast(id);
  const given = options.action ?? (options.undo ? { label: config.undoLabel, run: options.undo } : undefined);
  const action = given && {
    label: given.label,
    run: () => {
      dismiss();
      given.run();
    },
  };
  const ms = options.ms ?? (action ? config.actionMs : tone === 'error' ? config.errorMs : config.ms);
  const entry: Entry = {
    toast: { id, text, detail: options.detail, tone, action, role: tone === 'error' ? 'alert' : 'status', dismiss },
    remaining: ms,
    started: 0,
  };
  entries = [...entries, entry];
  while (entries.length > Math.max(1, config.max)) {
    const [oldest] = entries;
    stop(oldest);
    entries = entries.slice(1);
  }
  start(entry);
  emit();
  return dismiss;
}

export function dismissToast(id: number): void {
  const entry = entries.find((candidate) => candidate.toast.id === id);
  if (!entry) return;
  stop(entry);
  entries = entries.filter((candidate) => candidate !== entry);
  emit();
}

/** Every toast away, e.g. on sign-out. */
export function clearToasts(): void {
  entries.forEach(stop);
  entries = [];
  emit();
}

/** The toasts on screen, oldest first. The same array until something changes. */
export function getToasts(): readonly Toast[] {
  return list;
}

export function subscribeToasts(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Holds every timer (pointer over the toasts, focus in them, page hidden) or lets them run on. */
export function pauseToasts(pause: boolean): void {
  if (pause === paused) return;
  paused = pause;
  entries.forEach(pause ? stop : start);
}

/**
 * Calls `onChange` with the topmost open modal <dialog> (or null), now and whenever it changes.
 * A modal dialog makes everything outside it inert, a popover in the top layer included: toasts
 * with a button have to live inside the topmost modal while one is open.
 */
export function watchTopModal(onChange: (dialog: HTMLDialogElement | null) => void): () => void {
  let stack: HTMLDialogElement[] = [...document.querySelectorAll('dialog')].filter(isModal);
  let top: HTMLDialogElement | null = stack[stack.length - 1] ?? null;
  const observer = new MutationObserver((records) => {
    for (const record of records) {
      const target = record.target;
      if (record.type !== 'attributes' || !(target instanceof HTMLDialogElement)) continue;
      stack = stack.filter((dialog) => dialog !== target);
      if (isModal(target)) stack.push(target);
    }
    stack = stack.filter((dialog) => dialog.isConnected && isModal(dialog));
    const now = stack[stack.length - 1] ?? null;
    if (now !== top) {
      top = now;
      onChange(top);
    }
  });
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['open'], childList: true, subtree: true });
  onChange(top);
  return () => observer.disconnect();
}

function isModal(dialog: HTMLDialogElement): boolean {
  try {
    return dialog.matches(':modal');
  } catch {
    return dialog.open;
  }
}

/**
 * Wires the element that holds the toasts: a manual popover in the top layer (above the page
 * frame's stacking contexts), placed by toast.css above the nav. While a modal dialog is open the
 * element moves into the topmost one, or its buttons would be inert, and back to where it was when
 * the dialog closes (`followModals: false` when a renderer such as ToastRegion moves it itself).
 * The timers pause while a mouse is over it, focus is inside or the page is hidden. Returns the
 * function that unwires it.
 */
export function toastRegion(region: HTMLElement, { followModals = true }: { followModals?: boolean } = {}): () => void {
  const popover = typeof region.showPopover === 'function';
  const show = () => {
    if (!popover || !region.isConnected) return;
    if (region.matches(':popover-open')) region.hidePopover();
    region.showPopover();
  };
  if (popover) region.popover = 'manual';
  region.classList.add('toast-region');
  show();

  const home = region.parentNode;
  const next = region.nextSibling;
  const unwatch = followModals
    ? watchTopModal((dialog) => {
        if (dialog) dialog.append(region);
        else if (home && home.isConnected) home.insertBefore(region, next && next.parentNode === home ? next : null);
        show();
      })
    : () => {};

  let hovered = false;
  let focused = false;
  const update = () => pauseToasts(hovered || focused || document.visibilityState === 'hidden');
  const enter = (event: PointerEvent) => {
    if (event.pointerType !== 'mouse') return;
    hovered = true;
    update();
  };
  const leave = (event: PointerEvent) => {
    if (event.pointerType !== 'mouse') return;
    hovered = false;
    update();
  };
  const focusIn = () => {
    focused = true;
    update();
  };
  const focusOut = (event: FocusEvent) => {
    if (region.contains(event.relatedTarget as Node | null)) return;
    focused = false;
    update();
  };
  region.addEventListener('pointerenter', enter);
  region.addEventListener('pointerleave', leave);
  region.addEventListener('focusin', focusIn);
  region.addEventListener('focusout', focusOut);
  document.addEventListener('visibilitychange', update);
  // The last toast gone under the mouse: pointerleave may never come, so let a new one run.
  const unsubscribe = subscribeToasts(() => {
    if (!getToasts().length && hovered) {
      hovered = false;
      update();
    }
  });

  return () => {
    unsubscribe();
    unwatch();
    region.removeEventListener('pointerenter', enter);
    region.removeEventListener('pointerleave', leave);
    region.removeEventListener('focusin', focusIn);
    region.removeEventListener('focusout', focusOut);
    document.removeEventListener('visibilitychange', update);
    hovered = focused = false;
    update();
    if (popover && region.matches(':popover-open')) region.hidePopover();
  };
}

