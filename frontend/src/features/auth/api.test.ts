import { afterEach, describe, expect, it, vi } from 'vitest';
import { authApi } from './api';

afterEach(() => vi.unstubAllGlobals());

describe('authentication API contract', () => {
  it('projects only safe identity fields even if the response contains extras', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            user: {
              id: 'user-id',
              username: 'employee_a',
              role: 'employee',
              password_hash: 'must-not-keep',
              auth_version: 3,
            },
          }),
        ),
      ),
    );
    await expect(authApi.me()).resolves.toEqual({
      id: 'user-id',
      username: 'employee_a',
      role: 'employee',
    });
  });

  it.each([
    { user: { id: 'user-id', username: 'employee_a', role: 'superuser' } },
    { user: { id: 'user-id', role: 'employee' } },
    null,
  ])('rejects an unexpected identity response', async (body) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body))));
    await expect(authApi.me()).rejects.toMatchObject({ code: 'invalid_response' });
  });

  it('requires the contracted empty success response before confirming logout', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}')));
    await expect(authApi.logout()).rejects.toMatchObject({ code: 'invalid_response' });
  });
});
