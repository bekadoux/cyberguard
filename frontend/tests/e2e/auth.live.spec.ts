import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from '@playwright/test';

// Explicit live suite only. No secrets are passed into Vite or the browser bundle.
const username = process.env.E2E_USERNAME ?? 'employee_a';

test('real Go/Caddy login, HttpOnly cookie, persistence and server-side logout', async ({
  page,
  context,
}) => {
  const file = process.env.E2E_PASSWORD_FILE;
  if (!file)
    throw new Error(
      'E2E_PASSWORD_FILE must reference a local demo account password file.',
    );
  const password = readFileSync(resolve(file), 'utf8').replace(/\r?\n$/, '');
  await page.goto('/login');
  await page.getByLabel('Username', { exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  const cookie = (await context.cookies()).find(
    (cookie) => cookie.name === 'cyberguard_session',
  );
  // Boolean assertions avoid printing token values on a failed test.
  expect(
    Boolean(cookie?.httpOnly && cookie.path === '/' && cookie.sameSite === 'Lax'),
  ).toBe(true);
  expect(await page.evaluate(() => document.cookie.includes('cyberguard_session'))).toBe(
    false,
  );
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  const revoked = await context.request.get('/api/auth/me', {
    headers: { Cookie: `cyberguard_session=${cookie?.value ?? ''}` },
  });
  expect(revoked.status()).toBe(401);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});
