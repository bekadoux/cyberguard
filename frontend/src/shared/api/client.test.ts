import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, request } from './client';

afterEach(() => vi.unstubAllGlobals());

describe('API client', () => {
  it('uses cookie credentials and no-store with no bearer token', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ user: { id: 'user' } })));
    vi.stubGlobal('fetch', fetcher);
    await request('/auth/login', {
      method: 'POST',
      body: { username: 'employee', password: ' untouched ' },
    });
    expect(fetcher).toHaveBeenCalledWith(
      '/api/auth/login',
      expect.objectContaining({
        credentials: 'include',
        cache: 'no-store',
        method: 'POST',
        body: '{"username":"employee","password":" untouched "}',
      }),
    );
    const headers = fetcher.mock.calls[0]?.[1].headers as Headers;
    expect(headers.get('Content-Type')).toBe('application/json');
    expect(headers.has('Authorization')).toBe(false);
  });

  it('accepts an empty logout response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );
    await expect(request('/auth/logout', { method: 'POST' })).resolves.toBeUndefined();
  });

  it('extracts safe codes, request IDs and retry deadlines, not raw error messages', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: { code: 'login_rate_limited', message: 'private diagnostics' },
          }),
          {
            status: 429,
            headers: { 'Retry-After': '60', 'X-Request-ID': 'request-id' },
          },
        ),
      ),
    );
    const before = Date.now();
    const error = await request('/auth/login').catch((error: unknown) => error);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 429,
      code: 'login_rate_limited',
      requestId: 'request-id',
      message: 'login_rate_limited',
    });
    expect((error as ApiError).retryAt).toBeGreaterThanOrEqual(before + 60_000);
  });

  it('handles non-JSON gateway failures', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('Bad gateway', { status: 502 })),
    );
    await expect(request('/auth/me')).rejects.toMatchObject({
      status: 502,
      code: 'request_failed',
    });
  });

  it('handles network loss without exposing request contents', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockRejectedValue(new Error('sensitive network diagnostic')),
    );
    await expect(request('/auth/me')).rejects.toMatchObject({
      status: 0,
      code: 'network_unavailable',
    });
  });

  it('rejects a malformed success response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not json')));
    await expect(request('/auth/me')).rejects.toMatchObject({ code: 'invalid_response' });
  });

  it('supports HTTP-date Retry-After headers', () => {
    const date = new Date(Date.now() + 60_000).toUTCString();
    expect(
      new ApiError(503, 'auth_busy', new Headers({ 'Retry-After': date })).retryAt,
    ).toBe(Date.parse(date));
  });
});
