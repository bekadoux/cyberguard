import { readFileSync } from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';
import { randomUUID } from 'node:crypto';
import { chromium, firefox, type Browser, type Page } from '@playwright/test';
import { incidentChecks } from './live-incidents.ts';

// All credentials/cookies and action diagnostics stay outside the test reporter.
// Only explicitly disposable loopback projects are accepted for these write checks.
let checkpoint = 1;
let browser: Browser | undefined;
const check = (name: (typeof incidentChecks)[number]) => {
  checkpoint = incidentChecks.indexOf(name) + 1;
};
const requireTrue = (value: boolean) => {
  if (!value) throw new Error('Check failed.');
};
try {
  const api = new URL(process.env.E2E_INCIDENT_API_ORIGIN ?? '');
  const packaged = process.env.E2E_INCIDENT_PACKAGED === '1';
  const secrets = resolve(process.env.E2E_INCIDENT_SECRETS_DIR ?? '');
  requireTrue(
    api.protocol === 'http:' &&
      api.hostname === '127.0.0.1' &&
      !!api.port &&
      !['8080', '8088', '5173'].includes(api.port) &&
      api.pathname === '/' &&
      !api.username &&
      !api.password &&
      !api.search &&
      !api.hash,
  );
  requireTrue(
    /^cyberguard-(smoke|ui)-/.test(basename(dirname(secrets))) &&
      basename(secrets) === 'secrets',
  );
  const type =
    process.argv[2] === 'firefox'
      ? firefox
      : process.argv[2] === 'chromium'
        ? chromium
        : null;
  if (!type || !process.argv[3]) throw new Error('Missing browser settings.');
  if (packaged) requireTrue(process.argv[3] === api.origin);
  browser = await type.launch();
  const context = await browser.newContext({ baseURL: process.argv[3] });
  context.setDefaultTimeout(10_000);
  const page: Page = await context.newPage();
  // Do not intercept Vite modules such as /src/shared/api/client.ts.
  if (!packaged)
    await page.route(
      (url) => url.pathname.startsWith('/api/'),
      async (route) => {
        const url = new URL(route.request().url());
        const response = await route.fetch({
          url: `${api.origin}${url.pathname}${url.search}`,
          timeout: 15_000,
        });
        await route.fulfill({ response });
      },
    );
  async function login(username: string) {
    const password = readFileSync(
      join(secrets, `dev_${username}_password`),
      'utf8',
    ).replace(/\r?\n$/, '');
    await page.goto('/login');
    await page.getByLabel('Username', { exact: true }).fill(username);
    for (let attempt = 0; attempt < 2; attempt++) {
      await page.getByLabel('Password', { exact: true }).fill(password);
      const response = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/auth/login' &&
          response.request().method() === 'POST',
      );
      await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      const result = await response;
      const status = result.status();
      if (status === 429 && attempt === 0) {
        // Respect the unchanged login limit; never retry incident POSTs.
        const delay = Number(result.headers()['retry-after']);
        requireTrue(Number.isFinite(delay) && delay > 0 && delay <= 60);
        await page.waitForFunction(
          (deadline) =>
            Date.now() >= deadline &&
            !(document.querySelector('button[type="submit"]') as HTMLButtonElement | null)
              ?.disabled,
          Date.now() + delay * 1000,
          { timeout: 70_000 },
        );
        continue;
      }
      requireTrue(status === 200);
      break;
    }
    await page.getByRole('navigation', { name: 'Workspace' }).waitFor();
  }
  async function logout() {
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await page.getByRole('heading', { name: 'Welcome back' }).waitFor();
  }

  check('signing in reporter');
  await login('employee_a');
  const cookie = packaged
    ? (await context.cookies()).find((cookie) => cookie.name === 'cyberguard_session')
    : undefined;
  if (packaged) {
    requireTrue(
      Boolean(
        cookie?.httpOnly &&
        !cookie.secure &&
        cookie.path === '/' &&
        cookie.sameSite === 'Lax',
      ),
    );
    requireTrue(
      !(await page.evaluate(() => document.cookie.includes('cyberguard_session'))),
    );
    await page.goto('/account');
    await page.getByRole('heading', { name: 'You’re signed in.' }).waitFor();
    await page.reload();
    await page.getByRole('heading', { name: 'You’re signed in.' }).waitFor();
    await page.getByRole('link', { name: 'Incidents', exact: true }).click();
  }
  check('submitting report');
  await page.getByRole('link', { name: 'Report an incident', exact: true }).click();
  if (packaged) {
    await page.reload();
    await page.getByRole('heading', { name: 'Report an incident' }).waitFor();
  }
  const title = `Browser report ${randomUUID()}`;
  const description = '  Synthetic browser incident.\n\nŽiadne skutočné údaje. 🔒  ';
  await page.getByLabel('Title', { exact: true }).fill(title);
  await page.getByLabel('Description', { exact: true }).fill(description);
  await page.getByLabel('Affected system', { exact: true }).fill(' \u0085 ');
  const response = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/incidents' &&
      response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Submit incident', exact: true }).click();
  const created = await response;
  requireTrue(created.status() === 201);
  const data: unknown = await created.json();
  const row = (
    data as {
      incident: {
        id: string;
        reporter_id: string;
        status: string;
        description: string;
        affected_system: unknown;
        category: unknown;
        assigned_analyst: unknown;
        severity?: unknown;
      };
    }
  ).incident;
  if (packaged) requireTrue(created.headers()['location'] === `/api/incidents/${row.id}`);
  requireTrue(
    row.status === 'submitted' &&
      row.description === description &&
      row.affected_system === null &&
      row.category === null &&
      row.assigned_analyst === null &&
      !Object.hasOwn(row, 'severity'),
  );
  check('reading saved report');
  await page.getByRole('heading', { name: title, exact: true }).waitFor();
  requireTrue(
    (await page.locator('.incident-description').textContent()) === description,
  );
  requireTrue((await page.getByText('Severity', { exact: true }).count()) === 0);
  requireTrue(
    (await page.evaluate(() => localStorage.length + sessionStorage.length)) === 0,
  );
  check('reloading report');
  await page.reload();
  await page.getByRole('heading', { name: title, exact: true }).waitFor();
  requireTrue(
    (await page.locator('.incident-description').textContent()) === description,
  );
  check('checking reporter list');
  await page.getByRole('link', { name: 'Back to incidents' }).click();
  await page.getByRole('link', { name: title, exact: true }).waitFor();
  await logout();
  if (packaged) {
    const revoked = await context.request.get('/api/auth/me', {
      headers: { Cookie: `cyberguard_session=${cookie?.value ?? ''}` },
    });
    requireTrue(revoked.status() === 401);
  }
  check('checking employee isolation');
  await login('employee_b');
  await page.getByRole('heading', { name: 'No reports yet' }).waitFor();
  requireTrue((await page.getByRole('link', { name: title, exact: true }).count()) === 0);
  await page.goto(`/incidents/${row.id}`);
  await page.getByRole('heading', { name: 'Incident not found' }).waitFor();
  await logout();
  check('checking staff visibility');
  await login('analyst');
  await page.getByRole('link', { name: title, exact: true }).click();
  await page.getByText('Severity', { exact: true }).waitFor();
  requireTrue(
    (await page.locator('.incident-description').textContent()) === description,
  );
  check('signing out');
  await logout();
} catch {
  process.exitCode = checkpoint;
} finally {
  await browser?.close().catch(() => {
    process.exitCode ||= checkpoint;
  });
}
