export type ServiceWorkerUpdates = {
  // Called when a newer build is live. Without it the page reloads at once. With it the
  // application decides: call `apply` now, later (after asking the user, or once nothing would be
  // interrupted), or not at all. It is called again on every later check that still finds the
  // newer build, so an update that was put off is offered again when the app is next opened.
  onUpdate?: (apply: () => void) => void;
  onUpdateError?: (error: unknown) => void;
  reload?: () => void;
  // Where the application is served; only a path-prefixed preview needs anything but the root.
  base?: string;
  // The app's name (a DNS label) for the service worker's cache name: registers sw.js?name=<name>.
  // Without it the worker names its cache after the scope.
  name?: string;
};

export function registerServiceWorker(options: ServiceWorkerUpdates = {}): () => void {
  if (!('serviceWorker' in navigator)) return () => {};

  let disposed = false;
  let hadController = Boolean(navigator.serviceWorker.controller);
  const reload = options.reload ?? (() => location.reload());
  const found = () => {
    if (disposed) return;
    if (options.onUpdate) options.onUpdate(reload);
    else reload();
  };
  const origin = location.origin;
  const root = new URL(options.base ?? '/', origin);
  const scriptURL = new URL('sw.js', root);
  if (options.name) scriptURL.searchParams.set('name', options.name);
  const bootModule = import.meta.url;

  const update = async () => {
    try {
      const registration = await navigator.serviceWorker.register(scriptURL, { updateViaCache: 'none' });
      await registration.update();
      const response = await fetch(root, {
        cache: 'no-store',
        credentials: 'same-origin',
      });
      if (!response.ok || response.redirected) return;
      const documentCopy = new DOMParser().parseFromString(await response.text(), 'text/html');
      const entry = documentCopy.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src');
      if (entry && new URL(entry, root).href !== bootModule) found();
    } catch (error) {
      options.onUpdateError?.(error);
    }
  };

  const onControllerChange = () => {
    if (hadController) found();
    hadController = true;
  };
  const onLoad = () => { void update(); };
  const onVisibility = () => { if (!document.hidden) void update(); };

  navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);
  window.addEventListener('load', onLoad);
  document.addEventListener('visibilitychange', onVisibility);

  return () => {
    disposed = true;
    navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
    window.removeEventListener('load', onLoad);
    document.removeEventListener('visibilitychange', onVisibility);
  };
}
