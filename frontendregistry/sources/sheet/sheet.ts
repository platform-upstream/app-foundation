// Bottom-sheet behaviour on a native <dialog> (app-foundation registry module `sheet`).
// Registry-owned: do not edit in an application. No React here, so an app without React uses it
// directly; React apps use ./useSheet.js. Look (background, radius, shadow, animation in and out)
// stays in the application; sheet.css only places the sheet and carries the drag.
//
// showModal() puts the dialog in the top layer: it renders above everything wherever it sits in
// the DOM (no portal; a transform or view-transition-name on the page frame cannot trap it), makes
// the rest of the page inert, keeps Tab inside, and sends Escape only to the topmost modal.

const SCROLLER = '.app-shell-main';
const NO_DRAG = 'button, a[href], input, select, textarea, label, [contenteditable]:not([contenteditable=false]), [data-sheet-no-drag]';

interface Lock {
  count: number;
  overflow: string;
  gutter: string;
}
const locks = new Map<HTMLElement, Lock>();

/**
 * Stops the page scroller from scrolling until every caller has released it (ref-counted, so a
 * sheet over a sheet shares one lock). The default scroller is the shell's `.app-shell-main`:
 * with `pwa-shell` the document never scrolls, so locking `body` leaves the page behind the
 * sheet scrolling. Apps without the shell get the document scroller. Keeps the scroll position;
 * a classic scrollbar keeps its gutter, so nothing shifts sideways.
 */
export function lockScroll(scroller?: HTMLElement | null): () => void {
  const el = scroller ?? document.querySelector<HTMLElement>(SCROLLER) ?? (document.scrollingElement as HTMLElement | null);
  if (!el) return () => {};
  let lock = locks.get(el);
  if (!lock) {
    lock = { count: 0, overflow: el.style.overflow, gutter: el.style.scrollbarGutter };
    locks.set(el, lock);
    const classicScrollbar = el.offsetWidth - el.clientWidth > 0 && el.scrollHeight > el.clientHeight;
    if (classicScrollbar) el.style.scrollbarGutter = 'stable';
    el.style.overflow = 'hidden';
    el.setAttribute('data-scroll-locked', '');
  }
  lock.count++;
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const current = locks.get(el);
    if (!current || --current.count > 0) return;
    locks.delete(el);
    el.style.overflow = current.overflow;
    el.style.scrollbarGutter = current.gutter;
    el.removeAttribute('data-scroll-locked');
  };
}

export interface SheetOptions {
  /** The person asked to close: Escape, a tap on the backdrop, or a closing form (method=dialog).
   *  React apps set their open state to false here. Default: close the dialog. */
  onClose?: () => void;
  /** The scroller to lock while the sheet is open (default `.app-shell-main`). */
  scroller?: HTMLElement | null;
  /** Close on a tap on the backdrop (default true). */
  backdropClose?: boolean;
}

const stack: HTMLDialogElement[] = [];
const shown = new WeakMap<HTMLDialogElement, () => void>();

/** The sheets opened with showSheet, oldest first; the last one is on top. */
export function openSheets(): readonly HTMLDialogElement[] {
  return stack;
}

/**
 * Opens `dialog` as a modal sheet and returns the function that closes it. While open: the
 * scroller is locked, Escape and a backdrop tap call `onClose` (only for the topmost sheet), and
 * on a touch screen the dialog itself takes the focus unless an element inside has `autofocus`
 * (a focused field would bring up the keyboard and hide the nav). On close the focus returns to
 * the element that had it before, if it is still in the page.
 */
export function showSheet(dialog: HTMLDialogElement, options: SheetOptions = {}): () => void {
  const existing = shown.get(dialog);
  if (existing) return existing;

  const active = document.activeElement;
  const opener = active instanceof HTMLElement && active !== document.body ? active : null;
  const requestClose = () => (options.onClose ? options.onClose() : hide());
  let hiding = false;

  const onCancel = (event: Event) => {
    // Escape: the browser sends it to the topmost modal only. The app decides; never close here.
    event.preventDefault();
    if (stack[stack.length - 1] === dialog) requestClose();
  };
  const onClick = (event: MouseEvent) => {
    if (options.backdropClose === false || event.target !== dialog) return;
    // A click on the dialog element itself outside its box is a click on ::backdrop.
    const box = dialog.getBoundingClientRect();
    const inside = event.clientX >= box.left && event.clientX <= box.right && event.clientY >= box.top && event.clientY <= box.bottom;
    if (!inside) requestClose();
  };
  const onClosed = () => {
    cleanup();
    // Closed from inside (a form with method=dialog): tell the app, whose state still says open.
    if (!hiding && options.onClose) options.onClose();
  };
  dialog.addEventListener('cancel', onCancel);
  dialog.addEventListener('click', onClick);
  dialog.addEventListener('close', onClosed);

  const unlock = lockScroll(options.scroller);
  stack.push(dialog);
  dialog.setAttribute('data-sheet', '');

  // showModal focuses the first focusable element. On a touch screen that may be a field (the
  // keyboard comes up, pwa-shell hides the nav) or a button with a focus ring: the dialog itself
  // takes the focus there, in the same task, before anything is painted.
  const touch = matchMedia('(pointer: coarse)').matches;
  const ownFocus = touch && !dialog.querySelector('[autofocus]');
  const hadTabIndex = dialog.hasAttribute('tabindex');
  if (ownFocus && !hadTabIndex) dialog.tabIndex = -1;
  if (!dialog.open) dialog.showModal();
  if (ownFocus) dialog.focus({ preventScroll: true });

  let done = false;
  function cleanup() {
    if (done) return;
    done = true;
    shown.delete(dialog);
    dialog.removeEventListener('cancel', onCancel);
    dialog.removeEventListener('click', onClick);
    dialog.removeEventListener('close', onClosed);
    const at = stack.indexOf(dialog);
    if (at >= 0) stack.splice(at, 1);
    unlock();
    if (ownFocus && !hadTabIndex) dialog.removeAttribute('tabindex');
    if (opener && opener.isConnected) opener.focus({ preventScroll: true });
  }
  function hide() {
    if (done) return;
    hiding = true;
    if (dialog.open) dialog.close();
    cleanup();
  }
  shown.set(dialog, hide);
  return hide;
}

