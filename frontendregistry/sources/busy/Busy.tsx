// React layer of the busy cues (app-foundation registry module `busy`). Registry-owned: do not
// edit in an application. Unstyled: busy.css holds only the mechanics, the look comes from the
// app's custom properties and classes (docs/frontend-registry.md, `busy`).
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { busyGate, type BusyGate, type BusyTiming } from './busy.js';

/**
 * `busy` through a gate (busy.ts): true only after `delay` ms of busy, then for at least
 * `minVisible` ms. A plain boolean, so any source fits: `useIsFetching() > 0`, `query.isFetching`,
 * `mutation.isPending`.
 */
export function useDelayedBusy(busy: boolean, { delay, minVisible }: BusyTiming): boolean {
  const [on, setOn] = useState(false);
  const gate = useRef<BusyGate | null>(null);
  const latest = useRef(busy);
  latest.current = busy;
  useEffect(() => {
    const created = busyGate({ delay, minVisible }, setOn);
    gate.current = created;
    created.set(latest.current);
    return () => {
      created.dispose();
      gate.current = null;
      setOn(false);
    };
  }, [delay, minVisible]);
  useEffect(() => {
    gate.current?.set(busy);
  }, [busy]);
  return on;
}

export interface BusyBarProps extends BusyTiming {
  busy: boolean;
  /** Classes for the bar: its place (e.g. along the bottom of the top bar), height and `color`. */
  className?: string;
  /**
   * What the live region says while the bar shows, the app's copy (e.g. "Updating"). Leave it out
   * for background polls a screen reader need not hear; the region stays mounted either way.
   */
  status?: string;
}

/**
 * A thin indeterminate bar, always mounted and toggled by `data-busy`, so showing it never shifts
 * the layout. The bar itself is hidden from assistive technology; a visually hidden
 * `role="status"` region beside it carries `status`.
 */
export function BusyBar({ busy, delay, minVisible, className, status }: BusyBarProps) {
  const on = useDelayedBusy(busy, { delay, minVisible });
  return (
    <span className={className ? `busy-bar ${className}` : 'busy-bar'} data-busy={on ? '' : undefined}>
      <span className="busy-bar-track" aria-hidden="true"><i /></span>
      <span className="busy-status" role="status">{on && status ? status : ''}</span>
    </span>
  );
}

export interface PendingScopeProps extends BusyTiming {
  busy: boolean;
  className?: string;
  /** Content, or a function of whether the scope shows as pending (to render the app's own label). */
  children: ReactNode | ((on: boolean) => ReactNode);
}

/**
 * A part of the page whose results are being recomputed while the previous answer stays in view:
 * `data-pending` and `aria-busy` on the wrapper; descendants marked `data-result` dim and blur by
 * the app's `--busy-pending-opacity` and `--busy-pending-filter`. Everything else, the controls
 * that caused the recompute above all, stays sharp and usable.
 */
export function PendingScope({ busy, delay, minVisible, className, children }: PendingScopeProps) {
  const on = useDelayedBusy(busy, { delay, minVisible });
  return (
    <div className={className ? `busy-scope ${className}` : 'busy-scope'} data-pending={on ? '' : undefined} aria-busy={on || undefined}>
      {typeof children === 'function' ? children(on) : children}
    </div>
  );
}
