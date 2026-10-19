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
