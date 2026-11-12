// Runs the phone audit (argv[2], TypeScript through Node's type stripping) in Chromium against two
// fixture pages: clean.html must pass every check, defects.html must trip each one. Needs the
// Playwright library (PHONE_AUDIT_PLAYWRIGHT, a path to the `playwright` package) and a Chromium
// (CHROME, or Playwright's own). Started by TestPhoneAudit; exits non-zero on the first failure.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const audit = await import(pathToFileURL(process.argv[2]).href);
const { chromium } = (await import(pathToFileURL(join(process.env.PHONE_AUDIT_PLAYWRIGHT, 'index.mjs')).href)).default ?? (await import(pathToFileURL(join(process.env.PHONE_AUDIT_PLAYWRIGHT, 'index.mjs')).href));
const fixtures = join(dirname(fileURLToPath(import.meta.url)), 'phone-audit');
const ORIGIN = 'https://app.example.org';

const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {});
const noGlass = audit.forbidComputedStyle('no-glass', 'backdrop-filter');

async function run(file, viewport, extra = {}, options = {}) {
  const context = await browser.newContext({ ...audit.phoneContextOptions(viewport), baseURL: ORIGIN, ...extra });
  await context.route(ORIGIN + '/**', (route) => route.fulfill({ contentType: 'text/html', body: readFileSync(join(fixtures, file)) }));
  const page = await context.newPage();
  await audit.installLayoutShiftObserver(page);
  await audit.openRoute(page, '/', undefined, extra.reducedMotion === 'reduce' ? { animations: false } : undefined);
  const findings = extra.reducedMotion === 'reduce'
    ? await audit.auditReducedMotion(page, options)
    : await audit.auditPage(page, { axe: false, platformRules: [noGlass], ...options });
  await context.close();
  return findings;
}

const [se, phone] = audit.PHONE_VIEWPORTS;
assert.deepEqual([se.width, se.height, phone.width, phone.height], [375, 667, 390, 844]);
const options = audit.phoneContextOptions(phone);
assert.equal(options.hasTouch, true);
assert.equal(options.isMobile, true);
assert.equal(options.serviceWorkers, 'block');

for (const viewport of audit.PHONE_VIEWPORTS) {
  const clean = await run('clean.html', viewport);
  assert.deepEqual(clean, [], `clean.html at ${viewport.name}:\n${audit.formatFindings(clean)}`);
  const reduced = await run('clean.html', viewport, { reducedMotion: 'reduce' });
  assert.deepEqual(reduced, [], `clean.html under reduced motion at ${viewport.name}:\n${audit.formatFindings(reduced)}`);
}

// A refetch that pushes the content down is a layout shift.
const shift = (page) => page.evaluate(() => {
  const banner = document.createElement('div');
  banner.style.height = '120px';
  banner.textContent = 'Neu geladen';
  document.getElementById('content').prepend(banner);
});
const found = await run('defects.html', phone, {}, { refetch: shift });
const report = audit.formatFindings(found);
const has = (check, selector) => found.some((f) => f.check === check && (!selector || (f.selector ?? '').includes(selector)));
for (const [check, selector] of [
  ['autofocus', 'input#search'],
  ['page-overflow'],
  ['wide-element', '#wide'],
  ['text-overflow', '#cramped'],
  ['text-overflow', '#stuffed'],
  ['number-split', '#split'],
  ['tap-target', '#tiny'],
  ['input-font', '#small-input'],
  ['small-text', '#fine-print'],
  ['no-glass', '.glass'],
  ['layout-shift'],
  ['sideways-pan'],
]) assert.ok(has(check, selector), `defects.html: expected ${check} ${selector ?? ''}\n${report}`);
// The reported tap target names its size.
assert.match(found.find((f) => f.check === 'tap-target' && f.selector.includes('#tiny')).detail, /^24 x 24 under 44 x 44$/);

// In-flow content wider than its control still counts after leaving out positioned children.
assert.match(found.find((f) => f.check === 'text-overflow' && f.selector.includes('#stuffed')).detail, /^content 100px wide in 50px$/);

// skip leaves a check out.
const skipped = await run('defects.html', phone, {}, { skip: ['small-text', 'layout-shift', 'sideways-pan'] });
assert.ok(!skipped.some((f) => ['small-text', 'layout-shift', 'sideways-pan'].includes(f.check)), audit.formatFindings(skipped));
// ignore hides an element and what is inside it.
const ignoring = await run('defects.html', phone, {}, { ignore: '#tiny, #fine-print', skip: ['layout-shift', 'sideways-pan'] });
assert.ok(!ignoring.some((f) => /#tiny|#fine-print/.test(f.selector ?? '')), audit.formatFindings(ignoring));

const spinning = await run('defects.html', phone, { reducedMotion: 'reduce' });
assert.ok(spinning.some((f) => f.check === 'running-animation' && f.selector === 'div.spin' && f.detail.includes('"spin"')), audit.formatFindings(spinning));

await browser.close();
console.log('phone-audit ok');
