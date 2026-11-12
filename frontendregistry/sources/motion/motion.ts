/**
 * Motion mechanics (app-foundation registry module `motion`). Registry-owned: do not edit in an
 * application; `frontend-registry check` fails on any change.
 *
 * This file decides and triggers; it never says how something looks. Keyframes, durations, easings
 * and distances are the application's CSS (under `prefers-reduced-motion: no-preference`, see
 * motion.css). Nothing here runs on a refetch: an entrance plays on a list's first insert, an
 * indicator slides when the selected item changes. With "reduce motion" on, nothing moves.
 */
import { useEffect, useLayoutEffect, useState, useSyncExternalStore, type CSSProperties, type RefObject } from 'react';
import { flushSync } from 'react-dom';

const REDUCE = '(prefers-reduced-motion: reduce)';

/** True when the person asked the system for less motion. Scripted motion then jumps to its end. */
export function prefersReducedMotion(): boolean {
  return typeof globalThis.matchMedia === 'function' && globalThis.matchMedia(REDUCE).matches;
}

function subscribeReducedMotion(change: () => void): () => void {
  if (typeof globalThis.matchMedia !== 'function') return () => {};
  const query = globalThis.matchMedia(REDUCE);
  query.addEventListener('change', change);
  return () => query.removeEventListener('change', change);
}

/** prefersReducedMotion() as React state: follows the system setting while the page is open. */
export function useReducedMotion(): boolean {
  return useSyncExternalStore(subscribeReducedMotion, prefersReducedMotion, () => false);
}

/**
 * Resolves when the finite animations and transitions running on `element` or below it have
 * finished, or after `maxMs`, whichever comes first. Infinite ones (a spinner) are ignored.
 */
export function animationsSettled(element: Element, maxMs = 1500): Promise<void> {
  const running = typeof element.getAnimations === 'function'
    ? element.getAnimations({ subtree: true }).filter((animation) => animation.effect?.getTiming().iterations !== Infinity)
    : [];
  if (running.length === 0) return Promise.resolve();
  return new Promise<void>((resolve) => {
    const timer = setTimeout(resolve, maxMs);
    void Promise.allSettled(running.map((animation) => animation.finished)).then(() => {
      clearTimeout(timer);
      resolve();
    });
  });
}

/**
 * True while a list's entrance should play: from the render in which `ready` (the data is there)
 * first becomes true until the animations under `ref` have finished. Never again for the life of
 * the component: a refetch, a filter that empties and refills the list, or a new item mounts
 * without it. Under "reduce motion" it turns false at once.
 *
 *   const entering = useEntrance(listRef, items.length > 0);
 *   <ul ref={listRef} data-entering={entering || undefined}>
 *
 * The application's CSS animates `[data-entering] > li` under `no-preference`, with fill-mode
 * `backwards` (never `both`: a filled animation keeps each row a stacking context, and a row's menu
 * would open under the next row). Removing the attribute after the end changes nothing visible.
 */
export function useEntrance(ref: RefObject<Element | null>, ready: boolean, maxMs = 1500): boolean {
  const [done, setDone] = useState(false);
  const entering = ready && !done;
  useEffect(() => {
    if (!entering) return;
    let live = true;
    const finish = () => { if (live) setDone(true); };
    const element = ref.current;
    if (!element || prefersReducedMotion()) {
      finish();
      return () => { live = false; };
    }
    // One frame first, so the animations the attribute starts exist when they are collected.
    const frame = requestAnimationFrame(() => { void animationsSettled(element, maxMs).then(finish); });
    return () => { live = false; cancelAnimationFrame(frame); };
  }, [entering, ref, maxMs]);
  return entering;
}

type TransitionDocument = Document & { startViewTransition?: (update: () => void) => { finished: Promise<void> } };

