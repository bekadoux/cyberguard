import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it, vi } from 'vitest';
import { AuthContext } from '../auth/auth-context';
import { SessionStore } from '../auth/session-store';
import { AdminPage } from './AdminPage';
import { adminApi, type ManagedUser } from './workflow-api';

const account: ManagedUser = {
  id: 'user-id',
  username: 'case-owner',
  role: 'employee',
  is_blocked: false,
};

async function setup() {
  const users = vi.spyOn(adminApi, 'users').mockResolvedValue([account]);
  vi.spyOn(adminApi, 'audit').mockResolvedValue([]);
  const store = new SessionStore({
    me: vi.fn().mockResolvedValue({ id: 'admin-id', username: 'admin', role: 'admin' }),
    login: vi.fn(),
    logout: vi.fn(),
  });
  await store.refresh();
  render(
    <AuthContext.Provider value={store}>
      <AdminPage />
    </AuthContext.Provider>,
  );
  const select = await screen.findByRole('combobox', { name: 'Role for case-owner' });
  return { users, select, user: userEvent.setup() };
}

it('keeps the confirmed role on cancel and failure, and shows the saved role on success', async () => {
  const { users, select, user } = await setup();
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  const save = vi.spyOn(adminApi, 'role').mockRejectedValueOnce(new Error());
  await user.selectOptions(select, 'analyst');
  expect(confirm).toHaveBeenCalledWith('Change role for case-owner?');
  expect(save).not.toHaveBeenCalled();
  expect(select).toHaveValue('employee');

  confirm.mockReturnValue(true);
  await user.selectOptions(select, 'analyst');
  expect(await screen.findByRole('alert')).toBeVisible();
  expect(save).toHaveBeenCalledWith(account.id, 'analyst', '');
  expect(select).toHaveValue('employee');

  save.mockResolvedValue(undefined);
  users.mockResolvedValue([{ ...account, role: 'analyst' }]);
  await user.selectOptions(select, 'analyst');
  await waitFor(() => expect(select).toHaveValue('analyst'));
});

const invalidPasswords = [
  'a'.repeat(11),
  `${'😀'.repeat(32)}a`, // 129 bytes, despite having only 65 UTF-16 code units
  '\u0085'.repeat(6), // whitespace under Go strings.TrimSpace, but not JS trim()
  `${'a'.repeat(11)}\0`,
];

it('validates username and password before creating, and sends the original 12 bytes', async () => {
  const { user } = await setup();
  const create = vi.spyOn(adminApi, 'create').mockResolvedValue(undefined);
  const username = screen.getByLabelText('Username');
  const input = screen.getByLabelText('Initial password');
  const submit = screen.getByRole('button', { name: 'Create account' });
  await user.type(username, 'bad!');
  expect(username).toBeInvalid();
  await user.clear(username);
  await user.type(username, 'new-user');
  expect(username).toBeValid();
  for (const value of invalidPasswords) {
    fireEvent.change(input, { target: { value } });
    await user.click(submit);
    expect(input).toBeInvalid();
    expect(create).not.toHaveBeenCalled();
  }
  const value = ' 😀😀é ';
  fireEvent.change(input, { target: { value } });
  await user.click(submit);
  expect(create).toHaveBeenCalledWith('new-user', value, 'employee');
  expect(input).toHaveValue('');
});

it('masks the reset password, discards canceled input, and submits only a valid confirmed reset', async () => {
  const { user } = await setup();
  const reset = vi.spyOn(adminApi, 'password').mockResolvedValue(undefined);
  const open = screen.getByRole('button', { name: 'Reset password for case-owner' });
  const field = () => screen.getByLabelText('New password for case-owner');
  await user.click(open);
  expect(field()).toHaveAttribute('type', 'password');
  expect(field()).toHaveAccessibleDescription(/12–128 UTF-8 bytes/);
  await user.type(field(), 'temporary input');
  await user.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(open).toHaveFocus();
  expect(reset).not.toHaveBeenCalled();
  await user.click(open);
  expect(field()).toHaveValue('');
  const submit = screen.getByRole('button', { name: 'Confirm password reset' });
  for (const value of invalidPasswords) {
    fireEvent.change(field(), { target: { value } });
    await user.click(submit);
    expect(field()).toBeInvalid();
    expect(reset).not.toHaveBeenCalled();
  }
  await user.type(screen.getByLabelText(/Reason for account changes/), 'Requested');
  const value = '😀'.repeat(32);
  fireEvent.change(field(), { target: { value } });
  await user.click(submit);
  expect(reset).toHaveBeenCalledWith(account.id, value, 'Requested');
  expect(screen.queryByLabelText('New password for case-owner')).not.toBeInTheDocument();
  expect(await screen.findByRole('status')).toHaveTextContent('Password reset.');
});
