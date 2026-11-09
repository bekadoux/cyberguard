import { expect, test } from '@playwright/test';
import { auditIncidents } from '../support/accessibility';
import { detail, first, identity } from '../support/incidents';

test.beforeEach(async ({ page }) => {
  await identity(page);
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({ json: { incidents: [], next_cursor: null } }),
  );
});
async function report(page: import('@playwright/test').Page) {
  await page.getByLabel('Title', { exact: true }).fill('  Suspicious message 🔒  ');
  await page
    .getByLabel('Description', { exact: true })
    .fill('  Unexpected message.\n\nŽiadne údaje som neposkytol.  ');
}

test('navigates from the list, submits original allowed fields once and opens the saved record', async ({
  page,
}) => {
  let requests = 0;
  let saved = { ...detail };
  await page.route('**/api/incidents', (route) => {
    requests++;
    const body = route.request().postDataJSON();
    expect(body).toEqual({
      title: '  Suspicious message 🔒  ',
      description: '  Unexpected message.\n\nŽiadne údaje som neposkytol.  ',
      affected_system: '  Company email  ',
    });
    saved = { ...detail, ...body };
    return route.fulfill({ status: 201, json: { incident: saved } });
  });
  await page.route(`**/api/incidents/${first.id}`, (route) =>
    route.fulfill({ json: { incident: saved } }),
  );
  await page.goto('/incidents');
  await page.getByRole('link', { name: 'Report an incident', exact: true }).click();
  await report(page);
  await page.getByLabel('Affected system', { exact: true }).fill('  Company email  ');
  await page.getByRole('button', { name: 'Submit incident', exact: true }).click();
  await expect(
    page.getByText('Incident submitted. Your report has been saved.'),
  ).toBeVisible();
  expect(await page.locator('.incident-description').textContent()).toBe(
    saved.description,
  );
  expect(requests).toBe(1);
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
});

test('validates required text and code-point limits without sending a request', async ({
  page,
}) => {
  let requests = 0;
  await page.route('**/api/incidents', (route) => {
    requests++;
    return route.fulfill({ status: 400, json: { error: { code: 'invalid_request' } } });
  });
  await page.goto('/incidents/new');
  await page.getByRole('button', { name: 'Submit incident' }).click();
  await expect(page.getByLabel('Title', { exact: true })).toBeFocused();
  await expect(page.getByText('Enter a title with non-whitespace text.')).toBeVisible();
  await page.getByLabel('Title', { exact: true }).fill('🔒'.repeat(201));
  await page.getByLabel('Description', { exact: true }).fill('Details');
  await expect(page.getByText('Use at most 200 Unicode characters.')).toBeVisible();
  expect(requests).toBe(0);
  await page.getByLabel('Title', { exact: true }).fill('🔒'.repeat(200));
  await expect(page.getByText('200 / 200 characters', { exact: true })).toBeVisible();
  await expect(page.getByText('Use at most 200 Unicode characters.')).toHaveCount(0);
});

test('prevents duplicate POSTs while pending and ignores success after leaving the form', async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let requests = 0;
  await page.route('**/api/incidents', async (route) => {
    requests++;
    await gate;
    await route.fulfill({ status: 201, json: { incident: detail } }).catch(() => {});
  });
  await page.goto('/incidents/new');
  await report(page);
  await page.getByRole('button', { name: 'Submit incident' }).click();
  await expect(page.getByRole('button', { name: 'Submitting…' })).toBeDisabled();
  // Even synthetic/implicit submit events bypassing the disabled button cannot POST again.
  await page.locator('.report-form').evaluate((form) => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await expect(page.getByLabel('Title', { exact: true })).toBeDisabled();
  await page.getByRole('link', { name: 'Cancel', exact: true }).click();
  release();
  await expect(page.getByRole('heading', { name: 'My incidents' })).toBeVisible();
  expect(requests).toBe(1);
});

for (const status of [400, 403, 413, 415]) {
  test(`retains entered text on a definitive ${status} rejection`, async ({ page }) => {
    await page.route('**/api/incidents', (route) =>
      route.fulfill({
        status,
        json: {
          error: { code: 'request_failed', message: 'sensitive server diagnostics' },
        },
      }),
    );
    await page.goto('/incidents/new');
    await report(page);
    await page.getByRole('button', { name: 'Submit incident' }).click();
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page.getByRole('alert')).toBeFocused();
    await expect(page.getByRole('alert')).not.toContainText(
      'sensitive server diagnostics',
    );
    await expect(page.getByLabel('Title', { exact: true })).toHaveValue(
      '  Suspicious message 🔒  ',
    );
    await expect(page.getByRole('button', { name: 'Submit incident' })).toBeEnabled();
  });
}

for (const failure of ['network', '503', 'malformed success'] as const) {
  test(`does not automatically resubmit an ambiguous ${failure} result`, async ({
    page,
  }) => {
    let requests = 0;
    await page.route('**/api/incidents', (route) => {
      requests++;
      if (failure === 'network') return route.abort();
      return failure === '503'
        ? route.fulfill({
            status: 503,
            json: { error: { code: 'incidents_unavailable' } },
          })
        : route.fulfill({ status: 201, json: { incident: { id: first.id } } });
    });
    await page.goto('/incidents/new');
    await report(page);
    await page.getByRole('button', { name: 'Submit incident' }).click();
    await expect(page.getByRole('alert')).toContainText(
      'could not confirm whether your report was saved',
    );
    await expect(page.getByRole('button', { name: 'Submit incident' })).toBeDisabled();
    expect(requests).toBe(1);
    await page
      .getByLabel('I checked the list and want to submit this report again.')
      .check();
    await expect(page.getByRole('button', { name: 'Submit incident' })).toBeEnabled();
    expect(requests).toBe(1);
  });
}

test('expires the session on a submission 401 without keeping the report in browser storage', async ({
  page,
}) => {
  await page.route('**/api/incidents', (route) =>
    route.fulfill({ status: 401, json: { error: { code: 'authentication_required' } } }),
  );
  await page.goto('/incidents/new');
  await report(page);
  await page.getByRole('button', { name: 'Submit incident' }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
});

for (const colorScheme of ['light', 'dark'] as const) {
  test(`submission is accessible and reflows in ${colorScheme} mode`, async ({
    page,
    browserName,
    baseURL,
  }) => {
    await page.emulateMedia({ colorScheme });
    for (const width of [1280, 375, 320]) {
      await page.setViewportSize({ width, height: 667 });
      await page.goto('/incidents/new');
      await expect(
        page.getByRole('heading', { name: 'Report an incident' }),
      ).toBeVisible();
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth),
      ).toBeLessThanOrEqual(width);
      await page
        .getByRole('button', { name: 'Submit incident' })
        .scrollIntoViewIfNeeded();
      await expect(
        page.getByRole('button', { name: 'Submit incident' }),
      ).toBeInViewport();
    }
    expect(await auditIncidents(browserName, baseURL!, colorScheme, 'new')).toEqual([]);
  });
}
