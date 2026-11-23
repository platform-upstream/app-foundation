import { defineConfig } from '@playwright/test';

// Phone audit: `pnpm build && pnpm test:phone`. Playwright serves the built UI with `vite preview`
// and e2e/phone.spec.ts answers the API from fixtures, so no backend runs. CHROME=<chromium binary>
// runs a browser other than Playwright's own (`pnpm exec playwright install chromium`).
// In CI a port per run: a pull request's verify build and main's delivery of the same app can run
// at the same time on the build host and share its network ("Port … is already in use"). The
// workers inherit the variable, so they all reach the same server.
process.env.PHONE_PORT ??= String(process.env.CI ? 20000 + Math.floor(Math.random() * 20000) : __PHONE_PORT__);
const port = Number(process.env.PHONE_PORT);

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
