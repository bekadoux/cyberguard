import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ThemeProvider } from '../../shared/theme/ThemeProvider';
import { ApiError } from '../../shared/api/client';
import { AuthContext } from './auth-context';
import { authApi } from './api';
import { SignupPage } from './SignupPage';
import { SessionStore } from './session-store';

async function setup() {
  const store = new SessionStore({
    me: vi.fn().mockRejectedValue(new ApiError(401, 'authentication_required')),
    login: vi.fn(),
    logout: vi.fn(),
  });
  await store.refresh();
  render(
    <ThemeProvider>
      <AuthContext.Provider value={store}>
        <MemoryRouter initialEntries={['/signup']}>
          <Routes>
            <Route path="/signup" element={<SignupPage />} />
            <Route path="/login" element={<h1>Sign-in route</h1>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </ThemeProvider>,
  );
  return userEvent.setup();
}

async function fill(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Username'), 'NewEmployee');
  await user.type(
    screen.getByLabelText('Password', { exact: true }),
    'correct horse battery',
  );
  await user.type(screen.getByLabelText('Confirm password'), 'correct horse battery');
}

afterEach(() => vi.restoreAllMocks());

describe('registration screen', () => {
  it('creates an employee without implicitly logging in', async () => {
    const signup = vi
      .spyOn(authApi, 'signup')
      .mockResolvedValue({ id: 'id', username: 'newemployee', role: 'employee' });
    const user = await setup();
    await fill(user);
    await user.click(screen.getByRole('button', { name: 'Create employee account' }));
    await screen.findByRole('heading', { name: 'Account created' });
    expect(signup).toHaveBeenCalledWith({
      username: 'NewEmployee',
      password: 'correct horse battery',
    });
    expect(screen.getByText(/does not sign you in automatically/)).toBeVisible();
    await user.click(screen.getByRole('link', { name: 'Continue to sign in' }));
    expect(screen.getByRole('heading', { name: 'Sign-in route' })).toBeVisible();
  });
  it('shows when server registration is disabled and does not claim success', async () => {
    vi.spyOn(authApi, 'signup').mockRejectedValue(new ApiError(404, 'not_found'));
    const user = await setup();
    await fill(user);
    await user.click(screen.getByRole('button', { name: 'Create employee account' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Self-registration is not enabled',
    );
    expect(
      screen.queryByRole('heading', { name: 'Account created' }),
    ).not.toBeInTheDocument();
  });
  it('requires a matching password before sending a request', async () => {
    const signup = vi.spyOn(authApi, 'signup');
    const user = await setup();
    await fill(user);
    await user.clear(screen.getByLabelText('Confirm password'));
    await user.type(screen.getByLabelText('Confirm password'), 'different password');
    await user.click(screen.getByRole('button', { name: 'Create employee account' }));
    expect(screen.getByText('Passwords do not match.')).toBeVisible();
    expect(signup).not.toHaveBeenCalled();
  });
});
