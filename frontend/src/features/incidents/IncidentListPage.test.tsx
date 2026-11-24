import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router';
import { first } from '../../../tests/support/incidents';
import { useAuth } from '../auth/use-auth';
import { useIncidentList } from './use-incident-list';
import { IncidentListPage } from './IncidentListPage';
import type { AssessmentStatus, IncidentSummary } from './model';

vi.mock('../auth/use-auth', () => ({ useAuth: vi.fn() }));
vi.mock('./use-incident-list', () => ({ useIncidentList: vi.fn() }));

afterEach(() => vi.resetAllMocks());

function show(role: 'employee' | 'analyst' | 'admin', value: AssessmentStatus | null) {
  const refresh = vi.fn();
  vi.mocked(useAuth).mockReturnValue({
    user: { id: first.reporter_id, role, username: 'test' },
  } as ReturnType<typeof useAuth>);
  const incidents: IncidentSummary[] = [
    { ...first, status: 'submitted', severity: null, ai_assessment_status: value },
  ];
  vi.mocked(useIncidentList).mockReturnValue({
    incidents,
    cursor: null,
    phase: 'ready',
    error: null,
    source: 'submitted/phishing/0',
    refresh,
    loadMore: vi.fn(),
  } as ReturnType<typeof useIncidentList>);
  render(
    <MemoryRouter initialEntries={['/incidents?status=submitted&category=phishing']}>
      <IncidentListPage />
    </MemoryRouter>,
  );
  return refresh;
}

describe('staff AI assessment status column', () => {
  it.each([
    ['queued', 'Queued'],
    ['running', 'In progress'],
    ['completed', 'Complete'],
    ['failed', 'Failed'],
    [null, 'No assessment'],
  ] as const)(
    'shows %s as %s for staff, including their own reports',
    (status, label) => {
      show('analyst', status);
      expect(screen.getByRole('columnheader', { name: 'AI assessment' })).toBeVisible();
      expect(screen.getByRole('cell', { name: label })).toBeVisible();
      expect(screen.getByText(/Reported by you/)).toBeVisible();
    },
  );

  it('never renders the AI column for employees', () => {
    show('employee', 'completed');
    expect(
      screen.queryByRole('columnheader', { name: 'AI assessment' }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole('cell', { name: 'Complete' })).not.toBeInTheDocument();
  });

  it('refreshes without clearing the existing status and category filters', async () => {
    const user = userEvent.setup();
    const refresh = show('admin', 'running');
    expect(screen.getByLabelText('Status')).toHaveValue('submitted');
    expect(screen.getByLabelText('Category')).toHaveValue('phishing');
    await user.click(screen.getByRole('button', { name: 'Refresh list' }));
    expect(refresh).toHaveBeenCalledOnce();
    expect(screen.getByLabelText('Status')).toHaveValue('submitted');
    expect(screen.getByLabelText('Category')).toHaveValue('phishing');
  });
});
