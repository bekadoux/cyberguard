import { expect, test, type Page } from '@playwright/test';

const user = {
  id: 'd1234567-1234-4234-8234-123456789abc',
  username: 'employee_a',
  role: 'employee',
};
const error = (code: string) => ({ error: { code, message: 'Safe server message.' } });

async function anonymous(page: Page) {
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ status: 401, json: error('authentication_required') }),
  );
}

async function fill(page: Page) {
  await page.getByLabel('Username', { exact: true }).fill('employee_a');
  await page.getByLabel('Password', { exact: true }).fill('test-only-password');
}

test.beforeEach(async ({ page }) => {
  await anonymous(page);
});

test('validates, supports keyboard sign-in and submits one request', async ({ page }) => {
  let calls = 0;
  await page.route('**/api/auth/login', async (route) => {
    calls++;
    expect(route.request().postDataJSON()).toEqual({
      username: 'employee_a',
      password: 'test-only-password',
    });
    await route.fulfill({ json: { user } });
  });
  await page.goto('/login');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByLabel('Username', { exact: true })).toBeFocused();
  await fill(page);
  await page.getByLabel('Password', { exact: true }).press('Enter');
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  expect(calls).toBe(1);
  expect(
    await page.evaluate(() => ({
      local: { ...localStorage },
      session: { ...sessionStorage },
    })),
  ).toEqual({ local: {}, session: {} });
});

test('toggles password visibility without submitting', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('Password', { exact: true }).fill('private');
  await page.getByRole('button', { name: 'Show password' }).click();
  await expect(page.getByLabel('Password', { exact: true })).toHaveAttribute(
    'type',
    'text',
  );
  await page.getByRole('button', { name: 'Hide password' }).click();
  await expect(page.getByLabel('Password', { exact: true })).toHaveAttribute(
    'type',
    'password',
  );
});

for (const [status, code, message] of [
  [401, 'invalid_credentials', 'Invalid username or password.'],
  [403, 'csrf_rejected', 'This sign-in request was not allowed.'],
  [503, 'auth_unavailable', 'Sign-in is temporarily unavailable.'],
  [503, 'auth_busy', 'Sign-in is busy right now.'],
] as const) {
  test(`handles ${status} ${code} without a false login`, async ({ page }) => {
    await page.route('**/api/auth/login', (route) =>
      route.fulfill({ status, json: error(code) }),
    );
    await page.goto('/login');
    await fill(page);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText(message);
    await expect(page.getByRole('alert')).toBeFocused();
    await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
    await expect(page).toHaveURL(/\/login$/);
  });
}

test('honors rate limiting and re-enables sign-in after the cooldown', async ({
  page,
}) => {
  let calls = 0;
  await page.route('**/api/auth/login', (route) => {
    calls++;
    return route.fulfill({
      status: 429,
      headers: { 'Retry-After': '2' },
      json: error('login_rate_limited'),
    });
  });
  await page.goto('/login');
  await fill(page);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeDisabled();
  await expect(page.getByRole('alert')).toContainText('Try again in');
  expect(calls).toBe(1);
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeEnabled({
    timeout: 5000,
  });
});

test('handles network failure safely', async ({ page }) => {
  await page.route('**/api/auth/login', (route) => route.abort());
  await page.goto('/login');
  await fill(page);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Unable to connect.');
});

test('disables duplicate login submissions while a request is pending', async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let calls = 0;
  await page.route('**/api/auth/login', async (route) => {
    calls++;
    await gate;
    await route.fulfill({ json: { user } });
  });
  await page.goto('/login');
  await fill(page);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Signing in…' })).toBeDisabled();
  await expect(page.getByLabel('Username', { exact: true })).toBeDisabled();
  release();
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  expect(calls).toBe(1);
});

test('restores an existing session and never falsely reports failed logout', async ({
  page,
}) => {
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
  await page.route('**/api/auth/logout', (route) =>
    route.fulfill({ status: 503, json: error('auth_unavailable') }),
  );
  await page.goto('/login');
  await expect(page).toHaveURL('/');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(
    'Your session may still be active.',
  );
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  await page.route('**/api/auth/logout', (route) => route.fulfill({ status: 204 }));
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ status: 401, json: error('authentication_required') }),
  );
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});

test('shows a recoverable session outage rather than assuming logout', async ({
  page,
}) => {
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ status: 503, json: error('auth_unavailable') }),
  );
  await page.goto('/');
  await expect(
    page.getByRole('heading', { name: 'Unable to check your session' }),
  ).toBeVisible();
  await expect(
    page.getByText('This does not mean you have been signed out.', { exact: false }),
  ).toBeVisible();
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
});

test('handles expired identity when the page becomes visible again', async ({ page }) => {
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ status: 401, json: error('authentication_required') }),
  );
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});

test('allows logout through a session-check outage without exposing account content', async ({
  page,
}) => {
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ status: 503, json: error('auth_unavailable') }),
  );
  await page.route('**/api/auth/logout', (route) =>
    route.fulfill({ status: 503, json: error('auth_unavailable') }),
  );
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(
    page.getByRole('heading', { name: 'Unable to check your session' }),
  ).toBeVisible();
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(
    'Your session may still be active.',
  );
  await page.route('**/api/auth/logout', (route) => route.fulfill({ status: 204 }));
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});

test('supports system, dark and light themes with persistent preference', async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/login');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.getByLabel('Color theme').selectOption('light');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.reload();
  await expect(page.getByLabel('Color theme')).toHaveValue('light');
  await page.getByLabel('Color theme').selectOption('system');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByLabel('Color theme').selectOption('dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

test('fits a narrow mobile viewport and 200% equivalent zoom without horizontal scrolling', async ({
  page,
}) => {
  for (const width of [320, 375, 720]) {
    await page.setViewportSize({ width, height: 812 });
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    ).toBe(true);
    await expect(
      page.getByRole('button', { name: 'Sign in', exact: true }),
    ).toBeInViewport();
  }
});

test('synchronizes a theme change made in another tab', async ({ page, context }) => {
  await page.goto('/login');
  const other = await context.newPage();
  await anonymous(other);
  await other.goto('/login');
  await other.getByLabel('Color theme').selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByLabel('Color theme')).toHaveValue('dark');
  await other.close();
});

test('respects reduced motion while restoring a session', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('**/api/auth/me', () => {});
  await page.goto('/login');
  await expect(
    page.getByRole('heading', { name: 'Checking your session' }),
  ).toBeVisible();
  await expect(page.locator('.spinner')).toHaveCSS('animation-name', 'none');
});

test('works when preference storage is blocked', async ({ page }) => {
  await page.addInitScript(() => {
    Storage.prototype.setItem = () => {
      throw new DOMException('Blocked', 'SecurityError');
    };
    Storage.prototype.getItem = () => {
      throw new DOMException('Blocked', 'SecurityError');
    };
  });
  await page.goto('/login');
  await page.getByLabel('Color theme').selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});
