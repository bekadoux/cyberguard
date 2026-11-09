import { expect, test } from '@playwright/test';
import { analyst, detail, employee, first, identity, second } from '../support/incidents';
import { auditIncidents } from '../support/accessibility';

test.beforeEach(async ({ page }) => {
  await identity(page);
  await page.route('**/ai/incidents/*/assessment', (route) =>
    route.fulfill({ status: 503, json: { detail: 'Synthetic assessment outage' } }),
  );
});

test('lands on personal incidents, preserves nulls and offers account navigation', async ({
  page,
}) => {
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({ json: { incidents: [first], next_cursor: null } }),
  );
  await page.goto('/');
  await expect(page).toHaveURL('/incidents');
  await expect(page.getByRole('heading', { name: 'My incidents' })).toBeVisible();
  await expect(page.getByText(first.title)).toBeVisible();
  await expect(page.getByText('Not assessed', { exact: true })).toBeVisible();
  await expect(page.getByRole('columnheader', { name: 'Severity' })).toHaveCount(0);
  await expect(page.getByText('1 incident loaded · Newest first')).toBeVisible();
  await page.getByRole('link', { name: 'Account', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'You’re signed in.' })).toBeVisible();
});

test('uses opaque cursors and restarts traversal when filters change', async ({
  page,
}) => {
  const queries: string[] = [];
  await page.route('**/api/incidents?*', (route) => {
    const url = new URL(route.request().url());
    queries.push(url.search);
    return route.fulfill({
      json: {
        incidents: url.searchParams.has('cursor') ? [second] : [first],
        next_cursor: url.searchParams.has('cursor') ? null : 'opaque-cursor',
      },
    });
  });
  await page.goto('/incidents');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText(second.title)).toBeVisible();
  expect(queries.at(-1)).toContain('cursor=opaque-cursor');
  await page.getByLabel('Status', { exact: true }).selectOption('submitted');
  await expect(page.getByText(second.title)).toHaveCount(0);
  await expect.poll(() => queries.at(-1)).toContain('status=submitted');
  expect(queries.at(-1)).not.toContain('cursor');
  await expect(
    page.getByRole('button', { name: 'Load more', exact: true }),
  ).toBeEnabled();
  await page.getByLabel('Category', { exact: true }).selectOption('phishing');
  await expect.poll(() => queries.at(-1)).toContain('category=phishing');
  await page.getByRole('button', { name: 'Clear filters' }).click();
  await expect(page).toHaveURL('/incidents');
});

test('ignores an obsolete filter response and clears previous rows while loading', async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/incidents?*', async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get('status') === 'submitted') {
      await gate;
      await route
        .fulfill({ json: { incidents: [first], next_cursor: null } })
        .catch(() => {});
    } else await route.fulfill({ json: { incidents: [second], next_cursor: null } });
  });
  await page.goto('/incidents');
  await expect(page.getByText(second.title)).toBeVisible();
  await page.getByLabel('Status', { exact: true }).selectOption('submitted');
  await expect(page.getByText(second.title)).toHaveCount(0);
  await page.getByLabel('Status', { exact: true }).selectOption('resolved');
  await expect(page.getByText(second.title)).toBeVisible();
  release();
  await expect(page.getByText(first.title)).toHaveCount(0);
});

test('keeps already-loaded records after a pagination outage and allows retry', async ({
  page,
}) => {
  let fail = true;
  await page.route('**/api/incidents?*', (route) => {
    if (new URL(route.request().url()).searchParams.has('cursor'))
      return fail
        ? route.fulfill({
            status: 503,
            json: { error: { code: 'incidents_unavailable' } },
          })
        : route.fulfill({ json: { incidents: [second], next_cursor: null } });
    return route.fulfill({ json: { incidents: [first], next_cursor: 'next' } });
  });
  await page.goto('/incidents');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('temporarily unavailable');
  await expect(page.getByText(first.title)).toBeVisible();
  fail = false;
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText(second.title)).toBeVisible();
});

test('rejects unknown filters instead of silently requesting an unrestricted queue', async ({
  page,
}) => {
  let requests = 0;
  await page.route('**/api/incidents?*', (route) => {
    requests++;
    return route.fulfill({ json: { incidents: [], next_cursor: null } });
  });
  await page.goto('/incidents?severity=high');
  await expect(page.getByRole('heading', { name: 'Unsupported filters' })).toBeVisible();
  expect(requests).toBe(0);
  await page.getByRole('button', { name: 'Clear filters' }).click();
  await expect(page.getByRole('heading', { name: 'No reports yet' })).toBeVisible();
});

