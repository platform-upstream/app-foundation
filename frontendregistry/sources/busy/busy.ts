// Busy gate (app-foundation registry module `busy`). Registry-owned: do not edit in an
// application; `frontend-registry check` fails on any change. No React: Busy.tsx wraps it.
//
// Turns a busy flag (a fetch, a recompute, a save) into "show the cue" or not: nothing for an
// answer that comes within `delay`, so a fast poll never blinks; once shown, at least
// `minVisible`, so a cue never flashes for a frame. Both are the application's numbers.

export interface BusyTiming {
  /** Milliseconds busy must last before the cue shows. */
  delay: number;
  /** Milliseconds a shown cue stays at least, even when busy ends sooner. */
  minVisible: number;
}

/** The timer functions the gate uses; tests pass a fake one. */
export interface BusyClock {
  now(): number;
  setTimeout(run: () => void, ms: number): unknown;
  clearTimeout(id: unknown): void;
}

const systemClock: BusyClock = {
  now: () => Date.now(),
  setTimeout: (run, ms) => setTimeout(run, ms),
  clearTimeout: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
};

export interface BusyGate {
  /** Feeds the current busy flag; repeating the same value changes nothing. */
  set(busy: boolean): void;
  /** Whether the cue shows now. */
  readonly shown: boolean;
  /** Cancels a pending timer; `onChange` is not called again. */
  dispose(): void;
}

/**
 * A gate between a busy flag and its cue. `onChange(shown)` runs when the cue should appear or go:
 * - busy → shown after `delay` (at once for 0); busy ending before that hides nothing and shows
 *   nothing, the timer is dropped;
 * - not busy → hidden at once once `minVisible` has passed since it showed, else when it has;
 * - busy again while a shown cue waits out `minVisible` keeps it, without a new delay.
 */
export function busyGate(timing: BusyTiming, onChange: (shown: boolean) => void, clock: BusyClock = systemClock): BusyGate {
  const delay = Math.max(0, timing.delay);
  const minVisible = Math.max(0, timing.minVisible);
  let busy = false;
  let shown = false;
  let shownAt = 0;
  let timer: unknown;
  let disposed = false;

  const clear = () => {
    if (timer !== undefined) clock.clearTimeout(timer);
    timer = undefined;
  };
  const show = (next: boolean) => {
    if (disposed || shown === next) return;
    shown = next;
    if (next) shownAt = clock.now();
    onChange(next);
  };
  const later = (ms: number, next: boolean) => {
    timer = clock.setTimeout(() => {
      timer = undefined;
      show(next);
    }, ms);
  };

  return {
    set(next) {
      if (disposed || next === busy) return;
      busy = next;
      clear();
      if (busy) {
        if (shown) return;
        if (delay === 0) show(true);
        else later(delay, true);
        return;
      }
      if (!shown) return;
      const left = shownAt + minVisible - clock.now();
      if (left <= 0) show(false);
      else later(left, false);
    },
    get shown() {
      return shown;
    },
    dispose() {
      disposed = true;
      clear();
    },
  };
}
