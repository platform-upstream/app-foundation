// Pull to refresh for a Home Screen web app (app-foundation registry module `pull-refresh`).
// Registry-owned: do not edit in an application. No React here; React apps use
// ./usePullToRefresh.js. The indicator is drawn by the app from the state this reports.
//
// A Home Screen app has no pull to refresh of its own (a browser tab has), so it is on only when
// the app runs standalone on a touch screen. Nothing moves the page and no event is cancelled: the
// listeners are passive and the scroller's own bounce stays as it is.

export interface PullOptions {
  /** Indicator travel in px (after damping) past which letting go refreshes (default 72). */
  trigger?: number;
  /** The indicator stops here however far the finger goes (default 1.6 × trigger). */
  max?: number;
  /** Finger travel in px before the gesture decides between a pull and a sideways swipe (default 10). */
  slop?: number;
  /** Indicator travel per px of finger travel (default 0.5). */
  damping?: number;
  /** The busy state lasts at least this long in ms, so the spin is seen (default 500). */
  minSpin?: number;
  /** Override the standalone + coarse pointer check (tests, or an app that knows better). */
  enabled?: boolean;
}

export interface PullState {
  /** Indicator travel in px, 0 at rest; `trigger` while busy. */
  pull: number;
  /** Letting go now refreshes. */
  ready: boolean;
  /** The refresh runs. */
  busy: boolean;
}

export const PULL_REST: PullState = { pull: 0, ready: false, busy: false };

interface Resolved {
  trigger: number;
  max: number;
  slop: number;
  damping: number;
  minSpin: number;
}

export function pullSettings(options: PullOptions = {}): Resolved {
  const trigger = options.trigger ?? 72;
  return {
    trigger,
    max: options.max ?? Math.round(trigger * 1.6),
    slop: options.slop ?? 10,
    damping: options.damping ?? 0.5,
    minSpin: options.minSpin ?? 500,
  };
}

/** The damped indicator travel for a finger that moved `dy` px down (0 for upward moves). */
export function pullDistance(dy: number, options: PullOptions = {}): number {
  const { max, damping } = pullSettings(options);
  if (!(dy > 0)) return 0;
  return Math.min(max, dy * damping);
}

/**
 * What the first real movement of a touch says: a pull (down, more vertical than sideways), not a
 * pull (up, or sideways such as a carousel or tabs), or not yet known (still inside the slop).
 */
export function pullIntent(dx: number, dy: number, options: PullOptions = {}): 'pull' | 'cancel' | 'undecided' {
  const { slop } = pullSettings(options);
  if (Math.abs(dx) < slop && Math.abs(dy) < slop) return 'undecided';
  if (dy > 0 && dy > Math.abs(dx)) return 'pull';
  return 'cancel';
}

/** Whether pull to refresh is on: a touch screen running as a Home Screen app. */
export function pullEnabled(win: Pick<Window, 'matchMedia' | 'navigator'>): boolean {
  const mq = (query: string) => !!win.matchMedia?.(query)?.matches;
  // iOS reports a Home Screen app through navigator.standalone as well.
  const standalone = mq('(display-mode: standalone)') || mq('(display-mode: fullscreen)') || (win.navigator as { standalone?: boolean }).standalone === true;
  return standalone && mq('(pointer: coarse)');
}

/**
 * A modal dialog or sheet is open: the pull belongs to the page, which is behind it. Counts a
 * native modal `<dialog>`, anything with `aria-modal="true"`, and an open Radix dialog
 * (`[role=dialog][data-state=open]`, also when its content carries no aria-modal).
 */
export function dialogOpen(doc: Pick<Document, 'querySelector'>): boolean {
  let modal: Element | null = null;
  try {
    modal = doc.querySelector('dialog:modal');
  } catch {
    modal = doc.querySelector('dialog[open]');
  }
  return !!modal || !!doc.querySelector('[aria-modal="true"], [role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"]');
}

/**
 * Watches `scroller` (the shell's `.app-shell-main`) for a one-finger downward pull that starts at
 * its very top, reports every change to `onState`, and runs `refresh` when the finger lets go
 * past the trigger. Off unless standalone on a touch screen; ignores a touch while a dialog is
 * open, one with more than one finger, an upward or sideways first movement, and new pulls while
 * busy. Returns the function that detaches it.
 */
export function attachPullToRefresh(
  scroller: HTMLElement,
  refresh: () => unknown,
  onState: (state: PullState) => void,
  options: PullOptions = {},
): () => void {
  if (!(options.enabled ?? pullEnabled(window))) return () => {};
  const settings = pullSettings(options);
  let x = 0;
  let y = -1;
  let intent: ReturnType<typeof pullIntent> = 'undecided';
  let pull = 0;
  let busy = false;
  let alive = true;

  const report = () => {
    if (alive) onState({ pull, ready: busy || pull >= settings.trigger, busy });
  };
  const stop = () => {
    y = -1;
    if (pull && !busy) {
      pull = 0;
      report();
    }
  };
  const start = (event: TouchEvent) => {
    const ok = scroller.scrollTop <= 0 && event.touches.length === 1 && !busy && !dialogOpen(document);
    y = ok ? event.touches[0].clientY : -1;
    x = ok ? event.touches[0].clientX : 0;
    intent = 'undecided';
  };
  const move = (event: TouchEvent) => {
    if (y < 0) return;
    if (event.touches.length !== 1) return stop();
    const dx = event.touches[0].clientX - x;
    const dy = event.touches[0].clientY - y;
    if (intent === 'undecided') intent = pullIntent(dx, dy, settings);
    if (intent === 'cancel') return stop();
    if (intent !== 'pull') return;
    // The scroller moved instead (content above the top): no pull.
    const next = scroller.scrollTop > 0 ? 0 : pullDistance(dy, settings);
    if (next !== pull) {
      pull = next;
      report();
    }
  };
  const end = () => {
    if (y < 0) return;
    y = -1;
    if (pull < settings.trigger) return stop();
    busy = true;
    pull = settings.trigger;
    report();
    const seen = new Promise((resolve) => setTimeout(resolve, settings.minSpin));
    const running = Promise.resolve().then(refresh);
    void Promise.allSettled([running, seen]).then(() => {
      busy = false;
      pull = 0;
      report();
    });
  };

  const passive = { passive: true } as const;
  scroller.addEventListener('touchstart', start, passive);
  scroller.addEventListener('touchmove', move, passive);
  scroller.addEventListener('touchend', end, passive);
  scroller.addEventListener('touchcancel', stop, passive);
  return () => {
    alive = false;
    scroller.removeEventListener('touchstart', start);
    scroller.removeEventListener('touchmove', move);
    scroller.removeEventListener('touchend', end);
    scroller.removeEventListener('touchcancel', stop);
  };
}
