// React hooks for the bottom-sheet behaviour (app-foundation registry module `sheet`).
// Registry-owned: do not edit in an application.
import { useEffect, useRef, type RefObject } from 'react';
import { attachSheetDrag, showSheet, type SheetOptions } from './sheet.js';

/**
 * Shows the <dialog> in `ref` as a modal sheet while `open` is true (see showSheet): top layer,
 * scroll lock on `.app-shell-main`, Escape and backdrop only for the topmost sheet, focus back to
 * the opener. `onClose` is the person's request to close; set `open` to false there. On a desktop
 * the element with `data-autofocus` (or `options.initialFocus`) takes the focus: React's
 * `autoFocus` runs when the dialog mounts, before it is shown, and is lost.
 *
 *   const ref = useRef<HTMLDialogElement>(null);
 *   useSheet(ref, open, () => setOpen(false));
 *   useSheetDrag(ref, { onClose: () => setOpen(false), below: 700 });
 *   <dialog ref={ref} className="my-sheet" aria-labelledby="t">
 *     <header data-sheet-handle><span className="grabber" /><h2 id="t">Title</h2></header>
 *     <input name="title" data-autofocus />…
 *   </dialog>
 */
export function useSheet(
  ref: RefObject<HTMLDialogElement | null>,
  open: boolean,
  onClose: () => void,
  options: Omit<SheetOptions, 'onClose'> = {},
): void {
  const close = useRef(onClose);
  close.current = onClose;
  const { scroller, backdropClose } = options;
  // Read when the sheet opens: an inline element or selector need not be stable.
  const initialFocus = useRef(options.initialFocus);
  initialFocus.current = options.initialFocus;
  useEffect(() => {
    const dialog = ref.current;
    if (!open || !dialog) return;
    return showSheet(dialog, { scroller, backdropClose, initialFocus: initialFocus.current, onClose: () => close.current() });
  }, [open, ref, scroller, backdropClose]);
}

/**
 * Drag down from the sheet's `data-sheet-handle` elements (grabber, header) to close: past
 * `threshold` px (default 80) or with a flick. Touch and pen only; `below` limits it to viewports
 * narrower than that many px, where the sheet sits at the bottom.
 */
export function useSheetDrag(
  ref: RefObject<HTMLElement | null>,
  { onClose, threshold, below }: { onClose: () => void; threshold?: number; below?: number },
): void {
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const sheet = ref.current;
    if (!sheet) return;
    return attachSheetDrag(sheet, { onClose: () => close.current(), threshold, below });
  }, [ref, threshold, below]);
}