test('shows staff a read-only queue with the stricter own-report projection', async ({
  page,
}) => {
  await identity(page, analyst);
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({
      json: {
        incidents: [
          { ...first, severity: 'high' },
          { ...second, reporter_id: analyst.id, severity: 'critical' },
        ],
        next_cursor: null,
      },
    }),
  );
  await page.goto('/incidents');
  await expect(page.getByRole('heading', { name: 'Incident queue' })).toBeVisible();
  await expect(page.getByText('High', { exact: true })).toBeVisible();
  await expect(page.getByText('Reporter view', { exact: true })).toBeVisible();
  await expect(page.getByText('Critical', { exact: true })).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: /assign|classify|resolve/i }),
  ).toHaveCount(0);
});

test('treats incident 401 as expired identity, but 503 as a retryable feature failure', async ({
  page,
}) => {
  let status = 503;
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({
      status,
      json: {
        error: {
          code: status === 401 ? 'authentication_required' : 'incidents_unavailable',
        },
      },
    }),
  );
  await page.goto('/incidents');
  await expect(
    page.getByRole('heading', { name: 'Unable to load incidents' }),
  ).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible();
  status = 401;
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});

test('discards previous account rows when a shared-cookie identity changes', async ({
  page,
}) => {
  let user = employee;
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({
      json: { incidents: user.id === employee.id ? [first] : [], next_cursor: null },
    }),
  );
  await page.goto('/incidents');
  await expect(page.getByText(first.title)).toBeVisible();
  user = {
    ...employee,
    id: '33333333-3333-4333-8333-333333333333',
    username: 'employee_b',
  };
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByRole('heading', { name: 'No reports yet' })).toBeVisible();
  await expect(page.getByText(first.title)).toHaveCount(0);
});

test('recovers a canceled list read after logout fails without reusing its late data', async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let calls = 0;
  let signingOut = false;
  await page.route('**/api/incidents?*', async (route) => {
    calls++;
    if (!signingOut) {
      await gate;
      await route
        .fulfill({ json: { incidents: [first], next_cursor: null } })
        .catch(() => {});
    } else await route.fulfill({ json: { incidents: [second], next_cursor: null } });
  });
  await page.route('**/api/auth/logout', (route) => {
    signingOut = true;
    return route.fulfill({ status: 503, json: { error: { code: 'auth_unavailable' } } });
  });
  await page.goto('/incidents');
  await expect.poll(() => calls).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(
    'Your session may still be active.',
  );
  await expect(page.getByText(second.title)).toBeVisible();
  release();
  await expect(page.getByText(first.title)).toHaveCount(0);
});

for (const colorScheme of ['light', 'dark'] as const) {
  test(`list is accessible and has no page-level horizontal overflow in ${colorScheme} mode`, async ({
    page,
    browserName,
    baseURL,
  }) => {
    await page.emulateMedia({ colorScheme });
    await page.route('**/api/incidents?*', (route) =>
      route.fulfill({
        json: {
          incidents: [{ ...first, title: 'A very long incident title '.repeat(7) }],
          next_cursor: null,
        },
      }),
    );
    for (const [width, height] of [
      [1280, 720],
      [375, 667],
      [320, 568],
    ] as const) {
      await test.step(`${width} × ${height}`, async () => {
        await page.setViewportSize({ width, height });
        await page.goto('/incidents');
        await expect(page.getByText('1 incident loaded · Newest first')).toBeVisible();
        expect(
          await page.evaluate(() => document.documentElement.scrollWidth),
        ).toBeLessThanOrEqual(width);
      });
    }
    expect(await auditIncidents(browserName, baseURL!, colorScheme)).toEqual([]);
  });
}

test('opens read-only detail and returns to the same list filters', async ({ page }) => {
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({ json: { incidents: [first], next_cursor: null } }),
  );
  await page.route(`**/api/incidents/${first.id}`, (route) => {
    expect(new URL(route.request().url()).search).toBe('');
    return route.fulfill({ json: { incident: detail } });
  });
  await page.goto('/incidents?status=submitted');
  await page.getByRole('link', { name: first.title }).click();
  await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
  expect(await page.locator('.incident-description').textContent()).toBe(
    detail.description,
  );
  await expect(page.getByText('Unassigned', { exact: true })).toBeVisible();
  await expect(page.getByText('No resolution has been recorded.')).toBeVisible();
  await expect(page.getByText('Severity', { exact: true })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
  await page.getByRole('link', { name: 'Back to incidents' }).click();
  await expect(page).toHaveURL('/incidents?status=submitted');
});

