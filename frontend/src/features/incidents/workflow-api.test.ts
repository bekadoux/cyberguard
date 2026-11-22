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
  it('creates an account without sending a role', async () => {
    const fetcher = vi.fn().mockResolvedValue(Response.json({}, { status: 201 }));
    vi.stubGlobal('fetch', fetcher);
    await adminApi.create('new-user', 'test-only-password');
    expect(fetcher).toHaveBeenCalledWith(
      '/api/admin/users',
      expect.objectContaining({
        method: 'POST',
        body: '{"username":"new-user","password":"test-only-password"}',
      }),
    );
  });
  it('parses the optional owner and rejects invalid identifiers', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(Response.json({ owner_id: id }))
      .mockResolvedValueOnce(Response.json({ owner_id: null }))
      .mockResolvedValueOnce(Response.json({ owner_id: 'invalid' }));
    vi.stubGlobal('fetch', fetcher);
    await expect(adminApi.ownerId()).resolves.toBe(id);
    await expect(adminApi.ownerId()).resolves.toBeNull();
    await expect(adminApi.ownerId()).rejects.toMatchObject({ code: 'invalid_response' });
    expect(fetcher).toHaveBeenCalledWith('/api/admin/ownership', expect.anything());
  });
  it('sends the current password only when supplied', async () => {
    const fetcher = vi.fn().mockImplementation(() => Promise.resolve(Response.json({})));
    vi.stubGlobal('fetch', fetcher);
    await adminApi.password(id, 'test-only-password', '');
    await adminApi.password(id, 'test-only-password', '', ' current password ');
    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      `/api/admin/users/${id}/password`,
      expect.objectContaining({ body: '{"password":"test-only-password","reason":""}' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      `/api/admin/users/${id}/password`,
      expect.objectContaining({
        body: '{"password":"test-only-password","reason":"","current_password":" current password "}',
      }),
    );
  });
});