/**
 * Runs `update` (a synchronous state change that renders the next view) inside a same-document
 * view transition when the browser has them, motion is welcome and the page is visible; otherwise
 * it just runs it. The update is flushed synchronously, so React has rendered when the browser
 * takes the new snapshot. Resolves when the transition (or the plain update) is over.
 *
 * Never put `view-transition-name` on the scrolling page frame (`.app-shell-main`, the page
 * column): the named element becomes a stacking context, and fixed overlays inside it land under
 * the top bar and the nav. Name content inside the frame, or nothing (the root cross-fades). One
 * name may sit on one element at a time.
 */
export function viewTransition(update: () => void): Promise<void> {
  const doc = document as TransitionDocument;
  if (typeof doc.startViewTransition !== 'function' || prefersReducedMotion() || doc.visibilityState !== 'visible') {
    update();
    return Promise.resolve();
  }
  try {
    const transition = doc.startViewTransition(() => flushSync(update));
    // A skipped or aborted transition rejects; the update has run either way.
    return transition.finished.then(() => undefined, () => undefined);
  } catch {
    update();
    return Promise.resolve();
  }
}

/** Where the selected item sits inside its container (offset coordinates). */
export interface IndicatorBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** `x`: slides along a row (a tab underline); `y`: along a column (a sidebar); `box`: both. */
export type IndicatorAxis = 'x' | 'y' | 'box';

/** The indicator's inline style for a box; hidden while no item is selected. */
export function indicatorStyle(box: IndicatorBox | null, axis: IndicatorAxis = 'x'): CSSProperties {
  if (!box) return { visibility: 'hidden' };
  if (axis === 'x') return { transform: `translateX(${box.x}px)`, width: box.w };
  if (axis === 'y') return { transform: `translateY(${box.y}px)`, height: box.h };
  return { transform: `translate(${box.x}px, ${box.y}px)`, width: box.w, height: box.h };
}

/** Equal boxes keep the old object, so React skips the re-render. */
export function sameBox(a: IndicatorBox | null, b: IndicatorBox | null): boolean {
  return a === b || (!!a && !!b && a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h);
}

/**
 * A selection indicator that slides from the old item to the new one. Measures the item matching
 * `selector` inside `containerRef` (the container is its offsetParent: `position: relative`) when
 * `key` changes and whenever the container or the item resizes (fonts, a rotated phone, another
 * language). Render the indicator as an absolutely positioned child at the container's top left:
 *
 *   const { style, instant } = useSlidingIndicator(navRef, '[aria-current="page"]', pathname);
 *   <span className="tab-indicator" style={style} data-instant={instant || undefined} />
 *
 * `instant` is true for the first placement (and the first after no item was selected), so the
 * indicator does not fly in from the corner; the application's transition is written as
 * `.tab-indicator:not([data-instant])` under `no-preference`.
 */
export function useSlidingIndicator(containerRef: RefObject<HTMLElement | null>, selector: string, key: unknown, axis: IndicatorAxis = 'x'): { style: CSSProperties; instant: boolean } {
  const [box, setBox] = useState<IndicatorBox | null>(null);
  const [placed, setPlaced] = useState(false);
  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const find = () => container.querySelector<HTMLElement>(selector);
    const measure = () => {
      const item = find();
      const next = item ? { x: item.offsetLeft, y: item.offsetTop, w: item.offsetWidth, h: item.offsetHeight } : null;
      setBox((old) => (sameBox(old, next) ? old : next));
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    let live = true;
    let frame = 0;
    // Resizes are read once per frame, not once per observed element.
    const later = () => { if (live && !frame) frame = requestAnimationFrame(() => { frame = 0; measure(); }); };
    const observer = new ResizeObserver(later);
    observer.observe(container);
    const item = find();
    if (item) observer.observe(item);
    void document.fonts?.ready.then(later);
    return () => { live = false; observer.disconnect(); cancelAnimationFrame(frame); };
  }, [containerRef, selector, key]);
  useEffect(() => {
    if (!box) { setPlaced(false); return; }
    if (placed) return;
    // The first placement paints without a transition; later moves slide.
    const frame = requestAnimationFrame(() => setPlaced(true));
    return () => cancelAnimationFrame(frame);
  }, [box, placed]);
  return { style: indicatorStyle(box, axis), instant: !placed };
}
