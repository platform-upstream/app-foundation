// React hook for pull to refresh (app-foundation registry module `pull-refresh`). Registry-owned:
// do not edit in an application.
import { useEffect, useRef, useState, type RefObject } from 'react';
import { attachPullToRefresh, PULL_REST, type PullOptions, type PullState } from './pull.js';

/**
 * Pull to refresh on `scroller` (the shell's `.app-shell-main`, e.g. via AppShell's
 * `mainProps.ref`). Returns `{ pull, ready, busy }` for the app's own indicator; `refresh` runs
 * when the finger lets go past the trigger, `busy` lasts until it settles and at least `minSpin`.
 * Off unless the app runs standalone on a touch screen (see attachPullToRefresh).
 */
export function usePullToRefresh(
  scroller: RefObject<HTMLElement | null>,
  refresh: () => unknown,
  options: PullOptions = {},
): PullState {
  const [state, setState] = useState<PullState>(PULL_REST);
  const run = useRef(refresh);
  run.current = refresh;
  const { trigger, max, slop, damping, minSpin, enabled } = options;
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    const detach = attachPullToRefresh(element, () => run.current(), setState, { trigger, max, slop, damping, minSpin, enabled });
    return () => {
      detach();
      setState(PULL_REST);
    };
  }, [scroller, trigger, max, slop, damping, minSpin, enabled]);
  return state;
}
