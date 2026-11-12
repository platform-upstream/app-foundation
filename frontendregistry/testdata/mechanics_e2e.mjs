// Browser acceptance of the registry modules sheet, toast and pull-refresh at 390×844 with touch.
// Not part of `go test` (it needs Chromium and a built fixture). The fixture: install
// `template,sheet,toast,pull-refresh` into an empty directory, replace src/App.tsx with
// mechanics_e2e_app.tsx, import sheet.css and toast.css in src/main.tsx, `pnpm build`, serve dist/
// (e.g. `pnpm vite preview --port 4719`). Then:
//   PLAYWRIGHT=<path to playwright/index.mjs> CHROME=<chromium binary> node mechanics_e2e.mjs http://127.0.0.1:4719/
import assert from 'node:assert/strict';

const { chromium } = await import(process.env.PLAYWRIGHT ?? 'playwright');
const url = process.argv[2];
const browser = await chromium.launch({ executablePath: process.env.CHROME || undefined });

async function phone({ standalone = false } = {}) {
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 3,
    isMobile: true,
    hasTouch: true,
    locale: 'de-CH',
    serviceWorkers: 'block',
  });
  if (standalone) await context.addInitScript(() => Object.defineProperty(navigator, 'standalone', { value: true }));
  const page = await context.newPage();
  await page.goto(url);
  await page.waitForSelector('#open');
  const cdp = await context.newCDPSession(page);
  // A real touch gesture (touch, pointer and scroll events), in steps.
  const swipe = async (from, to, steps = 12) => {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: from[0], y: from[1] }] });
    for (let i = 1; i <= steps; i++) {
      const x = from[0] + ((to[0] - from[0]) * i) / steps;
      const y = from[1] + ((to[1] - from[1]) * i) / steps;
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y }] });
      await page.waitForTimeout(16);
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await page.waitForTimeout(400);
  };
  const scrollTop = () => page.evaluate(() => document.querySelector('.app-shell-main').scrollTop);
  return { context, page, swipe, scrollTop };
}

// --- sheet ---
{
  const { context, page, swipe, scrollTop } = await phone();
  // Scrolled, opener still in view (a tap would scroll it into view).
  await page.evaluate(() => { const main = document.querySelector('.app-shell-main'); main.scrollTop = 120; });
  const scrolled = await scrollTop();
  assert.ok(scrolled > 100, `page scrolled: ${scrolled}`);
  await page.evaluate(() => { const open = document.getElementById('open'); open.focus({ preventScroll: true }); open.click(); });
  const sheet = page.getByTestId('Erstes');
  await sheet.waitFor();
  assert.equal(await sheet.evaluate((d) => d.matches(':modal')), true, 'modal, in the top layer');
  assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('.app-shell-main')).overflowY), 'hidden', 'the shell scroller is locked');
  assert.equal(await page.evaluate(() => document.body.style.overflow), '', 'body untouched');
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.testid), 'Erstes', 'touch: the dialog takes the focus, not the field');
  const box = await sheet.boundingBox();
  assert.ok(Math.abs(box.y + box.height - 844) < 1, `sheet sits on the bottom edge: ${JSON.stringify(box)}`);
  assert.equal(await sheet.evaluate((d) => getComputedStyle(d, '::after').content), '""', 'safe-area spacer present');

  // The page behind does not scroll: a swipe on the backdrop, then one on the sheet's content.
  // Chromium never chains these to an inert scroller (it passes without the lock too); iOS Safari
  // does, which is what the overflow lock above prevents. Only an iPhone confirms that part.
  await swipe([195, 120], [195, 20]);
  await swipe([195, box.y + box.height - 40], [195, box.y + 40]);
  assert.equal(await scrollTop(), scrolled, 'the page behind the sheet did not scroll');

  // Stack: Escape closes only the topmost sheet.
  await page.tap('#inner');
  await page.getByTestId('Zweites').waitFor();
  await page.keyboard.press('Escape');
  await page.waitForTimeout(100);
  assert.equal(await page.getByTestId('Zweites').evaluate((d) => d.open), false);
  assert.equal(await sheet.evaluate((d) => d.open), true, 'the sheet below stays open');
  assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('.app-shell-main')).overflowY), 'hidden', 'still locked: ref-counted');

  // A press on a button in the header stays a press: no drag.
  // Drag the header down: the sheet follows, closes, focus returns to the opener, lock released.
  const header = await sheet.locator('header').boundingBox();
  await swipe([100, header.y + 20], [100, header.y + 260]);
  await page.waitForTimeout(300);
  assert.equal(await sheet.evaluate((d) => d.open), false, 'dragged closed');
  assert.equal(await page.evaluate(() => document.activeElement?.id), 'open', 'focus back on the opener');
  assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('.app-shell-main')).overflowY), 'auto', 'lock released');
  assert.equal(await scrollTop(), scrolled, 'scroll position kept');

  // A short drag settles back and the sheet stays open.
  await page.tap('#open');
  await sheet.waitFor();
  const header2 = await sheet.locator('header').boundingBox();
  await swipe([100, header2.y + 20], [100, header2.y + 50], 20);
  assert.equal(await sheet.evaluate((d) => d.open), true, 'a short slow drag does not close');
  assert.ok(['none', '0px'].includes(await sheet.evaluate((d) => getComputedStyle(d).translate)), 'settled back');
  // Backdrop tap closes.
  await page.tap('body', { position: { x: 195, y: 60 } });
  await page.waitForTimeout(100);
  assert.equal(await sheet.evaluate((d) => d.open), false, 'a backdrop tap closes');
  await context.close();
  console.log('sheet: ok');
}

