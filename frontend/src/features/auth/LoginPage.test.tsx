import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../shared/api/client';
import { ThemeProvider } from '../../shared/theme/ThemeProvider';
import { AuthContext } from './auth-context';
import { LoginPage } from './LoginPage';
import { SessionStore } from './session-store';

async function setup(failure?: ApiError) {
  const api = {
    me: vi.fn().mockRejectedValue(new ApiError(401, 'authentication_required')),
    login: failure
      ? vi.fn().mockRejectedValue(failure)
      : vi.fn().mockResolvedValue({ id: 'id', username: 'employee_a', role: 'employee' }),
    logout: vi.fn(),
  };
  const store = new SessionStore(api);
  await store.refresh();
  render(
    <ThemeProvider>
      <AuthContext.Provider value={store}>
        <MemoryRouter initialEntries={['/login']}>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<h1>Account</h1>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </ThemeProvider>,
  );
  return { api, interaction: userEvent.setup() };
}

describe('login form', () => {
  it('validates and focuses the first invalid field without a request', async () => {
    const { api, interaction } = await setup();
    await interaction.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(screen.getByLabelText('Username')).toHaveFocus();
    expect(screen.getByText('Enter your username.')).toBeVisible();
    expect(api.login).not.toHaveBeenCalled();
  });

  it('submits original credentials and navigates after server success', async () => {
    const { api, interaction } = await setup();
    await interaction.type(screen.getByLabelText('Username'), ' Employee_A ');
    await interaction.type(
      screen.getByLabelText('Password', { exact: true }),
      ' password ',
    );
    await interaction.click(screen.getByRole('button', { name: 'Sign in' }));
    await screen.findByRole('heading', { name: 'Account' });
    expect(api.login).toHaveBeenCalledWith({
      username: ' Employee_A ',
      password: ' password ',
    });
  });

  it('shows a generic credential failure, clears password and focuses the alert', async () => {
    const { interaction } = await setup(new ApiError(401, 'invalid_credentials'));
    await interaction.type(screen.getByLabelText('Username'), 'employee_a');
    await interaction.type(
      screen.getByLabelText('Password', { exact: true }),
      'incorrect',
    );
    await interaction.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Invalid username or password.',
    );
    expect(screen.getByRole('alert')).toHaveFocus();
    expect(screen.getByLabelText('Password', { exact: true })).toHaveValue('');
  });

  it('supports accessible password visibility', async () => {
    const { interaction } = await setup();
    await interaction.click(screen.getByRole('button', { name: 'Show password' }));
    expect(screen.getByLabelText('Password', { exact: true })).toHaveAttribute(
      'type',
      'text',
    );
    await interaction.click(screen.getByRole('button', { name: 'Hide password' }));
    expect(screen.getByLabelText('Password', { exact: true })).toHaveAttribute(
      'type',
      'password',
    );
  });

  it('honors Retry-After and prevents repeated submissions', async () => {
    const { api, interaction } = await setup(
      new ApiError(429, 'login_rate_limited', new Headers({ 'Retry-After': '60' })),
    );
    await interaction.type(screen.getByLabelText('Username'), 'employee_a');
    await interaction.type(
      screen.getByLabelText('Password', { exact: true }),
      'incorrect',
    );
    await interaction.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Sign in' })).toBeDisabled(),
    );
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }).closest('form')!);
    expect(api.login).toHaveBeenCalledTimes(1);
  });
});
