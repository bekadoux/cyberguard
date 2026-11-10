import { expect, test } from '@playwright/test';
import { completedAssessment } from '../support/assessments';
import { analyst, detail, employee, first, identity } from '../support/incidents';
import { auditIncidents } from '../support/accessibility';

test.beforeEach(async ({ page }) => {
  await identity(page, analyst);
  await page.route('**/api/incidents/*', (route) =>
    route.fulfill({ json: { incident: { ...detail, severity: null } } }),
  );
});
test('renders stored advisory output without changing confirmed incident fields', async ({
  page,
}) => {
  await page.clock.install();
  let reads = 0;
  await page.route('**/ai/incidents/*/assessment', (route) => {
    reads++;
    return route.fulfill({ json: completedAssessment });
  });
  await page.goto(`/incidents/${first.id}`);
  const panel = page.getByTestId('assessment-panel');
  await expect(panel.getByRole('heading', { name: 'Draft summary' })).toBeVisible();
  await expect(panel).toContainText('Unconfirmed working hypotheses');
  await expect(panel).toContainText('Phishing / social engineering');
  await expect(
    page.locator('.incident-metadata').getByText('Not assessed', { exact: true }),
  ).toHaveCount(2);
  await panel.getByText('Model signals', { exact: true }).click();
  await expect(panel).toContainText('not calibrated risk scores');
  const initialReads = reads;
  expect(initialReads).toBeGreaterThanOrEqual(1);
  await page.clock.fastForward(60_000);
  expect(reads).toBe(initialReads);
});
for (const role of ['employee', 'analyst', 'admin'] as const) {
  test(`applies ${role} assessment access on the reporter's own record`, async ({
    page,
  }) => {
    const user = { ...employee, role };
    await identity(page, user);
    let reads = 0;
    await page.route('**/ai/incidents/*/assessment', (route) => {
      reads++;
      return route.fulfill({ json: completedAssessment });
    });
    await page.goto(`/incidents/${first.id}`);
    await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
    if (role === 'employee') {
      await expect(page.getByTestId('assessment-panel')).toHaveCount(0);
      expect(reads).toBe(0);
    } else {
      await expect(
        page
          .getByTestId('assessment-panel')
          .getByRole('heading', { name: 'Draft summary' }),
      ).toBeVisible();
      expect(reads).toBeGreaterThanOrEqual(1);
    }
  });
}
test('refreshes queued/running/completed states only on explicit request', async ({
  page,
}) => {
  let state = 'queued';
  let reads = 0;
  await page.route('**/ai/incidents/*/assessment', (route) => {
    reads++;
    return route.fulfill({
      json:
        state === 'completed'
          ? completedAssessment
          : { job: { ...completedAssessment.job, status: state }, result: null },
    });
  });
  await page.goto(`/incidents/${first.id}`);
  const panel = page.getByTestId('assessment-panel');
  await expect(panel).toContainText('Waiting for a worker');
  const initialReads = reads;
  state = 'running';
  await panel.getByRole('button', { name: 'Refresh assessment' }).click();
  await expect(panel).toContainText('Assessment is processing');
  state = 'completed';
  await panel.getByRole('button', { name: 'Refresh assessment' }).click();
  await expect(panel.getByRole('heading', { name: 'Draft summary' })).toBeVisible();
  expect(reads).toBe(initialReads + 2);
});
test('labels fake output prominently and renders model text literally', async ({
  page,
}) => {
  const hostile = '<img id="injected-model-output" src=x onerror="alert(1)">';
  await page.route('**/ai/incidents/*/assessment', (route) =>
    route.fulfill({
      json: {
        ...completedAssessment,
        result: {
          ...completedAssessment.result,
          source_kind: 'fake',
          assessment: {
            ...completedAssessment.result.assessment,
            is_fake: true,
            brief: { ...completedAssessment.result.assessment.brief, summary: hostile },
          },
        },
      },
    }),
  );
  await page.goto(`/incidents/${first.id}`);
  const panel = page.getByTestId('assessment-panel');
  await expect(panel).toContainText('Test fixture — not AI output.');
  await expect(panel.getByText(hostile, { exact: true })).toBeVisible();
  await expect(page.locator('#injected-model-output')).toHaveCount(0);
});
for (const status of [403, 404, 503]) {
  test(`keeps the incident visible when the AI service returns ${status}`, async ({
    page,
  }) => {
    await page.route('**/ai/incidents/*/assessment', (route) =>
      route.fulfill({ status, json: { detail: 'sensitive upstream diagnostic' } }),
    );
    await page.goto(`/incidents/${first.id}`);
    await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
    const panel = page.getByTestId('assessment-panel');
    await expect(panel.getByRole('status')).toContainText(
      status === 404
        ? 'No assessment is available'
        : status === 403
          ? 'do not have access'
          : 'temporarily unavailable',
    );
    await expect(panel).not.toContainText('sensitive upstream diagnostic');
  });
}
test('shows terminal failure safely without inventing a retry mutation', async ({
  page,
}) => {
  await page.route('**/ai/incidents/*/assessment', (route) =>
    route.fulfill({
      json: {
        job: {
          ...completedAssessment.job,
          status: 'failed',
          error_code: 'internal_error',
        },
        result: null,
      },
    }),
  );
  await page.goto(`/incidents/${first.id}`);
  const panel = page.getByTestId('assessment-panel');
  await expect(panel).toContainText('could not be completed');
  await expect(panel).not.toContainText('internal_error');
  await expect(panel.getByRole('button')).toHaveCount(1);
});
test('invalidates the current session on an assessment 401', async ({ page }) => {
  await page.route('**/ai/incidents/*/assessment', (route) =>
    route.fulfill({ status: 401, json: { detail: 'expired' } }),
  );
  await page.goto(`/incidents/${first.id}`);
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});
test('discards late assessment content after shared-cookie identity changes', async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/ai/incidents/*/assessment', async (route) => {
    await gate;
    await route.fulfill({ json: completedAssessment }).catch(() => {});
  });
  await page.goto(`/incidents/${first.id}`);
  await expect(page.getByTestId('assessment-panel')).toContainText('Loading assessment');
  await identity(page, employee);
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByTestId('assessment-panel')).toHaveCount(0);
  release();
  await expect(
    page.getByText(completedAssessment.result.assessment.brief.summary, { exact: true }),
  ).toHaveCount(0);
});
for (const colorScheme of ['light', 'dark'] as const) {
  test(`assessment reflows and is accessible in ${colorScheme} mode`, async ({
    page,
    browserName,
    baseURL,
  }) => {
    await page.emulateMedia({ colorScheme });
    await page.route('**/ai/incidents/*/assessment', (route) =>
      route.fulfill({ json: completedAssessment }),
    );
    for (const width of [1280, 375, 320]) {
      await page.setViewportSize({ width, height: 667 });
      await page.goto(`/incidents/${first.id}`);
      await expect(page.getByRole('heading', { name: 'Draft summary' })).toBeVisible();
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth),
      ).toBeLessThanOrEqual(width);
    }
    expect(
      await auditIncidents(browserName, baseURL!, colorScheme, 'assessment'),
    ).toEqual([]);
  });
}
