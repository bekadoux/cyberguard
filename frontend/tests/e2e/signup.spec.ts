import { expect, test } from '@playwright/test';

const error = (code: string) => ({ error: { code, message: 'Safe server message.' } });

test.beforeEach(async ({ page }) => {
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ status: 401, json: error('authentication_required') }),
  );
});

async function fill(page: import('@playwright/test').Page) {
  await page.getByLabel('Username', { exact: true }).fill('NewEmployee');
  await page.getByLabel('Password', { exact: true }).fill('correct horse battery');
  await page.getByLabel('Confirm password').fill('correct horse battery');
}

test('creates an employee without establishing a browser session', async ({ page }) => {
  let count = 0;
  await page.route('**/api/auth/signup', (route) => {
    count++;
    expect(route.request().postDataJSON()).toEqual({
      username: 'NewEmployee',
      password: 'correct horse battery',
    });
    return route.fulfill({
      status: 201,
      json: {
        user: {
          id: 'd1234567-1234-4234-8234-123456789abc',
          username: 'newemployee',
          role: 'employee',
        },
      },
    });
  });
  await page.goto('/signup');
  await fill(page);
  await page.getByRole('button', { name: 'Create employee account' }).click();
  await expect(page.getByRole('heading', { name: 'Account created' })).toBeVisible();
  expect(count).toBe(1);
  expect(await page.evaluate(() => ({ ...localStorage }))).toEqual({});
  await page.getByRole('link', { name: 'Continue to sign in' }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test('handles feature-gated registration (HTTP 404) without showing success', async ({
  page,
}) => {
  await page.route('**/api/auth/signup', (route) =>
    route.fulfill({ status: 404, json: error('not_found') }),
  );
  await page.goto('/signup');
  await fill(page);
  await page.getByRole('button', { name: 'Create employee account' }).click();
  await expect(page.getByRole('alert')).toContainText('Self-registration is not enabled');
  await expect(page.getByRole('heading', { name: 'Account created' })).toHaveCount(0);
});
