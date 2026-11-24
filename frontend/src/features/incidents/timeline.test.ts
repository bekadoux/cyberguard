import { afterEach, describe, expect, it, vi } from 'vitest';
import { first } from '../../../tests/support/incidents';
import { getTimeline, isReporter, parseTimeline } from './timeline';

afterEach(() => vi.unstubAllGlobals());
describe('reporter status timeline', () => {
  it('shows the reporter view for their own reports regardless of role, never other reports', () => {
    expect(isReporter(first.reporter_id, first.reporter_id)).toBe(true);
    expect(isReporter(first.reporter_id, '22222222-2222-4222-8222-222222222222')).toBe(
      false,
    );
    expect(isReporter(first.reporter_id, undefined)).toBe(false);
  });
  it('preserves server order, timestamps and continuation cursor', () => {
    expect(
      parseTimeline({
        events: [
          { status: 'triage', occurred_at: first.updated_at },
          { status: 'submitted', occurred_at: first.created_at },
        ],
        next_cursor: 'opaque',
      }),
    ).toEqual({
      events: [
        { status: 'triage', occurred_at: first.updated_at },
        { status: 'submitted', occurred_at: first.created_at },
      ],
      next_cursor: 'opaque',
    });
  });
  it('rejects unknown statuses and invalid timestamps', () => {
    for (const event of [
      { status: 'secret', occurred_at: first.created_at },
      { status: 'submitted', occurred_at: 'invalid' },
    ])
      expect(() => parseTimeline({ events: [event], next_cursor: null })).toThrow(
        'invalid_response',
      );
  });
  it('calls only the reporter timeline endpoint with an opaque cursor', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ events: [], next_cursor: null })));
    vi.stubGlobal('fetch', fetch);
    await getTimeline(first.id, 'cursor token', new AbortController().signal);
    expect(fetch.mock.calls[0]?.[0]).toContain(`/incidents/${first.id}/timeline?`);
    expect(fetch.mock.calls[0]?.[0]).toContain('cursor=cursor+token');
    expect(fetch.mock.calls[0]?.[0]).not.toContain('/history');
  });
});