export interface SheetDragOptions {
  /** Called when a drag ends far or fast enough; the sheet has already slid out. */
  onClose: () => void;
  /** Distance in px past which letting go closes (default 80). */
  threshold?: number;
  /** Only below this viewport width in px (where the sheet is a bottom sheet); default always. */
  below?: number;
}

/** What letting go does: close past the threshold or on a downward flick (px/ms), else settle back. */
export function sheetDragRelease(dy: number, velocity: number, threshold = 80): 'close' | 'settle' {
  if (dy > threshold) return 'close';
  if (dy > 20 && velocity > 0.5) return 'close';
  return 'settle';
}

/** Speed in px/ms of the last `window` ms of samples (finger positions over time). */
export function dragVelocity(samples: ReadonlyArray<{ t: number; y: number }>, window = 100): number {
  if (samples.length < 2) return 0;
  const last = samples[samples.length - 1];
  let first = samples[0];
  for (const sample of samples) {
    if (last.t - sample.t <= window) {
      first = sample;
      break;
    }
  }
  const dt = last.t - first.t;
  return dt > 0 ? (last.y - first.y) / dt : 0;
}

function reducedMotion(): boolean {
  return matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/**
 * Drag a sheet down to close it, from its handles: elements inside marked `data-sheet-handle`
 * (the grabber and the header). Touch and pen only, never the mouse; a press on a button, link or
 * field inside a handle stays a press. The offset goes to `--sheet-drag` (sheet.css turns it into
 * `translate`, so the app's own transform and animations are untouched) and `data-dragging` is
 * set while the finger is down. Returns the function that detaches the listeners.
 */
export function attachSheetDrag(sheet: HTMLElement, options: SheetDragOptions): () => void {
  let drag: { id: number; y0: number; samples: Array<{ t: number; y: number }> } | null = null;

  const reset = () => {
    sheet.style.removeProperty('--sheet-drag');
    sheet.removeAttribute('data-dragging');
  };
  const down = (event: PointerEvent) => {
    if (event.pointerType === 'mouse' || drag) return;
    if (options.below !== undefined && innerWidth >= options.below) return;
    const target = event.target as Element | null;
    const handle = target?.closest('[data-sheet-handle]');
    if (!handle || !sheet.contains(handle)) return;
    if (target?.closest(NO_DRAG)) return;
    reset();
    drag = { id: event.pointerId, y0: event.clientY, samples: [{ t: event.timeStamp, y: event.clientY }] };
    (handle as HTMLElement).setPointerCapture?.(event.pointerId);
  };
  const move = (event: PointerEvent) => {
    if (!drag || event.pointerId !== drag.id) return;
    drag.samples.push({ t: event.timeStamp, y: event.clientY });
    if (drag.samples.length > 20) drag.samples.shift();
    const dy = Math.max(0, event.clientY - drag.y0);
    sheet.setAttribute('data-dragging', '');
    sheet.style.setProperty('--sheet-drag', `${dy}px`);
  };
  const up = (event: PointerEvent) => {
    if (!drag || event.pointerId !== drag.id) return;
    const dy = Math.max(0, event.clientY - drag.y0);
    const velocity = dragVelocity(drag.samples);
    drag = null;
    sheet.removeAttribute('data-dragging');
    if (dy === 0) return reset();
    if (sheetDragRelease(dy, velocity, options.threshold) === 'settle') return sheet.style.removeProperty('--sheet-drag');
    if (reducedMotion()) return options.onClose();
    // Slide the rest of the way with the app's transition on `translate` (sheet.css), then close.
    sheet.style.setProperty('--sheet-drag', '100%');
    void getComputedStyle(sheet).translate; // start the transition from the current offset
    const sliding = sheet.getAnimations().map((animation) => animation.finished);
    void Promise.allSettled(sliding).then(() => options.onClose());
  };
  const cancel = (event: PointerEvent) => {
    if (!drag || event.pointerId !== drag.id) return;
    drag = null;
    sheet.removeAttribute('data-dragging');
    sheet.style.removeProperty('--sheet-drag');
  };

  sheet.addEventListener('pointerdown', down);
  sheet.addEventListener('pointermove', move);
  sheet.addEventListener('pointerup', up);
  sheet.addEventListener('pointercancel', cancel);
  // The next opening starts in place.
  sheet.addEventListener('close', reset);
  return () => {
    sheet.removeEventListener('pointerdown', down);
    sheet.removeEventListener('pointermove', move);
    sheet.removeEventListener('pointerup', up);
    sheet.removeEventListener('pointercancel', cancel);
    sheet.removeEventListener('close', reset);
    reset();
  };
}
