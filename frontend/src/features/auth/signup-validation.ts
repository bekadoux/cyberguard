import type { Credentials } from './api';

export type SignupFields = Credentials & { confirmPassword: string };

// UI validation is only a convenience. The Go API remains authoritative for
// username normalization, length (UTF-8 bytes), and password policy.
export function validateSignup(fields: SignupFields) {
  const errors: Partial<Record<keyof SignupFields, string>> = {};
  const bytes = (value: string) => new TextEncoder().encode(value).length;
  if (!fields.username) errors.username = 'Enter a username.';
  else if (bytes(fields.username) > 128)
    errors.username = 'Username must be at most 128 UTF-8 bytes.';
  if (!fields.password) errors.password = 'Enter a password.';
  else if (
    bytes(fields.password) < 12 ||
    bytes(fields.password) > 128 ||
    fields.password.includes('\0') ||
    !fields.password.trim()
  )
    errors.password = 'Use a nonblank password of 12–128 UTF-8 bytes.';
  if (!fields.confirmPassword) errors.confirmPassword = 'Confirm your password.';
  else if (fields.confirmPassword !== fields.password)
    errors.confirmPassword = 'Passwords do not match.';
  return errors;
}
