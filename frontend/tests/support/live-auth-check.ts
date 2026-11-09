import { readFileSync } from 'node:fs';
import { chromium, firefox, type Browser } from '@playwright/test';
import { liveAuthChecks } from './live-auth.ts';

// Standalone browser process: no Playwright Test fixtures, assertions, reporters,
// traces or automatic ARIA failure snapshots ever see the password or cookie.
let checkpoint = 1;
let browser: Browser | undefined;
function checking(check: (typeof liveAuthChecks)[number]) {
  checkpoint = liveAuthChecks.indexOf(check) + 1;
}
function requireTrue(value: boolean) {
  if (!value) throw new Error('Check failed.');
}

try {
  const browserType =
    process.argv[2] === 'firefox'
      ? firefox
      : process.argv[2] === 'chromium'
        ? chromium
        : null;
  if (!browserType || !process.argv[3] || !process.env.E2E_PASSWORD_FILE) {
    throw new Error('Missing live test settings.');
  }
  const password = readFileSync(process.env.E2E_PASSWORD_FILE, 'utf8').replace(
    /\r?\n$/,
    '',
  );
  browser = await browserType.launch({ timeout: 15_000 });
  const context = await browser.newContext({ baseURL: process.argv[3] });
  context.setDefaultTimeout(5_000);
  const page = await context.newPage();

  checking('opening the login page');
  await page.goto('/login');
  checking('entering credentials');
  await page.getByLabel('Username', { exact: true }).fill(process.env.E2E_USERNAME ?? '');
  await page.getByLabel('Password', { exact: true }).fill(password);
  checking('signing in');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('heading', { name: 'You’re signed in.' }).waitFor();

  checking('checking cookie policy');
  const cookie = (await context.cookies()).find(
    (cookie) => cookie.name === 'cyberguard_session',
  );
  requireTrue(
    Boolean(cookie?.httpOnly && cookie.path === '/' && cookie.sameSite === 'Lax'),
  );
  requireTrue(
    !(await page.evaluate(() => document.cookie.includes('cyberguard_session'))),
  );
  checking('checking browser storage');
  requireTrue(
    (await page.evaluate(() => localStorage.length + sessionStorage.length)) === 0,
  );
  checking('restoring the session');
  await page.reload();
  await page.getByRole('heading', { name: 'You’re signed in.' }).waitFor();
  checking('signing out');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByRole('heading', { name: 'Welcome back' }).waitFor();
  checking('rejecting the revoked cookie');
  const revoked = await context.request.get('/api/auth/me', {
    headers: { Cookie: `cyberguard_session=${cookie?.value ?? ''}` },
    timeout: 5_000,
  });
  requireTrue(revoked.status() === 401);
  checking('confirming sign-out after reload');
  await page.reload();
  await page.getByRole('heading', { name: 'Welcome back' }).waitFor();
} catch {
  // Do not serialize the original error: action logs can include passwords/cookies.
  process.exitCode = checkpoint;
} finally {
  await browser?.close().catch(() => {
    process.exitCode ||= checkpoint;
  });
}
