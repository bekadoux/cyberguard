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
export async function identity(page: Page, user: User = employee) {
  await page.route('**/api/auth/me', (route) => route.fulfill({ json: { user } }));
}
