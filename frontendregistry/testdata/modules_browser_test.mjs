// The browser side of the modules touch, sheet and pull-refresh in Chromium: CSS that only a layout
// engine can judge and focus that only a real dialog moves. argv[2] is the sources directory; the
// TypeScript modules are served with their types stripped. Needs the Playwright library
// (PHONE_AUDIT_PLAYWRIGHT, a path to the `playwright` package) and a Chromium (CHROME, or
// Playwright's own). Started by TestModulesInBrowser; exits non-zero on the first failure.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
process.removeAllListeners('warning'); // stripTypeScriptTypes is experimental
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const sources = process.argv[2];
const playwright = await import(pathToFileURL(join(process.env.PHONE_AUDIT_PLAYWRIGHT, 'index.mjs')).href);
const { chromium } = playwright.default ?? playwright;
const ORIGIN = 'https://app.example.org';

const files = {
  '/touch.css': ['text/css', readFileSync(join(sources, 'touch', 'touch.css'), 'utf8')],
  '/sheet.css': ['text/css', readFileSync(join(sources, 'sheet', 'sheet.css'), 'utf8')],
  '/sheet.js': ['text/javascript', stripTypeScriptTypes(readFileSync(join(sources, 'sheet', 'sheet.ts'), 'utf8'))],
  '/pull.js': ['text/javascript', stripTypeScriptTypes(readFileSync(join(sources, 'pull-refresh', 'pull.ts'), 'utf8'))],
  '/': ['text/html', `<!doctype html>
<html lang="de"><head><meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/touch.css"><link rel="stylesheet" href="/sheet.css">
<style>.chip { height: 24px; padding: 0 8px; } .menu { position: fixed; top: 0; left: 0; width: 10px; height: 10px; }</style>
</head><body>
<button id="plain" type="button">Speichern</button>
<button id="chip" type="button" class="hit-target chip">Filter</button>
<button id="open" type="button">Öffnen</button>
<dialog id="sheet" aria-label="Bearbeiten">
  <button id="first" type="button">Abbrechen</button>
  <input id="title" aria-label="Titel" data-autofocus>
  <input id="note" aria-label="Notiz">
  <div class="menu" id="menu"></div>
</dialog>
<div id="radix" role="dialog" data-state="closed"></div>
<script type="module">
  import * as sheet from '/sheet.js';
  import * as pull from '/pull.js';
  window.sheet = sheet;
  window.pull = pull;
  window.ready = true;
</script>
</body></html>`],
};

const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {});

async function open(touch) {
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    ...(touch ? { isMobile: true, hasTouch: true, deviceScaleFactor: 3 } : {}),
    serviceWorkers: 'block',
  });
  await context.route(ORIGIN + '/**', (route) => {
    const [type, body] = files[new URL(route.request().url()).pathname] ?? ['text/plain', ''];
    return route.fulfill({ contentType: type, body });
  });
  const page = await context.newPage();
  await page.goto(ORIGIN + '/');
  await page.waitForFunction(() => window.ready);
  return { context, page };
}

// touch: the 44px floor on coarse pointers, but not on a .hit-target (its ::after takes the finger).
{
  const { context, page } = await open(true);
  assert.equal(await page.evaluate(() => matchMedia('(pointer: coarse)').matches), true, 'a coarse pointer');
  const height = (id) => page.evaluate((id) => document.getElementById(id).getBoundingClientRect().height, id);
  assert.ok(await height('plain') >= 44, 'a plain button gets the floor');
  assert.equal(await height('chip'), 24, '.hit-target keeps its own height');
  await context.close();
}

// sheet: focus, and translate only while a drag offset is set.
{
  const { context, page } = await open(false);
  const active = () => page.evaluate(() => document.activeElement?.id);
  await page.evaluate(() => { document.getElementById('open').focus(); window.hide = window.sheet.showSheet(document.getElementById('sheet')); });
  assert.equal(await active(), 'title', 'desktop: [data-autofocus] takes the focus');
  assert.equal(await page.evaluate(() => getComputedStyle(document.getElementById('sheet')).translate), 'none', 'no translate at rest');
  assert.equal(await page.evaluate(() => document.getElementById('menu').getBoundingClientRect().top), 0, 'a fixed child is placed against the viewport');
  await page.evaluate(() => { const d = document.getElementById('sheet'); d.setAttribute('data-dragging', ''); d.style.setProperty('--sheet-drag', '30px'); });
  assert.equal(await page.evaluate(() => getComputedStyle(document.getElementById('sheet')).translate), '0px 30px', 'the drag offset moves it');
  await page.evaluate(() => { const d = document.getElementById('sheet'); d.removeAttribute('data-dragging'); d.style.removeProperty('--sheet-drag'); window.hide(); });
  assert.equal(await active(), 'open', 'focus back on the opener');

  await page.evaluate(() => { window.hide = window.sheet.showSheet(document.getElementById('sheet'), { initialFocus: '#note' }); });
  assert.equal(await active(), 'note', 'initialFocus as a selector');
  await page.evaluate(() => window.hide());
  await page.evaluate(() => { window.hide = window.sheet.showSheet(document.getElementById('sheet'), { initialFocus: document.getElementById('first') }); });
  assert.equal(await active(), 'first', 'initialFocus as an element');
  await page.evaluate(() => window.hide());
  await context.close();
}
{
  const { context, page } = await open(true);
  await page.evaluate(() => { window.sheet.showSheet(document.getElementById('sheet')); });
  assert.equal(await page.evaluate(() => document.activeElement?.id), 'sheet', 'touch: the dialog itself, never the field');
  await context.close();
}

// pull-refresh: a Radix dialog counts while open.
{
  const { context, page } = await open(true);
  assert.equal(await page.evaluate(() => window.pull.dialogOpen(document)), false, 'a closed Radix dialog');
  await page.evaluate(() => { document.getElementById('radix').dataset.state = 'open'; });
  assert.equal(await page.evaluate(() => window.pull.dialogOpen(document)), true, 'an open Radix dialog');
  await context.close();
}

await browser.close();
console.log('modules in the browser ok');
