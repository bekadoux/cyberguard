import { defineConfig, devices } from '@playwright/test';

const packagedOrigin = process.env.E2E_PACKAGED_ORIGIN;
if (packagedOrigin) {
  const url = new URL(packagedOrigin);
  if (
    url.protocol !== 'http:' ||
    url.hostname !== '127.0.0.1' ||
    !url.port ||
    ['8080', '8088', '5173'].includes(url.port) ||
    url.origin !== packagedOrigin
  ) {
    throw new Error(
      'E2E_PACKAGED_ORIGIN must be an explicit disposable loopback HTTP origin on a nonstandard port.',
    );
  }
}

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: false,
  // The backend deliberately limits login attempts per gateway peer.
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: packagedOrigin ?? 'http://127.0.0.1:5173',
    viewport: { width: 1440, height: 960 },
    // Fixtures use synthetic credentials. Real credentials stay in a standalone process;
    // these switches alone do not disable automatic ARIA/action failure diagnostics.
    trace: 'off',
    video: 'off',
    screenshot: 'off',
  },
  projects: [
    {
      name: 'packaged-firefox',
      testMatch: 'packaged.spec.ts',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'packaged-chromium',
      testMatch: 'packaged.spec.ts',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'incident-live-firefox',
      testMatch: 'incidents.live.spec.ts',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'incident-live-chromium',
      testMatch: 'incidents.live.spec.ts',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'firefox',
      testMatch: [
        'login.spec.ts',
        'accessibility.spec.ts',
        'live-auth-safety.spec.ts',
        'incidents.spec.ts',
        'submission.spec.ts',
      ],
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'chromium',
      testMatch: [
        'login.spec.ts',
        'accessibility.spec.ts',
        'live-auth-safety.spec.ts',
        'incidents.spec.ts',
        'submission.spec.ts',
      ],
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'live-firefox',
      testMatch: 'auth.live.spec.ts',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'live-chromium',
      testMatch: 'auth.live.spec.ts',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: packagedOrigin
    ? undefined
    : {
        command: 'npm run dev',
        url: 'http://127.0.0.1:5173',
        reuseExistingServer: !process.env.CI,
      },
});
