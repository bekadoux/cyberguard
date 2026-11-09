import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { ThemeProvider } from '../../shared/theme/ThemeProvider';
import { AuthContext } from './auth-context';
import { LoginPage } from './LoginPage';
import { RequireSession } from './RequireSession';
import { SessionStore } from './session-store';

const user = { id: 'user-id', username: 'employee_a', role: 'employee' as const };

async function setup(restoreIdentity = true) {
  const api = {
    me: vi.fn().mockRejectedValue(new ApiError(503, 'auth_unavailable')),
    login: vi.fn().mockResolvedValue(user),
    logout: vi.fn().mockResolvedValue(undefined),
  };
  const store = new SessionStore(api);
  if (restoreIdentity) await store.login({ username: 'employee_a', password: 'test' });
  await store.refresh();
  render(
    <ThemeProvider>
      <AuthContext.Provider value={store}>
        <MemoryRouter>
          <Routes>
            <Route element={<RequireSession />}>
              <Route index element={<h1>Protected account content</h1>} />
            </Route>
            <Route path="/login" element={<LoginPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </ThemeProvider>,
  );
  return { api, store, interaction: userEvent.setup() };
}

describe('session outage sign-out', () => {
  it.each([true, false])(
    'allows logout while /me fails, with restored identity %s',
    async (restoreIdentity) => {
      const { api, store, interaction } = await setup(restoreIdentity);
      expect(screen.queryByText('Protected account content')).not.toBeInTheDocument();
      await interaction.click(screen.getByRole('button', { name: 'Sign out' }));
      expect(api.logout).toHaveBeenCalledOnce();
      expect(await screen.findByRole('heading', { name: 'Welcome back' })).toBeVisible();
      expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
    },
  );

  it('keeps access gated and offers retry when logout fails', async () => {
    const { api, store, interaction } = await setup();
    api.logout.mockRejectedValueOnce(new ApiError(503, 'auth_unavailable'));
    await interaction.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Your session may still be active.',
    );
    expect(store.getSnapshot()).toMatchObject({ status: 'unavailable', user });
    expect(screen.queryByText('Protected account content')).not.toBeInTheDocument();
    await interaction.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('heading', { name: 'Welcome back' })).toBeVisible();
  });

  it('disables retry and duplicate logout while sign-out is pending', async () => {
    const { api, interaction } = await setup();
    let finish!: () => void;
    api.logout.mockReturnValueOnce(new Promise<void>((resolve) => (finish = resolve)));
    await interaction.dblClick(screen.getByRole('button', { name: 'Sign out' }));
    expect(screen.getByRole('button', { name: 'Signing out…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeDisabled();
    expect(api.logout).toHaveBeenCalledOnce();
    await act(async () => finish());
    expect(await screen.findByRole('heading', { name: 'Welcome back' })).toBeVisible();
  });

  it('keeps logout available during rechecking and ignores the obsolete refresh', async () => {
    const { api, store, interaction } = await setup();
    let finish!: (value: typeof user) => void;
    api.me.mockReturnValueOnce(new Promise<typeof user>((resolve) => (finish = resolve)));
    await interaction.click(screen.getByRole('button', { name: 'Try again' }));
    expect(screen.getByRole('heading', { name: 'Checking your session' })).toBeVisible();
    await interaction.click(screen.getByRole('button', { name: 'Sign out' }));
    await act(async () => finish(user));
    expect(store.getSnapshot()).toMatchObject({ status: 'anonymous', user: null });
    expect(await screen.findByRole('heading', { name: 'Welcome back' })).toBeVisible();
  });

  it('restores session retry when logout fails after canceling a recheck', async () => {
    const { api, store, interaction } = await setup();
    let finish!: (value: typeof user) => void;
    api.me.mockReturnValueOnce(new Promise<typeof user>((resolve) => (finish = resolve)));
    api.logout.mockRejectedValueOnce(new ApiError(503, 'auth_unavailable'));
    await interaction.click(screen.getByRole('button', { name: 'Try again' }));
    await interaction.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Your session may still be active.',
    );
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
    await act(async () => finish(user));
    expect(store.getSnapshot()).toMatchObject({ status: 'unavailable', user });
    api.me.mockResolvedValueOnce(user);
    await interaction.click(screen.getByRole('button', { name: 'Try again' }));
    expect(
      await screen.findByRole('heading', { name: 'Protected account content' }),
    ).toBeVisible();
  });
});
