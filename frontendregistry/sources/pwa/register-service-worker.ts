export type ServiceWorkerUpdates = {
  onUpdateError?: (error: unknown) => void;
  reload?: () => void;
};

export function registerServiceWorker(options: ServiceWorkerUpdates = {}): () => void {
  if (!('serviceWorker' in navigator)) return () => {};

  let disposed = false;
  let hadController = Boolean(navigator.serviceWorker.controller);
  const reload = options.reload ?? (() => location.reload());
  const scriptURL = new URL('sw.js', document.baseURI);
  const bootModule = import.meta.url;

  const update = async () => {
    try {
      const registration = await navigator.serviceWorker.register(scriptURL, { updateViaCache: 'none' });
      await registration.update();
      const response = await fetch(new URL('./', document.baseURI), {
        cache: 'no-store',
        credentials: 'same-origin',
      });
      if (!response.ok || response.redirected) return;
      const documentCopy = new DOMParser().parseFromString(await response.text(), 'text/html');
      const entry = documentCopy.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src');
      if (!disposed && entry && new URL(entry, document.baseURI).href !== bootModule) reload();
    } catch (error) {
      options.onUpdateError?.(error);
    }
  };

  const onControllerChange = () => {
    if (hadController && !disposed) reload();
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
