import type { Page } from '@playwright/test';
import { phoneAudit } from './foundation/phone-audit-tests.ts';

// The app's phone audit (registry module `phone-audit`): every route at 375x667 and 390x844 with
// touch, then once with reduced motion. It fails on sideways pan, overflowing text, targets under
// 44px, fields under 16px, text under 14px, autofocus, layout shift on a refetch and axe findings.
// Add each new route, with fixtures that look like real data (long words, long numbers).
const notes = [
  { id: 3, text: 'Call the plumber about the kitchen tap before Friday', createdAt: '2026-11-12T08:30:00Z' },
  { id: 2, text: 'Grundstücksentwässerungsanlage: offer 1’284.50 CHF', createdAt: '2026-11-11T17:05:00Z' },
  { id: 1, text: 'Book the train', createdAt: '2026-11-10T06:45:00Z' },
];

async function api(page: Page) {
  await page.route('**/api/notes', (route) => route.fulfill({ json: notes }));
}

phoneAudit({
  setup: api,
  routes: [{ path: '/', name: 'notes', ready: (page) => page.getByRole('listitem').first().waitFor() }],
});
