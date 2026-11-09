import type { Credentials } from './api';

export function validateCredentials({ username, password }: Credentials) {
  const errors: Partial<Record<keyof Credentials, string>> = {};
  const bytes = (value: string) => new TextEncoder().encode(value).length;
  if (!username.trim()) {
    errors.username = 'Enter your username.';
  } else if (
    bytes(username) > 128 ||
    !/^[a-z0-9][a-z0-9_.-]{2,63}$/.test(username.trim().toLowerCase())
  ) {
    errors.username =
      'Use 3–64 letters, numbers, dots, underscores or hyphens. Start with a letter or number.';
  }
  if (!password) errors.password = 'Enter your password.';
  else if (bytes(password) > 128)
    errors.password = 'Your password exceeds 128 UTF-8 bytes.';
  return errors;
}
