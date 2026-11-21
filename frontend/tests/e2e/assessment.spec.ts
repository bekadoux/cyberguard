import { expect, test } from '@playwright/test';
import { completedAssessment } from '../support/assessments';
import { analyst, detail, employee, first, identity } from '../support/incidents';
import { auditIncidents } from '../support/accessibility';
import type { JobReview } from '../../src/features/incidents/workflow-api';

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

test('preserves sibling drafts, patches only changed fields, and reloads the saved job decision', async ({
  page,
}) => {
  let report = {
    ...detail,
    category: null as string | null,
    severity: 'low',
    status: 'triage',
    affected_system: detail.affected_system as string | null,
    assigned_analyst_id: analyst.id,
    assigned_analyst: { id: analyst.id, username: analyst.username },
  };
  let job = completedAssessment.job;
  const reviews = new Map<string, JobReview>();
  const patches: unknown[] = [];
  await page.route(`**/api/incidents/${first.id}`, (r) =>
    r.fulfill({ json: { incident: report } }),
  );
  await page.route('**/ai/incidents/*/assessment', (r) =>
    r.fulfill({ json: { ...completedAssessment, job } }),
  );
  await page.route('**/api/incidents/*/ai-feedback/*', (r) => {
    const id = new URL(r.request().url()).pathname.split('/').at(-1)!;
    return r.fulfill({ json: { review: reviews.get(id) ?? null } });
  });
  await page.route('**/api/incidents/*/ai-feedback', (r) => {
    const review: JobReview = r.request().postDataJSON();
    reviews.set(review.job_id, review);
    report.category = review.category;
    return r.fulfill({ status: 201, json: { recorded: true } });
  });
  await page.route('**/api/incidents/*/fields', (r) => {
    expect(r.request().method()).toBe('PATCH');
    const patch = r.request().postDataJSON();
    patches.push(patch);
    report = { ...report, ...patch };
    return r.fulfill({ json: { updated: true } });
  });
  await page.goto(`/incidents/${first.id}`);
  const save = page.getByRole('button', { name: 'Save fields' });
  await expect(save).toBeDisabled();
  const system = page.getByLabel('Affected system');
  await system.fill('  Company email ');
  await expect(save).toBeDisabled();
  await system.fill('');
  await page.getByLabel('Your message').fill('Unsaved discussion');
  await page.getByLabel('Next action').selectOption('false_positive');
  await page.getByLabel('Reason (required)', { exact: true }).fill('Unsaved transition');
  const reviewForm = page.getByRole('form', { name: 'AI feedback review' });
  await reviewForm.getByLabel('Reason (optional)').fill('Analyst confirmed');
  await page.getByRole('button', { name: 'Refresh assessment' }).click();
  await expect(reviewForm.getByLabel('Reason (optional)')).toHaveValue(
    'Analyst confirmed',
  );
  await reviewForm.getByRole('button', { name: 'Save human decision' }).click();
  const recorded = page.getByRole('region', { name: 'Recorded human review' });
  await expect(recorded).toContainText('Analyst confirmed');
  await expect(page.getByRole('combobox', { name: 'Category', exact: true })).toHaveValue(
    'phishing',
  );
  await expect(system).toHaveValue('');
  await Promise.all([
    page.waitForResponse(
      (r) => new URL(r.url()).pathname === `/api/incidents/${first.id}` && r.ok(),
    ),
    save.click(),
  ]);
  await expect(save).toBeDisabled();
  expect(patches).toEqual([{ affected_system: null }]);
  await expect(page.getByLabel('Your message')).toHaveValue('Unsaved discussion');
  await expect(page.getByLabel('Reason (required)')).toHaveValue('Unsaved transition');
  await page.reload();
  await expect(recorded).toContainText('accepted');
  await expect(recorded).toContainText('Phishing / social engineering');
  await expect(reviewForm).toHaveCount(0);
  await expect(page.getByLabel('Your message')).toHaveValue('');
  job = { ...job, id: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee' };
  await page.getByRole('button', { name: 'Refresh assessment' }).click();
  await expect(reviewForm).toBeVisible();
  await expect(recorded).toHaveCount(0);
});

test('keeps review read failures distinct from an unreviewed job and displays terminal decisions', async ({
  page,
}) => {
  let failed = true;
  await identity(page, { ...analyst, role: 'admin' });
  await page.route(`**/api/incidents/${first.id}`, (r) =>
    r.fulfill({ json: { incident: { ...detail, status: 'closed' } } }),
  );
  await page.route('**/ai/incidents/*/assessment', (r) =>
    r.fulfill({ json: completedAssessment }),
  );
  await page.route('**/api/incidents/*/ai-feedback/*', (r) =>
    failed
      ? r.fulfill({ status: 503, json: { error: { code: 'unavailable' } } })
      : r.fulfill({
          json: {
            review: {
              job_id: completedAssessment.job.id,
              decision: 'corrected',
              category: 'other',
              reason: 'Correction on record',
            },
          },
        }),
  );
  await page.goto(`/incidents/${first.id}`);
  await expect(page.getByText(/Unable to load human review/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save human decision' })).toHaveCount(0);
  failed = false;
  await page.getByRole('button', { name: 'Retry review' }).click();
  await expect(page.getByRole('region', { name: 'Recorded human review' })).toContainText(
    'Correction on record',
  );
  await expect(page.getByRole('button', { name: 'Save human decision' })).toHaveCount(0);
});

test('recovers a concurrent review and keeps it read-only after reassignment', async ({
  page,
}) => {
  let assigned = true;
  let reviewed = false;
  let reads = 0;
  await page.route(`**/api/incidents/${first.id}`, (r) =>
    r.fulfill({
      json: {
        incident: {
          ...detail,
          assigned_analyst_id: assigned ? analyst.id : null,
          assigned_analyst: assigned ? analyst : null,
        },
      },
    }),
  );
  await page.route('**/ai/incidents/*/assessment', (r) =>
    r.fulfill({ json: completedAssessment }),
  );
  await page.route('**/api/incidents/*/ai-feedback/*', (r) => {
    reads++;
    return r.fulfill({
      json: {
        review: reviewed
          ? {
              job_id: completedAssessment.job.id,
              decision: 'rejected',
              category: null,
              reason: 'Independent review',
            }
          : null,
      },
    });
  });
  await page.route('**/api/incidents/*/ai-feedback', (r) => {
    reviewed = true;
    return r.fulfill({ status: 409, json: { error: { code: 'conflict' } } });
  });
  await page.goto(`/incidents/${first.id}`);
  await page.getByRole('button', { name: 'Save human decision' }).click();
  await expect(page.getByText('Reason: Independent review')).toBeVisible();
  const eligibleReads = reads;
  await page.route('**/api/incidents/*/comments', (r) => {
    if (r.request().method() === 'POST') {
      assigned = false;
      return r.fulfill({ status: 201, json: { saved: true } });
    }
    return r.fulfill({ json: { comments: [] } });
  });
  await page.getByLabel('Your message').fill('Public follow-up');
  await page.getByRole('button', { name: 'Post comment' }).click();
  await expect(page.locator('.incident-metadata')).toContainText('Unassigned');
  await expect(page.getByText('Reason: Independent review')).toBeVisible();
  await expect(page.getByRole('form', { name: 'AI feedback review' })).toHaveCount(0);
  expect(reads).toBe(eligibleReads);
});

test('an unassigned analyst can read a saved review but cannot review a new job', async ({
  page,
}) => {
  let job = completedAssessment.job;
  await page.route('**/ai/incidents/*/assessment', (r) =>
    r.fulfill({ json: { ...completedAssessment, job } }),
  );
  await page.route('**/api/incidents/*/ai-feedback/*', (r) =>
    r.fulfill({
      json: {
        review: new URL(r.request().url()).pathname.endsWith(completedAssessment.job.id)
          ? {
              job_id: completedAssessment.job.id,
              decision: 'corrected',
              category: 'other',
              reason: 'Recorded by the assigned analyst',
            }
          : null,
      },
    }),
  );
  await page.goto(`/incidents/${first.id}`);
  const recorded = page.getByRole('region', { name: 'Recorded human review' });
  const form = page.getByRole('form', { name: 'AI feedback review' });
  await expect(recorded).toContainText('Recorded by the assigned analyst');
  await expect(form).toHaveCount(0);
  job = { ...job, id: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee' };
  await Promise.all([
    page.waitForResponse((r) => r.url().endsWith(`/ai-feedback/${job.id}`) && r.ok()),
    page.getByRole('button', { name: 'Refresh assessment' }).click(),
  ]);
  await expect(page.getByText('Loading human review…')).toHaveCount(0);
  await expect(recorded).toHaveCount(0);
  await expect(form).toHaveCount(0);
});
for (const role of ['employee', 'analyst', 'admin'] as const) {
  test(`applies ${role} assessment access on the reporter's own record`, async ({
    page,
  }) => {
    const user = { ...employee, role };
    await identity(page, user);
    let reads = 0;
    let reviewReads = 0;
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.includes('/ai-feedback/')) reviewReads++;
    });
    await page.route('**/ai/incidents/*/assessment', (route) => {
      reads++;
      return route.fulfill({ json: completedAssessment });
    });
    await page.goto(`/incidents/${first.id}`);
    await expect(page.getByRole('heading', { name: first.title })).toBeVisible();
    if (role === 'employee') {
      await expect(page.getByTestId('assessment-panel')).toHaveCount(0);
      expect(reads).toBe(0);
      expect(reviewReads).toBe(0);
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
