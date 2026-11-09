import { ApiError, request } from '../../shared/api/client';

export type User = {
  id: string;
  username: string;
  role: 'employee' | 'analyst' | 'admin';
};

export type Credentials = { username: string; password: string };

function parseUser(data: unknown): User {
  const user = (data as { user?: Partial<User> } | null)?.user;
  if (
    !user ||
    typeof user.id !== 'string' ||
    typeof user.username !== 'string' ||
    !['employee', 'analyst', 'admin'].includes(user.role ?? '')
  ) {
    throw new ApiError(200, 'invalid_response');
  }
  return { id: user.id, username: user.username, role: user.role as User['role'] };
}

export const authApi = {
  async me(signal?: AbortSignal): Promise<User> {
    return parseUser(await request('/auth/me', { signal }));
  },
  async login(credentials: Credentials): Promise<User> {
    return parseUser(await request('/auth/login', { method: 'POST', body: credentials }));
  },
  async logout(): Promise<void> {
    const result = await request('/auth/logout', { method: 'POST' });
    if (result !== undefined) throw new ApiError(200, 'invalid_response');
  },
};
