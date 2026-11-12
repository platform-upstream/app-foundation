import { defineConfig } from '@playwright/test';

// Phone audit: `pnpm build && pnpm test:phone`. Playwright serves the built UI with `vite preview`
// and e2e/phone.spec.ts answers the API from fixtures, so no backend runs. CHROME=<chromium binary>
// runs a browser other than Playwright's own (`pnpm exec playwright install chromium`).
const port = __PHONE_PORT__;

export default defineConfig({
  testDir: 'e2e',
  testIgnore: 'foundation/**',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  reporter: 'list',
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    browserName: 'chromium',
    launchOptions: process.env.CHROME ? { executablePath: process.env.CHROME } : {},
  },
  webServer: {
    command: `vite preview --host 127.0.0.1 --port ${port} --strictPort`,
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: false,
  },
});
