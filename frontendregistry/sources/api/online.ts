// Connectivity (app-foundation registry module `api`). Registry-owned: do not edit in an
// application; `frontend-registry check` fails on any change. What the app shows while offline,
// and whether its queries pause, stays in the application.
import { useSyncExternalStore } from 'react';
import { APIError } from './api.js';

function subscribe(onChange: () => void): () => void {
  window.addEventListener('online', onChange);
  window.addEventListener('offline', onChange);
  return () => {
    window.removeEventListener('online', onChange);
    window.removeEventListener('offline', onChange);
  };
}

/** navigator.onLine; true where it is unknown (it only ever proves being offline). */
export function isOnline(): boolean {
  return typeof navigator === 'undefined' || navigator.onLine !== false;
}

/** The browser's connectivity, re-rendering on its online and offline events. */
export function useOnline(): boolean {
  return useSyncExternalStore(subscribe, isOnline, () => true);
}

/**
 * Whether a request failed because it never reached a server (offline, DNS, connection reset) as
 * opposed to an answer from it: an APIError (any status, also the 401 of an expired session) is
 * an answer. A failed fetch is a TypeError: "Failed to fetch" (Chromium), "NetworkError when
 * attempting to fetch resource" (Firefox), "Load failed" (Safari).
 */
export function isOfflineError(error: unknown): boolean {
  if (error instanceof APIError) return false;
  if (!isOnline()) return true;
  if (!(error instanceof Error)) return false;
  return error.name === 'TypeError' && /fetch|network|load failed/i.test(error.message);
}
