// React rendering of the toast store (app-foundation registry module `toast`). Registry-owned: do
// not edit in an application. Unstyled: the app gives the classes and, if it likes, the markup.
import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { getToasts, subscribeToasts, toastRegion, watchTopModal, type Toast } from './toast.js';

/** The toasts on screen, oldest first. */
export function useToasts(): readonly Toast[] {
  return useSyncExternalStore(subscribeToasts, getToasts, getToasts);
}

export interface ToastRegionProps {
  /** Classes for the region, e.g. its desktop placement (toast.css puts it above the nav). */
  className?: string;
  /** Classes for each toast's element, which carries role status/alert and data-tone. */
  toastClassName?: string;
  /** The region's accessible name, the app's copy (e.g. "Notifications"). */
  label?: string;
  /** A toast's content; default: its text, the detail and the action button. */
  children?: (toast: Toast) => ReactNode;
}

/**
 * Mount once, anywhere (outside <AppShell> is fine): it renders into the top layer above the nav,
 * and into the topmost open modal dialog while there is one, so its buttons stay pressable.
 */
export function ToastRegion({ className, toastClassName, label, children }: ToastRegionProps) {
  const toasts = useToasts();
  const [host, setHost] = useState<HTMLElement | null>(null);
  const region = useRef<HTMLDivElement>(null);

  useEffect(() => watchTopModal((dialog) => setHost(dialog ?? document.body)), []);
  useEffect(() => {
    const element = region.current;
    if (!element) return;
    return toastRegion(element, { followModals: false });
  }, [host]);

  if (!host) return null;
  return createPortal(
    <div ref={region} className={['toast-region', className].filter(Boolean).join(' ')} aria-label={label}>
      {toasts.map((toast) => (
        <div key={toast.id} role={toast.role} aria-atomic="true" data-tone={toast.tone} className={toastClassName}>
          {children ? (
            children(toast)
          ) : (
            <>
              <span>{toast.text}</span>
              {toast.detail && <small>{toast.detail}</small>}
              {toast.action && (
                <button type="button" onClick={toast.action.run}>
                  {toast.action.label}
                </button>
              )}
            </>
          )}
        </div>
      ))}
    </div>,
    host,
  );
}
