import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { first } from '../../../tests/support/incidents';
import { useAuth } from '../auth/use-auth';
import { getTimeline } from './timeline';
import { IncidentTimeline } from './IncidentTimeline';

vi.mock('../auth/use-auth', () => ({ useAuth: vi.fn() }));
vi.mock('./timeline', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./timeline')>()),
  getTimeline: vi.fn(),
}));

afterEach(() => vi.resetAllMocks());

function setup() {
  vi.mocked(useAuth).mockReturnValue({
    pending: false,
    runProtected: (fn: () => Promise<unknown>) => fn(),
  } as ReturnType<typeof useAuth>);
  return render(<IncidentTimeline incidentId={first.id} revision={0} />);
}

describe('reporter timeline UI', () => {
  it('appends cursor pages in server order without staff history calls', async () => {
    const get = vi
      .mocked(getTimeline)
      .mockResolvedValueOnce({
        events: [{ status: 'triage', occurred_at: first.updated_at }],
        next_cursor: 'opaque-page-2',
      })
      .mockResolvedValueOnce({
        events: [{ status: 'submitted', occurred_at: first.created_at }],
        next_cursor: null,
      });
    const user = userEvent.setup();
    setup();
    expect(screen.getByRole('heading', { name: 'Status history' })).toBeVisible();
    await screen.findByRole('button', { name: 'Load more' });
    await user.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(2));
    expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('Triage');
    expect(screen.getAllByRole('listitem')[1]).toHaveTextContent('Submitted');
    expect(get.mock.calls[0]?.[1]).toBeNull();
    expect(get.mock.calls[1]?.[1]).toBe('opaque-page-2');
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument();
  });

  it('shows a recoverable error without hiding already loaded events', async () => {
    vi.mocked(getTimeline)
      .mockResolvedValueOnce({
        events: [{ status: 'triage', occurred_at: first.updated_at }],
        next_cursor: 'retry-cursor',
      })
      .mockRejectedValueOnce(new Error('temporary error'))
      .mockResolvedValueOnce({
        events: [{ status: 'submitted', occurred_at: first.created_at }],
        next_cursor: null,
      });
    const user = userEvent.setup();
    setup();
    await user.click(await screen.findByRole('button', { name: 'Load more' }));
    await screen.findByRole('alert');
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(2));
    expect(vi.mocked(getTimeline).mock.calls[2]?.[1]).toBe('retry-cursor');
  });

  it('retries a failed refresh from the first page instead of the old cursor', async () => {
    const get = vi
      .mocked(getTimeline)
      .mockResolvedValueOnce({
        events: [{ status: 'triage', occurred_at: first.updated_at }],
        next_cursor: 'old-cursor',
      })
      .mockRejectedValueOnce(new Error('temporary error'))
      .mockResolvedValueOnce({
        events: [{ status: 'investigating', occurred_at: first.updated_at }],
        next_cursor: 'new-cursor',
      });
    const user = userEvent.setup();
    const { rerender } = setup();
    await screen.findByRole('button', { name: 'Load more' });
    rerender(<IncidentTimeline incidentId={first.id} revision={1} />);
    await screen.findByRole('alert');
    expect(screen.getByRole('listitem')).toHaveTextContent('Triage');
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() =>
      expect(screen.getByRole('listitem')).toHaveTextContent('Investigating'),
    );
    expect(get.mock.calls.map(([, cursor]) => cursor)).toEqual([null, null, null]);
  });
});
