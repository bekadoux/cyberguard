import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../shared/api/client';
import type { User } from './api';
import { SessionStore } from './session-store';

const user: User = { id: 'user-id', username: 'employee_a', role: 'employee' };
const credentials = { username: 'employee_a', password: 'not-a-real-password' };
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function fixture() {
  const api = {
    me: vi
      .fn<() => Promise<User>>()
      .mockRejectedValue(new ApiError(401, 'authentication_required')),
    login: vi.fn<() => Promise<User>>().mockResolvedValue(user),
    logout: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
  };
  return { api, store: new SessionStore(api) };
}

describe('session state', () => {
  it('restores identity from /me, not browser storage', async () => {
    const { api, store } = fixture();
    api.me.mockResolvedValue(user);
    await store.refresh();
    expect(store.getSnapshot()).toMatchObject({ status: 'authenticated', user });
    expect(localStorage.length).toBe(0);
  });

  it('treats protected 401 as expired/revoked identity', async () => {
    const { store } = fixture();
    await store.login(credentials);
    await store.refresh();
    expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
  });

  it('preserves identity on 503 while marking the session unavailable', async () => {
    const { api, store } = fixture();
    await store.login(credentials);
    api.me.mockRejectedValue(new ApiError(503, 'auth_unavailable'));
    await store.refresh();
    expect(store.getSnapshot()).toMatchObject({ status: 'unavailable', user });
    api.me.mockResolvedValue(user);
    await store.refresh();
    expect(store.getSnapshot()).toMatchObject({
      status: 'authenticated',
      user,
      error: null,
    });
  });

  it('never claims successful logout when persistence fails', async () => {
    const { api, store } = fixture();
    await store.login(credentials);
    api.logout.mockRejectedValueOnce(new ApiError(503, 'auth_unavailable'));
    await expect(store.logout()).rejects.toMatchObject({ status: 503 });
    expect(store.getSnapshot()).toMatchObject({
      status: 'authenticated',
      user,
      pending: false,
    });
    await store.logout();
    expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
  });

  it.each(['success', 'unauthorized'] as const)(
    'ignores a stale %s /me response after login',
    async (result) => {
      const { api, store } = fixture();
      const old = deferred<User>();
      api.me.mockReturnValue(old.promise);
      const refresh = store.refresh();
      await store.login(credentials);
      if (result === 'success') old.resolve({ ...user, username: 'old_account' });
      else old.reject(new ApiError(401, 'authentication_required'));
      await refresh;
      expect(store.getSnapshot()).toMatchObject({ status: 'authenticated', user });
    },
  );

  it('ignores a stale authenticated response after logout', async () => {
    const { api, store } = fixture();
    await store.login(credentials);
    const old = deferred<User>();
    api.me.mockReturnValue(old.promise);
    const refresh = store.refresh();
    await store.logout();
    old.resolve(user);
    await refresh;
    expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
  });

  it('prevents overlapping login/logout and refresh during mutations', async () => {
    const { api, store } = fixture();
    const login = deferred<User>();
    api.login.mockReturnValue(login.promise);
    const pending = store.login(credentials);
    await expect(store.logout()).rejects.toMatchObject({ code: 'auth_pending' });
    await expect(store.login(credentials)).rejects.toMatchObject({
      code: 'auth_pending',
    });
    await store.refresh();
    expect(api.me).not.toHaveBeenCalled();
    expect(api.logout).not.toHaveBeenCalled();
    expect(api.login).toHaveBeenCalledTimes(1);
    login.resolve(user);
    await pending;
  });

  it('retains anonymous state after generic invalid credentials', async () => {
    const { api, store } = fixture();
    await store.refresh();
    api.login.mockRejectedValue(new ApiError(401, 'invalid_credentials'));
    await expect(store.login(credentials)).rejects.toMatchObject({ status: 401 });
    expect(store.getSnapshot()).toMatchObject({
      status: 'anonymous',
      user: null,
      pending: false,
    });
  });

  it('lets the newest session refresh win even if an older response arrives last', async () => {
    const { api, store } = fixture();
    const old = deferred<User>();
    api.me.mockReturnValueOnce(old.promise).mockResolvedValueOnce(user);
    const previous = store.refresh();
    await store.refresh();
    old.reject(new ApiError(401, 'authentication_required'));
    await previous;
    expect(store.getSnapshot()).toMatchObject({ status: 'authenticated', user });
  });

  it('expires the current identity on a protected business-request 401', async () => {
    const { store } = fixture();
    await store.login(credentials);
    await expect(
      store.runProtected(() =>
        Promise.reject(new ApiError(401, 'authentication_required')),
      ),
    ).rejects.toMatchObject({ status: 401 });
    expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
  });

  it('does not claim logout when an incident dependency returns 503', async () => {
    const { store } = fixture();
    await store.login(credentials);
    await expect(
      store.runProtected(() =>
        Promise.reject(new ApiError(503, 'incidents_unavailable')),
      ),
    ).rejects.toMatchObject({ status: 503 });
    expect(store.getSnapshot()).toMatchObject({ status: 'authenticated', user });
  });

  it.each(['success', 'unauthorized'] as const)(
    'discards obsolete protected %s after a new login',
    async (result) => {
      const { store } = fixture();
      await store.login(credentials);
      const response = deferred<string>();
      const request = store
        .runProtected(() => response.promise)
        .catch((error: unknown) => error);
      await store.login(credentials);
      if (result === 'success') response.resolve('old account data');
      else response.reject(new ApiError(401, 'authentication_required'));
      expect(await request).toMatchObject({ name: 'AbortError' });
      expect(store.getSnapshot()).toMatchObject({ status: 'authenticated', user });
    },
  );

  it('does not return already-authorized protected data after logout', async () => {
    const { store } = fixture();
    await store.login(credentials);
    const response = deferred<string>();
    const request = store
      .runProtected(() => response.promise)
      .catch((error: unknown) => error);
    await store.logout();
    response.resolve('old account data');
    expect(await request).toMatchObject({ name: 'AbortError' });
    expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
  });
});