// --- toast ---
{
  const { context, page } = await phone();
  await page.tap('#toast');
  const item = page.locator('.toast-region [role=status]').first();
  await item.waitFor();
  const region = page.locator('.toast-region');
  assert.equal(await region.evaluate((r) => r.matches(':popover-open')), true, 'a popover in the top layer');
  const toastBox = await item.boundingBox();
  const navBox = await page.locator('.app-shell-nav').boundingBox();
  assert.ok(toastBox.y + toastBox.height <= navBox.y + 0.5, `toast above the nav: ${toastBox.y + toastBox.height} <= ${navBox.y}`);
  assert.ok(navBox.y - (toastBox.y + toastBox.height) < 40, 'and right above it (no double safe area)');
  await item.getByRole('button', { name: 'Rückgängig' }).tap();
  assert.equal(await page.evaluate(() => window.undone), 1);
  assert.equal(await page.locator('.toast-region [role=status]').count(), 0, 'undo dismissed it');

  // Inside an open sheet the toast moves into the dialog, so its button is not inert.
  await page.tap('#open');
  await page.getByTestId('Erstes').waitFor();
  await page.tap('#toast-in-sheet');
  await page.locator('.toast-region [role=status]').first().waitFor();
  assert.equal(await page.locator('.toast-region').evaluate((r) => r.parentElement?.tagName), 'DIALOG');
  await page.locator('.toast-region button').tap();
  assert.equal(await page.evaluate(() => window.undone), 2, 'undo pressable above a modal sheet');
  assert.equal(await page.getByTestId('Erstes').evaluate((d) => d.open), true, 'the tap did not close the sheet');
  await page.keyboard.press('Escape');
  await page.waitForTimeout(100);
  assert.equal(await page.locator('.toast-region').evaluate((r) => r.parentElement === document.body), true, 'back to body');
  await context.close();
  console.log('toast: ok');
}

// --- pull-refresh ---
{
  // A browser tab: never.
  const tab = await phone();
  await tab.swipe([195, 200], [195, 600], 20);
  assert.equal(await tab.page.evaluate(() => window.refreshed), 0, 'off in a browser tab');
  await tab.context.close();

  const { context, page, swipe, scrollTop } = await phone({ standalone: true });
  assert.equal(await scrollTop(), 0);
  const band = await page.locator('#band').boundingBox();
  // Mostly sideways (the band), drifting down: no pull.
  await swipe([300, band.y + 20], [40, band.y + 180], 20);
  assert.equal(await page.evaluate(() => window.refreshed), 0, 'a sideways swipe is no pull');
  // Upward: no pull.
  await swipe([195, 600], [195, 300], 20);
  assert.equal(await page.evaluate(() => window.refreshed), 0, 'an upward swipe is no pull');
  await page.evaluate(() => { document.querySelector('.app-shell-main').scrollTop = 0; });
  // Short vertical pull: indicator, no refresh.
  await swipe([195, 200], [195, 260], 10);
  assert.equal(await page.evaluate(() => window.refreshed), 0, 'a short pull settles');
  // Vertical pull past the trigger: refresh, busy, then rest.
  await swipe([195, 200], [195, 520], 20);
  assert.equal(await page.evaluate(() => window.refreshed), 1, 'a vertical pull refreshes');
  await page.waitForTimeout(600);
  assert.equal(await page.locator('.ptr').count(), 0, 'indicator gone after the refresh');
  // Not while a sheet is open.
  await page.tap('#open');
  await page.getByTestId('Erstes').waitFor();
  await swipe([195, 100], [195, 500], 20);
  assert.equal(await page.evaluate(() => window.refreshed), 1, 'off while a sheet is open');
  await context.close();
  console.log('pull-refresh: ok');
}

await browser.close();
console.log('all ok');
