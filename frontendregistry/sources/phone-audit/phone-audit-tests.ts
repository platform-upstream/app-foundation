/**
 * Phone audit as Playwright tests (app-foundation registry module `phone-audit`). Registry-owned:
 * do not edit in an application. The application's own spec (e2e/phone.spec.ts, see
 * docs/frontend-registry.md) calls phoneAudit() with its routes and fixtures:
 *
 *   import { phoneAudit } from './foundation/phone-audit-tests.ts';
 *   phoneAudit({ setup: mockApi, routes: [{ path: '/' }, { path: '/settings' }] });
 *
 * One test per route and phone size (375x667, 390x844), then one reduced-motion test per route at
 * the last size. A failing test lists every finding; the findings are also attached as JSON. Every
 * test is also an error gate: an uncaught exception, a console error or a failed request while the
 * route is open fails it (`allowErrors` accepts the application's documented noise, `skip` takes
 * `page-error`, `console-error`, `request-failed`).
 */
import { expect, test, type Page } from '@playwright/test';
import {
  PHONE_VIEWPORTS,
  auditPage,
  auditReducedMotion,
  formatFindings,
  installLayoutShiftObserver,
  openRoute,
  phoneContextOptions,
  watchErrors,
  type AuditOptions,
  type Finding,
  type PhoneViewport,
} from './phone-audit.ts';

export interface PhoneRoute {
  /** The path to open, relative to the Playwright baseURL. */
  path: string;
  /** Test title; defaults to the path. */
  name?: string;
  /** Route-specific fixtures (page.route mocks, local storage), after the config's setup. */
  setup?: (page: Page) => Promise<void>;
  /** Waits until the page shows its data (a heading, a list); the audit then settles. */
  ready?: (page: Page) => Promise<void>;
  /** Overrides for this route (an extra ignore, a skipped check). */
  options?: AuditOptions;
}

export interface PhoneAuditConfig extends AuditOptions {
  routes: PhoneRoute[];
  /** Fixtures for every route, before the route's own setup. */
  setup?: (page: Page) => Promise<void>;
  /** Phone sizes; default PHONE_VIEWPORTS. */
  viewports?: readonly PhoneViewport[];
  /** Browser locale; default de-CH (long German words and ’ separators find the most breakage). */
  locale?: string;
  /** Run the reduced-motion pass (default true). */
  reducedMotion?: boolean;
}

/** Opens the route; the returned function gives the page errors since (the error gate). */
async function open(page: Page, config: PhoneAuditConfig, route: PhoneRoute, settleAnimations: boolean): Promise<() => Finding[]> {
  const allow = route.options?.allowErrors ?? config.allowErrors;
  const skip = new Set([...(config.skip ?? []), ...(route.options?.skip ?? [])]);
  const errors = watchErrors(page, allow);
  await config.setup?.(page);
  await route.setup?.(page);
  await installLayoutShiftObserver(page);
  await openRoute(page, route.path, route.ready, { animations: settleAnimations });
  return () => errors().filter((finding) => !skip.has(finding.check));
}

async function report(findings: Finding[]): Promise<void> {
  await test.info().attach('phone-audit.json', { body: JSON.stringify(findings, null, 2), contentType: 'application/json' });
  expect(findings, formatFindings(findings)).toEqual([]);
}

/** Registers the phone-audit tests for the given routes. Call once at the top level of a spec. */
export function phoneAudit(config: PhoneAuditConfig): void {
  const viewports = config.viewports ?? PHONE_VIEWPORTS;
  const { routes, setup: _setup, viewports: _viewports, locale, reducedMotion = true, ...shared } = config;
  for (const viewport of viewports) {
    test.describe(`phone ${viewport.name}`, () => {
      test.use(phoneContextOptions(viewport, locale ? { locale } : {}));
      for (const route of routes) {
        test(route.name ?? route.path, async ({ page }) => {
          const errors = await open(page, config, route, true);
          const findings = await auditPage(page, { ...shared, ...route.options });
          await report([...findings, ...errors()]);
        });
      }
    });
  }
  if (!reducedMotion) return;
  const last = viewports[viewports.length - 1];
  test.describe(`phone ${last.name} reduced motion`, () => {
    // reducedMotion is a context option, not a test fixture: it goes through contextOptions.
    test.use({ ...phoneContextOptions(last, locale ? { locale } : {}), contextOptions: { reducedMotion: 'reduce' } });
    for (const route of routes) {
      test(route.name ?? route.path, async ({ page }) => {
        const errors = await open(page, config, route, false);
        const findings = await auditReducedMotion(page, { ignore: route.options?.ignore ?? config.ignore });
        await report([...findings, ...errors()]);
      });
    }
  });
}
