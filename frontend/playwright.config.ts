import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: false,
  // The backend deliberately limits login attempts per gateway peer.
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:5173',
    viewport: { width: 1440, height: 960 },
    // Fixtures use synthetic credentials. Real credentials stay in a standalone process;
    // these switches alone do not disable automatic ARIA/action failure diagnostics.
    trace: 'off',
    video: 'off',
    screenshot: 'off',
  },
  projects: [
    {
      name: 'firefox',
      testMatch: ['login.spec.ts', 'accessibility.spec.ts', 'live-auth-safety.spec.ts'],
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'chromium',
      testMatch: ['login.spec.ts', 'accessibility.spec.ts', 'live-auth-safety.spec.ts'],
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
  webServer: {
    command: 'npm run dev',
    url: 'http://127.0.0.1:5173',
    reuseExistingServer: !process.env.CI,
  },
});