for (const role of ['analyst', 'admin'] as const) {
  test(`respects ${role} professional and own-reporter projections`, async ({ page }) => {
    const user = { ...analyst, role };
    await identity(page, user);
    await page.route('**/api/incidents/*', (route) =>
      route.fulfill({
        json: {
          incident: new URL(route.request().url()).pathname.endsWith(first.id)
            ? {
                ...detail,
                severity: 'high',
                resolution_note: 'Full reporter-facing outcome.',
                assigned_analyst_id: analyst.id,
                assigned_analyst: { id: analyst.id, username: null },
              }
            : { ...detail, id: second.id, reporter_id: user.id, severity: 'critical' },
        },
      }),
    );
    await page.goto(`/incidents/${first.id}`);
    await expect(page.getByText('High', { exact: true })).toBeVisible();
    await expect(
      page.getByText('Analyst account unavailable', { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText('Full reporter-facing outcome.', { exact: true }),
    ).toBeVisible();
    await page.goto(`/incidents/${second.id}`);
    await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
    await expect(page.getByText('Severity', { exact: true })).toHaveCount(0);
    await expect(page.getByText('Critical', { exact: true })).toHaveCount(0);
  });
}

test('uses the same concealed not-found state for invalid and inaccessible IDs', async ({
  page,
}) => {
  await page.route('**/api/incidents/*', (route) =>
    route.fulfill({ status: 404, json: { error: { code: 'not_found' } } }),
  );
  for (const id of ['not-a-uuid', first.id]) {
    await page.goto(`/incidents/${id}`);
    await expect(page.getByRole('heading', { name: 'Incident not found' })).toBeVisible();
    await expect(
      page.getByText('This report does not exist or is not available to you.'),
    ).toBeVisible();
    await expect(page.getByText(detail.description, { exact: true })).toHaveCount(0);
  }
});

test('does not partially return a report on dependency failure and supports retry', async ({
  page,
}) => {
  let unavailable = true;
  await page.route('**/api/incidents/*', (route) =>
    unavailable
      ? route.fulfill({ status: 503, json: { error: { code: 'incidents_unavailable' } } })
      : route.fulfill({ json: { incident: detail } }),
  );
  await page.goto(`/incidents/${first.id}`);
  await expect(
    page.getByRole('heading', { name: 'Unable to load incident' }),
  ).toBeVisible();
  await expect(page.locator('.incident-description')).toHaveCount(0);
  unavailable = false;
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
});

test('renders hostile report text literally rather than as markup', async ({ page }) => {
  const description =
    '<img id="injected-content" src=x onerror="alert(1)">\n\n  Original whitespace 🔒  ';
  await page.route('**/api/incidents/*', (route) =>
    route.fulfill({ json: { incident: { ...detail, description } } }),
  );
  await page.goto(`/incidents/${first.id}`);
  await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
  expect(await page.locator('.incident-description').textContent()).toBe(description);
  await expect(page.locator('#injected-content')).toHaveCount(0);
});

test('does not display an obsolete detail after navigating to another report', async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({ json: { incidents: [first, second], next_cursor: null } }),
  );
  await page.route('**/api/incidents/*', async (route) => {
    if (route.request().url().endsWith(first.id)) {
      await gate;
      await route.fulfill({ json: { incident: detail } }).catch(() => {});
    } else
      await route.fulfill({
        json: {
          incident: { ...detail, ...second, description: 'Second report details' },
        },
      });
  });
  await page.goto('/incidents');
  await page.getByRole('link', { name: first.title }).click();
  await page.getByRole('link', { name: 'Back to incidents' }).click();
  await page.getByRole('link', { name: second.title }).click();
  await expect(page.getByRole('heading', { name: second.title })).toBeVisible();
  release();
  await expect(page.getByRole('heading', { name: first.title })).toHaveCount(0);
  await expect(page.locator('.incident-description')).toHaveText('Second report details');
});

for (const colorScheme of ['light', 'dark'] as const) {
  test(`detail is accessible and reflows in ${colorScheme} mode`, async ({
    page,
    browserName,
    baseURL,
  }) => {
    await page.emulateMedia({ colorScheme });
    await page.route('**/api/incidents/*', (route) =>
      route.fulfill({
        json: {
          incident: {
            ...detail,
            title: 'X'.repeat(200),
            description: 'Ž🔒'.repeat(5000),
          },
        },
      }),
    );
    for (const width of [1280, 375, 320]) {
      await page.setViewportSize({ width, height: 667 });
      await page.goto(`/incidents/${first.id}`);
      await expect(page.getByRole('heading', { name: 'X'.repeat(200) })).toBeVisible();
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth),
      ).toBeLessThanOrEqual(width);
      await page
        .getByRole('heading', { name: 'Incident information' })
        .scrollIntoViewIfNeeded();
      await expect(
        page.getByRole('heading', { name: 'Incident information' }),
      ).toBeInViewport();
    }
    expect(await auditIncidents(browserName, baseURL!, colorScheme, 'detail')).toEqual(
      [],
    );
  });
}
