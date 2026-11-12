// Theme before the first paint (app-foundation registry module `profile`). Registry-owned: do not
// edit in an application; `frontend-registry check` fails on any change. For an app with a dark
// look (ProfileProvider `themes`); index.html loads it in <head> before any stylesheet, as a file
// because a CSP without 'unsafe-inline' blocks inline scripts:
//
//   <meta name="theme-color" content="#ffffff" data-light="#ffffff" data-dark="#16181d" />
//   <script src="/profile-theme.js"></script>
//
// It sets <html data-theme="light|dark"> and color-scheme from the person's stored appearance
// (localStorage profile.theme, kept by profile.tsx: auto|light|dark; auto follows the system), so
// a dark app never flashes light. From then on it keeps every <meta name="theme-color"> that
// carries data-light/data-dark in step with data-theme (the app's colours; a `media` attribute
// there is dropped, the choice may differ from the system's). data-legacy-key on the script tag
// names an app's older key, read while profile.theme is not set yet.
(function () {
  var root = document.documentElement;
  var script = document.currentScript;
  var legacy = script && script.getAttribute('data-legacy-key');

  function preference() {
    try {
      return localStorage.getItem('profile.theme') || (legacy && localStorage.getItem(legacy)) || 'auto';
    } catch (e) {
      return 'auto'; // storage blocked
    }
  }

  function systemDark() {
    try {
      return window.matchMedia('(prefers-color-scheme: dark)').matches;
    } catch (e) {
      return false;
    }
  }

  try {
    var pref = preference();
    var dark = pref === 'dark' || (pref !== 'light' && systemDark());
    root.setAttribute('data-theme', dark ? 'dark' : 'light');
    root.style.colorScheme = dark ? 'dark' : 'light';
  } catch (e) {
    // Never break the page over its theme.
  }

  function paint() {
    var theme = root.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
    var metas = document.querySelectorAll('meta[name="theme-color"]');
    for (var i = 0; i < metas.length; i++) {
      var colour = metas[i].getAttribute('data-' + theme);
      if (!colour) continue;
      metas[i].setAttribute('content', colour);
      metas[i].removeAttribute('media');
    }
  }

  try {
    paint();
    if (typeof MutationObserver === 'function') {
      new MutationObserver(paint).observe(root, { attributes: true, attributeFilter: ['data-theme'] });
    }
  } catch (e) {
    // The status bar keeps index.html's colour.
  }
})();
