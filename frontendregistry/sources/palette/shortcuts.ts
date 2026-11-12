// Keyboard shortcut guard (app-foundation registry module `palette`). Registry-owned: do not edit
// in an application; `frontend-registry check` fails on any change. Which keys do what, and the
// list that shows them, stay in the application.

/** The part of a KeyboardEvent the handler reads (a plain object in tests). */
export interface ShortcutKey {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
  target?: EventTarget | null;
  defaultPrevented?: boolean;
  isComposing?: boolean;
  /** 229 while an IME composes (Safari sends it without isComposing). */
  keyCode?: number;
  getModifierState?: (key: string) => boolean;
}

/** An action; returning false means it did not apply, so the key is left to the browser. */
export type ShortcutAction = () => boolean | void;

export interface ShortcutOptions {
  /** Single keys, matched by the character typed (`event.key`): '/', 'r', '?'. */
  keys?: Record<string, ShortcutAction>;
  /** Letters after the `goKey` ("g then i"), case-insensitive. */
  go?: Record<string, ShortcutAction>;
  /** Combinations with the platform's primary modifier (⌘ on Apple, Ctrl elsewhere), by lower-case
   *  key: { k: openPalette }. They also work in a text field, never with Alt (AltGr) held. */
  primary?: Record<string, ShortcutAction>;
  /** The key that starts a go sequence (default 'g'). */
  goKey?: string;
  /** How long a `goKey` waits for its letter, in ms (default 1500). */
  timeout?: number;
  /** ⌘ instead of Ctrl as the primary modifier; default from the platform. */
  apple?: boolean;
  /** Whether a dialog has the keyboard; default: a modal dialog or [aria-modal] is open. */
  dialogOpen?: () => boolean;
  now?: () => number;
}

const NOT_TYPING = ['button', 'checkbox', 'radio', 'submit', 'reset', 'range', 'color', 'file', 'image'];

/** Whether keys typed here belong to the element: a text field, a textarea, a select, an editor. */
export function isTypingTarget(target: EventTarget | null | undefined): boolean {
  if (!target || typeof (target as HTMLElement).tagName !== 'string') return false;
  const el = target as HTMLElement;
  if (el.isContentEditable) return true;
  const tag = el.tagName.toUpperCase();
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag !== 'INPUT') return false;
  return !NOT_TYPING.includes(((el as HTMLInputElement).type || 'text').toLowerCase());
}

/** Whether the key comes from inside a dialog (the palette itself, a confirm, a sheet). */
function inDialog(target: EventTarget | null | undefined): boolean {
  const el = target as HTMLElement | null;
  return Boolean(el && typeof el.closest === 'function' && el.closest('dialog,[role="dialog"],[role="alertdialog"]'));
}

function defaultDialogOpen(): boolean {
  if (typeof document === 'undefined') return false;
  if (document.querySelector('[aria-modal="true"]')) return true;
  try {
    return document.querySelector('dialog:modal') !== null;
  } catch {
    return false; // :modal unknown (Safari before 15.6)
  }
}

/** ⌘ on macOS, iOS and iPadOS, Ctrl elsewhere. */
export function isApplePlatform(nav: { platform?: string; userAgentData?: { platform?: string } } | undefined =
  typeof navigator === 'undefined' ? undefined : (navigator as unknown as { platform?: string; userAgentData?: { platform?: string } })): boolean {
  const platform = nav?.userAgentData?.platform || nav?.platform || '';
  return /mac|iphone|ipad|ipod/i.test(platform);
}

/** AltGr: Alt, or Ctrl+Alt on Windows, or the AltGraph state (Swiss AltGr+2 types @). */
function altHeld(event: ShortcutKey): boolean {
  return Boolean(event.altKey || event.getModifierState?.('AltGraph'));
}

/**
 * A keydown handler with the go sequence's memory. It returns whether it took the key; the caller
 * then calls preventDefault. Left alone: keys during IME composition, keys already handled, keys
 * typed into a field, keys inside or behind an open dialog, and anything with Ctrl, ⌘ or Alt held
 * (except the `primary` combinations, which never fire with Alt).
 *
 *   const handle = createShortcutHandler({ keys: { '/': focusSearch }, go: { i: () => navigate('/inbox') } });
 *   window.addEventListener('keydown', (e) => { if (handle(e)) e.preventDefault(); });
 */
export function createShortcutHandler(options: ShortcutOptions) {
  const { keys = {}, go = {}, primary = {}, goKey = 'g', timeout = 1500, now = Date.now } = options;
  const dialogOpen = options.dialogOpen ?? defaultDialogOpen;
  let goSince = 0;
  const run = (action: ShortcutAction | undefined) => (action ? action() !== false : false);

  return function handle(event: ShortcutKey): boolean {
    if (event.defaultPrevented || event.isComposing || event.keyCode === 229) return false;
    const alt = altHeld(event);
    const apple = options.apple ?? isApplePlatform();
    const primaryHeld = apple ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
    if (event.ctrlKey || event.metaKey || alt) {
      goSince = 0;
      if (!primaryHeld || alt || inDialog(event.target)) return false;
      return run(primary[event.key.toLowerCase()]);
    }
    if (isTypingTarget(event.target) || inDialog(event.target) || dialogOpen()) {
      goSince = 0;
      return false;
    }
    const key = event.key;
    const pending = goSince > 0 && now() - goSince <= timeout;
    goSince = 0;
    if (pending) {
      const action = go[key.toLowerCase()];
      if (action && run(action)) return true;
      // Any other key ends the sequence; it still works on its own below.
    }
    if (key === goKey && Object.keys(go).length > 0) {
      goSince = now();
      return true;
    }
    return run(keys[key]);
  };
}
