import { expect, test } from '@playwright/test';
import { runLiveIncidents } from '../support/live-incidents';

test.beforeAll(() => {
  if (!process.env.E2E_PACKAGED_ORIGIN || !process.env.E2E_INCIDENT_SECRETS_DIR) {
    throw new Error(
      'E2E_PACKAGED_ORIGIN and E2E_INCIDENT_SECRETS_DIR must identify an explicit disposable packaged fixture.',
    );
  }
});

test('loads the built SPA directly through Caddy without Vite', async ({ page }) => {
  const scripts: string[] = [];
  page.on('request', (request) => {
    if (request.resourceType() === 'script')
      scripts.push(new URL(request.url()).pathname);
  });
  await page.goto('/incidents/new');
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  await expect(page).toHaveURL('/login');
  expect(scripts.some((path) => /^\/assets\/.+\.js$/.test(path))).toBe(true);
  expect(scripts.some((path) => path.startsWith('/src/') || path.includes('@vite'))).toBe(
    false,
  );
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});

test('real same-origin packaged login, creation, reload, isolation and logout', async ({
  browserName,
  baseURL,
}) => {
  test.setTimeout(180_000);
  await runLiveIncidents(
    browserName,
    baseURL!,
    process.env.E2E_PACKAGED_ORIGIN!,
    process.env.E2E_INCIDENT_SECRETS_DIR!,
    true,
    process.env.E2E_TEST_ASSESSMENTS === '1',
  );
});
