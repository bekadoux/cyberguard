import type { Credentials } from './api';

export function validateCredentials({ username, password }: Credentials) {
  const errors: Partial<Record<keyof Credentials, string>> = {};
  const bytes = (value: string) => new TextEncoder().encode(value).length;
  // Go owns normalization and the normalized ASCII policy. JavaScript's Unicode
  // lowercasing/trimming differs and can reject names the backend accepts.
  if (!username) {
    errors.username = 'Enter your username.';
  } else if (bytes(username) > 128) {
    errors.username = 'Your username exceeds 128 UTF-8 bytes.';
  }
  if (!password) errors.password = 'Enter your password.';
  else if (bytes(password) > 128)
    errors.password = 'Your password exceeds 128 UTF-8 bytes.';
  return errors;
}
