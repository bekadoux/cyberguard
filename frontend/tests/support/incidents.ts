import type { Page } from '@playwright/test';
import type { User } from '../../src/features/auth/api';

export const employee: User = {
  id: '11111111-1111-4111-8111-111111111111',
  username: 'employee_a',
  role: 'employee',
};
export const analyst: User = {
  id: '22222222-2222-4222-8222-222222222222',
  username: 'analyst',
  role: 'analyst',
};
export const first = {
  id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  reporter_id: employee.id,
  title: 'Suspicious message from IT support',
  status: 'submitted',
  category: null,
  assigned_analyst_id: null,
  created_at: '2026-11-09T10:00:00Z',
  updated_at: '2026-11-09T10:00:00Z',
};
export const second = {
  ...first,
  id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
  title: 'Work laptop missing',
  created_at: '2026-11-08T10:00:00Z',
};
export const detail = {
  ...first,
  description:
    'An unexpected email asked me to confirm my company password.\n\nI did not open the link. Žiadne prihlasovacie údaje som neposkytol.',
  affected_system: 'Company email',
  resolution_note: null,
  assigned_analyst: null,
};
export async function identity(page: Page, user: User = employee) {
  // Detail screens now load workflow collections as well as the original report.
  // Keep synthetic identities off real services unless a test overrides these routes.
  for (const [path, collection] of [
    ['/incidents/*/comments', 'comments'],
    ['/incidents/*/history', 'history'],
    ['/admin/users', 'users'],
    ['/admin/audit', 'audit'],
    ['/notifications', 'notifications'],
  ] as const) {
    await page.route(`**/api${path}`, (route) =>
      route.request().method() === 'GET'
        ? route.fulfill({ json: { [collection]: [] } })
        : route.fulfill({
            status: 405,
            json: { error: { code: 'unexpected_mutation' } },
          }),
    );
  }
  await page.route('**/api/incidents/*/timeline?*', (route) =>
    route.fulfill({ json: { events: [], next_cursor: null } }),
  );
  await page.route('**/api/incidents/*/timeline', (route) =>
    route.fulfill({ json: { events: [], next_cursor: null } }),
  );
  await page.route('**/api/admin/users?eligible=analyst&*', (route) =>
    route.fulfill({ json: { users: [], next_cursor: null } }),
  );
  await page.route('**/api/admin/ownership', (route) =>
    route.fulfill({ json: { owner_id: null } }),
  );
  await page.route('**/api/admin/demo-incidents', (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({ json: { enabled: false, remaining: 0 } })
      : route.fulfill({ status: 405, json: { error: { code: 'unexpected_mutation' } } }),
  );
  await page.route('**/api/incidents/*/ai-feedback/*', (route) =>
    route.fulfill({ json: { review: null } }),
  );
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
}
