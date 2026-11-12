// Web Push on the page (app-foundation registry module `push`). Registry-owned: do not edit in an
// application; `frontend-registry check` fails on any change. The service worker side is
// public/sw-push.js, loaded from the app's public/sw-app.js.
import { useCallback, useEffect, useState } from 'react';

/**
 * unsupported: no service worker or Push API. needs-install: iOS in a browser tab (Web Push only
 * works in the Home Screen app). denied: the person blocked notifications. off/on: this device's
 * subscription. unknown: still looking.
 */
export type PushState = 'unknown' | 'unsupported' | 'needs-install' | 'denied' | 'off' | 'on';

export type PushOptions = {
  /** The base of the app's push routes (default /api/push): GET key → {publicKey}, POST subscribe
   *  (the subscription's JSON, push.Subscription), POST unsubscribe ({endpoint}). */
  endpoint?: string;
  /** The VAPID public key (base64url). Without it, it is read from `${endpoint}/key`. */
  vapidKey?: string;
};

export type Push = {
  state: PushState;
  /** A request in flight (subscribe or unsubscribe). */
  busy: boolean;
  /** Why the last subscribe or unsubscribe failed, for the app to show; '' when it did not. */
  error: string;
  /** Asks for permission (call it from a tap: iOS only asks then), subscribes, tells the server. */
  subscribe: () => Promise<void>;
  /** Tells the server and drops this device's subscription; either alone is enough to stop. */
  unsubscribe: () => Promise<void>;
};

type BadgeNavigator = { setAppBadge?: (n?: number) => Promise<void>; clearAppBadge?: () => Promise<void> };

/**
 * Sets the app icon's count where the platform has one (Home Screen app on iOS 16.4+, installed
 * Chromium apps); 0 clears it. A no-op elsewhere; never throws (iOS refuses it without
 * notification permission).
 */
export function setAppBadge(n: number, nav: BadgeNavigator | undefined = typeof navigator === 'undefined' ? undefined : (navigator as BadgeNavigator)): void {
  if (!nav || !Number.isFinite(n)) return;
  try {
    const done = n > 0 ? nav.setAppBadge?.(Math.floor(n)) : nav.clearAppBadge?.();
    done?.catch?.(() => {});
  } catch {
    // Badging is a nicety.
  }
}

/** The base64url VAPID key as the bytes pushManager.subscribe wants. */
export function applicationServerKey(publicKey: string): Uint8Array<ArrayBuffer> {
  const base64 = publicKey.trim().replace(/-/g, '+').replace(/_/g, '/');
  const text = atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '='));
  const bytes = new Uint8Array(new ArrayBuffer(text.length));
  for (let i = 0; i < text.length; i++) bytes[i] = text.charCodeAt(i);
  return bytes;
}

type Env = { serviceWorker: boolean; pushManager: boolean; standalone: boolean; ios: boolean; permission: NotificationPermission | 'unsupported' };

/** Where push stands before asking the browser about a subscription; null: look it up. */
export function pushStateFrom(env: Env): PushState | null {
  if (env.ios && !env.standalone) return 'needs-install';
  if (!env.serviceWorker || !env.pushManager || env.permission === 'unsupported') return 'unsupported';
  if (env.permission === 'denied') return 'denied';
  return null;
}

function environment(): Env {
  const standalone =
    (typeof matchMedia !== 'undefined' && matchMedia('(display-mode: standalone)').matches) ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true;
  // iPadOS reports a Mac; a touch screen tells them apart.
  const ios = /iP(hone|ad|od)/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
  return {
    serviceWorker: 'serviceWorker' in navigator,
    pushManager: typeof window !== 'undefined' && 'PushManager' in window,
    standalone,
    ios,
    permission: typeof Notification === 'undefined' ? 'unsupported' : Notification.permission,
  };
}

function message(step: string, error: unknown): string {
  return `${step}: ${error instanceof Error ? error.message : String(error)}`;
}

/** The registration, or an error instead of waiting forever (dev server, worker failed to install). */
async function readyRegistration(timeoutMs = 10_000): Promise<ServiceWorkerRegistration> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error('no active service worker')), timeoutMs);
  });
  try {
    return await Promise.race([navigator.serviceWorker.ready, timeout]);
  } finally {
    clearTimeout(timer);
  }
}

async function post(url: string, body: unknown): Promise<void> {
  const response = await fetch(url, {
    method: 'POST', credentials: 'same-origin', redirect: 'manual',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: JSON.stringify(body),
  });
  if (response.type === 'opaqueredirect') throw new Error('signed out');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
}

async function publicKey(endpoint: string): Promise<string> {
  const response = await fetch(`${endpoint}/key`, { credentials: 'same-origin', redirect: 'manual', headers: { Accept: 'application/json' } });
  if (response.type === 'opaqueredirect') throw new Error('signed out');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const body = (await response.json()) as { publicKey?: unknown };
  if (typeof body.publicKey !== 'string' || body.publicKey === '') throw new Error('no public key');
  return body.publicKey;
}

/** This device's Web Push subscription and the switch for it. */
export function usePush({ endpoint = '/api/push', vapidKey }: PushOptions = {}): Push {
  const base = endpoint.replace(/\/+$/, '');
  const [state, setState] = useState<PushState>('unknown');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const early = pushStateFrom(environment());
    if (early) { setState(early); return; }
    let live = true;
    navigator.serviceWorker.getRegistration()
      .then((registration) => registration?.pushManager.getSubscription() ?? null)
      .then((subscription) => { if (live) setState(subscription ? 'on' : 'off'); })
      .catch(() => { if (live) setState('off'); });
    return () => { live = false; };
  }, []);

  const subscribe = useCallback(async () => {
    setError('');
    setBusy(true);
    try {
      const early = pushStateFrom(environment());
      if (early) { setState(early); return; }
      let permission: NotificationPermission;
      try {
        permission = await Notification.requestPermission();
      } catch (e) { setError(message('permission', e)); return; }
      if (permission !== 'granted') { setState(permission === 'denied' ? 'denied' : 'off'); return; }
      let subscription: PushSubscription;
      try {
        const key = vapidKey ?? (await publicKey(base));
        const registration = await readyRegistration();
        subscription = (await registration.pushManager.getSubscription()) ??
          (await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: applicationServerKey(key) }));
      } catch (e) { setError(message('subscribe', e)); return; }
      try {
        await post(`${base}/subscribe`, subscription.toJSON());
      } catch (e) { setError(message('register', e)); return; }
      setState('on');
    } finally {
      setBusy(false);
    }
  }, [base, vapidKey]);

  const unsubscribe = useCallback(async () => {
    setError('');
    setBusy(true);
    try {
      let subscription: PushSubscription | null;
      try {
        const registration = await navigator.serviceWorker.getRegistration();
        subscription = (await registration?.pushManager.getSubscription()) ?? null;
      } catch (e) { setError(message('lookup', e)); return; }
      if (!subscription) { setState('off'); return; }
      // Both legs run: a failure on one must not leave the switch on.
      const failures: string[] = [];
      const server = await post(`${base}/unsubscribe`, { endpoint: subscription.endpoint }).then(() => true, (e: unknown) => { failures.push(message('server', e)); return false; });
      const browser = await subscription.unsubscribe().then(() => true, (e: unknown) => { failures.push(message('browser', e)); return false; });
      if (failures.length) setError(failures.join('; '));
      if (server || browser) setState('off');
    } finally {
      setBusy(false);
    }
  }, [base]);

  return { state, busy, error, subscribe, unsubscribe };
}
