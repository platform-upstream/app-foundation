// Shared PWA shell mechanics (app-foundation registry module `pwa-shell`). Registry-owned: do not
// edit in an application.
import { useEffect, useState } from 'react';

/**
 * Mirrors window.visualViewport into CSS custom properties so the shell can follow the soft
 * keyboard. iOS standalone PWAs ignore `interactive-widget=resizes-content` and Safari has no
 * VirtualKeyboard API, so the shell shrinks to the visible viewport instead.
 *
 * On <html>: --app-height, --vv-offset-top, --vv-bottom (px), and the `keyboard-open` class while
 * an editable element has focus (touch devices) or the viewport shrank by more than 60px (others).
 * Returns whether the keyboard is open.
 */
export function useVisualViewport(): boolean {
  const [keyboardOpen, setKeyboardOpen] = useState(false);
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const root = document.documentElement;
    const viewport = window.visualViewport;
    let frame = 0;
    const editableFocused = () => {
      const element = document.activeElement as HTMLElement | null;
      return (
        !!element &&
        (element.tagName === 'TEXTAREA' || element.tagName === 'INPUT' || element.isContentEditable)
      );
    };
    const apply = () => {
      frame = 0;
      const layoutHeight = window.innerHeight;
      const height = viewport?.height ?? layoutHeight;
      const top = viewport?.offsetTop ?? 0;
      root.style.setProperty('--app-height', `${height}px`);
      root.style.setProperty('--vv-offset-top', `${top}px`);
      root.style.setProperty('--vv-bottom', `${Math.max(0, layoutHeight - height - top)}px`);
      // On touch devices focus alone decides: iOS standalone may shrink innerHeight and the visual
      // viewport together, so the height difference can read ~0 while the keyboard is up.
      const coarse =
        typeof window.matchMedia === 'function' && window.matchMedia('(pointer: coarse)').matches;
      const open = editableFocused() && (coarse || layoutHeight - height > 60);
      root.classList.toggle('keyboard-open', open);
      setKeyboardOpen(open);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(apply);
    };
    apply();
    const windowEvents = ['resize', 'orientationchange', 'focusin', 'focusout', 'pageshow'];
    viewport?.addEventListener('resize', schedule);
    viewport?.addEventListener('scroll', schedule);
    for (const name of windowEvents) window.addEventListener(name, schedule);
    document.addEventListener('visibilitychange', schedule);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      viewport?.removeEventListener('resize', schedule);
      viewport?.removeEventListener('scroll', schedule);
      for (const name of windowEvents) window.removeEventListener(name, schedule);
      document.removeEventListener('visibilitychange', schedule);
    };
  }, []);
  return keyboardOpen;
}

/** Whether el or an ancestor below the shell can still scroll vertically in the drag's direction
 *  (dy < 0: the finger moves up, the content would scroll down). */
export function canScrollY(el: Element | null, dy: number): boolean {
  for (let node = el; node && node !== document.documentElement; node = node.parentElement) {
    if (!(node instanceof HTMLElement)) continue;
    const overflow = getComputedStyle(node).overflowY;
    if ((overflow === 'auto' || overflow === 'scroll') && node.scrollHeight > node.clientHeight + 1) {
      // A scroller at its end still takes the drag: its overscroll-behavior: contain stops it there.
      return true;
    }
    if (node.classList.contains('app-shell')) return false;
  }
  return false;
}

/**
 * Keeps iOS from rubber-banding the whole shell. The document never scrolls (the shell is one
 * screen high, <main> scrolls inside), yet iOS standalone moves the whole page with the nav off the
 * bottom edge when a vertical drag starts where nothing can scroll: on the nav, the top bar or a page
 * shorter than the screen (overscroll-behavior on the root does not stop it, iOS 26, 2026-11-21).
 * Such drags are cancelled; horizontal drags, pinch zoom and anything inside a scroller stay native.
 */
export function useNoShellRubberBand(): void {
  useEffect(() => {
    if (typeof window === 'undefined' || !('ontouchstart' in window)) return;
    let startX = 0;
    let startY = 0;
    let decided: 'native' | 'blocked' | null = null;
    const onStart = (event: TouchEvent) => {
      decided = event.touches.length === 1 ? null : 'native';
      startX = event.touches[0]?.clientX ?? 0;
      startY = event.touches[0]?.clientY ?? 0;
    };
    const onMove = (event: TouchEvent) => {
      if (decided === 'native' || event.touches.length !== 1) return;
      const touch = event.touches[0];
      if (!touch) return;
      if (decided === null) {
        const dx = touch.clientX - startX;
        const dy = touch.clientY - startY;
        if (Math.abs(dx) < 3 && Math.abs(dy) < 3) return;
        const target = event.target instanceof Element ? event.target : null;
        const inShell = !!target?.closest('.app-shell');
        decided = !inShell || Math.abs(dx) > Math.abs(dy) || canScrollY(target, dy) ? 'native' : 'blocked';
      }
      if (decided === 'blocked' && event.cancelable) event.preventDefault();
    };
    document.addEventListener('touchstart', onStart, { passive: true });
    document.addEventListener('touchmove', onMove, { passive: false });
    return () => {
      document.removeEventListener('touchstart', onStart);
      document.removeEventListener('touchmove', onMove);
    };
  }, []);
}
