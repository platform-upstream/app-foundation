// Edge marks for sideways scrollers (app-foundation registry module `scroll-edges`).
// Registry-owned: do not edit in an application; `frontend-registry check` fails on any change.
// Import ./scroll-edges.css for the neutral `.edge-fade` mask; snapping, arrows and the strip's
// look stay in the application.
import { type RefObject, useEffect } from 'react';

/** Whether a sideways scroller has more to show before and after what is in view (2 px slack). */
export function scrollEdges(scrollLeft: number, clientWidth: number, scrollWidth: number): { start: boolean; end: boolean } {
  // Right-to-left scrollers report a negative scrollLeft.
  const left = Math.abs(scrollLeft);
  return { start: left > 2, end: left + clientWidth < scrollWidth - 2 };
}

/**
 * Marks a sideways scroller with `data-more-start` / `data-more-end` while there is more to scroll
 * that way, so CSS (`.edge-fade`) can fade that edge. Written straight to the element, so
 * scrolling never re-renders; kept fresh on scroll, on resize of the scroller or its items, and
 * when items are added or removed.
 */
export function useScrollEdges(ref: RefObject<HTMLElement | null>): void {
  useEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    const mark = () => {
      const { start, end } = scrollEdges(el.scrollLeft, el.clientWidth, el.scrollWidth);
      el.toggleAttribute('data-more-start', start);
      el.toggleAttribute('data-more-end', end);
    };
    mark();
    el.addEventListener('scroll', mark, { passive: true });
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(mark);
    const observeChildren = () => {
      if (!resize) return;
      resize.disconnect();
      resize.observe(el);
      for (const child of Array.from(el.children)) resize.observe(child);
    };
    observeChildren();
    // Items can arrive without this component rendering again (a child's own data).
    const mutations = typeof MutationObserver === 'undefined' ? null : new MutationObserver(() => { observeChildren(); mark(); });
    mutations?.observe(el, { childList: true });
    return () => {
      el.removeEventListener('scroll', mark);
      resize?.disconnect();
      mutations?.disconnect();
    };
    // No dependencies on purpose: after every render, so a scroller that mounts later (after its
    // data loaded) or is replaced is picked up.
  });
}
