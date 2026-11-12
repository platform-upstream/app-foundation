// Web Push in the service worker (app-foundation registry module `push`). Registry-owned: do not
// edit in an application; `frontend-registry check` fails on any change. Load it from the app's
// own public/sw-app.js and wire the two events there, with the app's own title and icon:
//
//   importScripts('sw-push.js');
//   self.addEventListener('push', (event) =>
//     FoundationPush.handlePush(event, { fallbackTitle: 'Acme', icon: '/pwa/icon-192.png' }));
//   self.addEventListener('notificationclick', (event) => FoundationPush.handleClick(event));
//
// The payload is the app's own JSON: {title, body, url, tag, badge} (also `id` as the tag, the
// shape Console and Paper send) or a Declarative Web Push message
// ({"web_push": 8030, "notification": {title, body, navigate, tag, app_badge}}). Every push shows
// a notification (iOS drops the subscription of a worker that stays silent); a missing or broken
// payload shows the fallback title. What a push says and counts stays the app's.
(() => {
  const TITLE_MAX = 120;
  const BODY_MAX = 500;
  const TAG_MAX = 120;

  // Text cut to `max` characters (code points, so an emoji is never split), ending in "…" when cut.
  function clip(value, max) {
    if (typeof value !== 'string') return '';
    const text = value.trim();
    const chars = Array.from(text);
    return chars.length <= max ? text : chars.slice(0, max - 1).join('') + '…';
  }

  // A count for the app icon: a whole number from 0, as a number or a numeric string; else none.
  function count(value) {
    const n = typeof value === 'string' && /^\s*\d+\s*$/.test(value) ? Number(value) : value;
    return typeof n === 'number' && Number.isFinite(n) && n >= 0 ? Math.min(Math.floor(n), 1e6) : undefined;
  }

  // A path on this app (scope) only: never another origin, never a protocol-relative "//host".
  function sameOriginURL(value) {
    const scope = self.registration.scope;
    if (typeof value !== 'string' || value === '' || /^\s|[\\\u0000-\u001f]/.test(value)) return scope;
    try {
      const url = new URL(value, scope);
      if (url.origin !== new URL(scope).origin || !url.href.startsWith(scope)) return scope;
      return url.href;
    } catch {
      return scope;
    }
  }

  /** The notification a push payload asks for: {title, options, badge}. Never throws. */
  function parsePush(raw, settings = {}) {
    let data = raw;
    if (typeof raw === 'string') {
      try { data = JSON.parse(raw); } catch { data = null; }
    }
    if (typeof data !== 'object' || data === null || Array.isArray(data)) data = {};
    // Declarative Web Push (Safari 18.4): the same fields under `notification`.
    const declarative = data.web_push === 8030 && typeof data.notification === 'object' && data.notification !== null;
    const source = declarative ? data.notification : data;
    const url = declarative ? source.navigate : source.url;
    const tag = clip(typeof source.tag === 'string' && source.tag ? source.tag : source.id, TAG_MAX);
    const options = {
      body: clip(source.body, BODY_MAX),
      data: { url: sameOriginURL(url) },
    };
    // One tag per subject: a re-fire, an update and an all-clear replace each other.
    if (tag) options.tag = tag;
    if (typeof settings.icon === 'string' && settings.icon) options.icon = settings.icon;
    return {
      title: clip(source.title, TITLE_MAX) || clip(settings.fallbackTitle, TITLE_MAX) || 'Notification',
      options,
      badge: count(declarative ? source.app_badge : source.badge),
    };
  }

  /** Sets the app icon's count (0 clears it) where the Badging API exists; never throws. */
  async function applyBadge(n) {
    const nav = self.navigator;
    if (n === undefined || !nav) return;
    try {
      if (n > 0 && typeof nav.setAppBadge === 'function') await nav.setAppBadge(n);
      else if (n === 0 && typeof nav.clearAppBadge === 'function') await nav.clearAppBadge();
    } catch {
      // A refused badge (no notification permission) is not worth failing the notification.
    }
  }

  /** The push listener's work: shows the notification and sets the badge. Returns the promise. */
  function handlePush(event, settings = {}) {
    let raw;
    try { raw = event.data ? event.data.text() : undefined; } catch { raw = undefined; }
    const { title, options, badge } = parsePush(raw, settings);
    const done = Promise.all([self.registration.showNotification(title, options), applyBadge(badge)]);
    event.waitUntil(done);
    return done;
  }

  /**
   * The notificationclick listener's work: focuses an open window of the app and then navigates
   * it to the notification's page (focus first, while the click still counts as a user gesture),
   * or opens a new window there. Only pages of this app are ever opened.
   */
  function handleClick(event) {
    event.notification.close();
    const data = event.notification.data;
    const target = sameOriginURL(data && typeof data === 'object' ? data.url : undefined);
    const done = (async () => {
      const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      const open = windows.find((client) => typeof client.url === 'string' && client.url.startsWith(self.registration.scope));
      if (open) {
        const focused = await open.focus().catch(() => open);
        const client = focused || open;
        // navigate() rejects for a window this worker does not control; the focused app stays.
        if (client.url !== target && typeof client.navigate === 'function') await client.navigate(target).catch(() => {});
        return;
      }
      await self.clients.openWindow(target);
    })();
    event.waitUntil(done);
    return done;
  }

  self.FoundationPush = Object.freeze({ handlePush, handleClick, parsePush, applyBadge });
})();
