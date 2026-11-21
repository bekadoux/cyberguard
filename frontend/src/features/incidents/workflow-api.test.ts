import { afterEach, describe, expect, it, vi } from 'vitest';
import { adminApi, notificationsApi, workflowApi } from './workflow-api';

const id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
afterEach(() => vi.unstubAllGlobals());

describe('investigation API contracts', () => {
  it('uses PATCH and cookie credentials for human-confirmed edits', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response('{"updated":true}'));
    vi.stubGlobal('fetch', fetcher);
    await workflowApi.fields(id, { category: 'phishing', severity: 'high' });
    expect(fetcher).toHaveBeenCalledWith(
      `/api/incidents/${id}/fields`,
      expect.objectContaining({
        method: 'PATCH',
        credentials: 'include',
        cache: 'no-store',
        body: '{"category":"phishing","severity":"high"}',
      }),
    );
  });
  it('does not claim accepted or uncommitted feedback as saved', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('{}', { status: 202 })),
    );
    await expect(
      workflowApi.feedback(id, {
        job_id: id,
        decision: 'accepted',
        category: 'phishing',
        reason: '',
      }),
    ).rejects.toMatchObject({ code: 'invalid_response' });
  });
  it.each([
    {
      job_id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
      decision: 'accepted',
      category: 'phishing',
      reason: '',
    },
    { job_id: id, decision: 'corrected', category: 'bad', reason: 'Correction' },
    { job_id: id, decision: 'rejected', category: 'phishing', reason: 'Reason' },
    { job_id: id, decision: 'accepted', category: null, reason: '' },
  ])('rejects mismatched jobs and malformed review decisions', async (review) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ review })));
    await expect(workflowApi.review(id, id)).rejects.toMatchObject({
      code: 'invalid_response',
    });
  });
  it('does not offer blocked or ineligible accounts returned by a malformed picker page', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json({
          users: [{ id, username: 'blocked', role: 'analyst', is_blocked: true }],
          next_cursor: null,
        }),
      ),
    );
    await expect(adminApi.eligibleAnalysts('', null)).rejects.toMatchObject({
      code: 'invalid_response',
    });
  });
  it('rejects malformed notifications rather than displaying arbitrary server values', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ notifications: [{ id, incident_id: id, message: 123 }] }),
          ),
        ),
    );
    await expect(notificationsApi.list()).rejects.toMatchObject({
      code: 'invalid_response',
    });
  });
  it('requires a valid UUID for notification read writes', async () => {
    await expect(notificationsApi.read('../other')).rejects.toMatchObject({
      status: 404,
    });
  });
  it('parses safe user DTOs only', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            users: [
              {
                id,
                username: 'alice',
                role: 'analyst',
                is_blocked: false,
                password_hash: 'not returned',
              },
            ],
          }),
        ),
      ),
    );
    await expect(adminApi.users()).resolves.toEqual([
      { id, username: 'alice', role: 'analyst', is_blocked: false },
    ]);
  });
});
