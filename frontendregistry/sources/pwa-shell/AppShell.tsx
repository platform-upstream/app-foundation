// Shared PWA shell mechanics (app-foundation registry module `pwa-shell`). Registry-owned: do not
// edit in an application. Import ./shell.css before the application's own CSS.
import { useEffect, useRef, type ComponentPropsWithRef, type ReactNode } from 'react';
import { useNoShellRubberBand, useVisualViewport } from './viewport.js';

export interface AppShellProps {
  /** Required. Carries the page title and pads env(safe-area-inset-top) itself. */
  topBar: ReactNode;
  /** The application's bottom nav. It paints its own background and safe-area padding, is never
   *  position:fixed, and hides itself where a sidebar takes over (e.g. `md:hidden`). */
  nav?: ReactNode;
  /** Optional desktop sidebar, rendered before the column. */
  sidebar?: ReactNode;
  /** Classes for the outer element, e.g. the application's desktop row layout. */
  className?: string;
  /** Props for the scrolling <main>: id, tabIndex, ref, extra classes. */
  mainProps?: ComponentPropsWithRef<'main'>;
  children: ReactNode;
}

/**
 * Shell > sidebar + column; column = top bar, scrolling <main>, nav as the last flex child. The
 * nav is left out while the soft keyboard is open. The nav slot's height is published as
 * --shell-nav-height on <html> (0px without a visible nav) for toasts and floating buttons.
 * Overlays, dialogs and skip links belong outside, as siblings of <AppShell>.
 */
export function AppShell({ topBar, nav, sidebar, className, mainProps, children }: AppShellProps) {
  const keyboardOpen = useVisualViewport();
  useNoShellRubberBand();
  const navSlot = useRef<HTMLDivElement>(null);
  const showNav = nav != null && !keyboardOpen;

  useEffect(() => {
    const root = document.documentElement;
    const slot = navSlot.current;
    const publish = () => root.style.setProperty('--shell-nav-height', `${slot?.offsetHeight ?? 0}px`);
    publish();
    if (!slot || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(publish);
    observer.observe(slot);
    return () => observer.disconnect();
  }, [showNav]);

  const { className: mainClassName, ...main } = mainProps ?? {};
  return (
    <div className={classes('app-shell', className)}>
      {sidebar}
      <div className="app-shell-column">
        {topBar}
        <main {...main} className={classes('app-shell-main', mainClassName)}>
          {children}
        </main>
        {showNav && (
          <div ref={navSlot} className="app-shell-nav">
            {nav}
          </div>
        )}
      </div>
    </div>
  );
}

function classes(...names: Array<string | undefined>): string {
  return names.filter(Boolean).join(' ');
}
