import { describe, expect, it } from 'vitest';
import { validateCredentials } from './validation';

describe('credential validation', () => {
  it('accepts backend username normalization without changing the password', () => {
    expect(
      validateCredentials({ username: '  EMPLOYEE_A ', password: '  pass word  ' }),
    ).toEqual({});
  });

  it('requires both fields', () => {
    expect(validateCredentials({ username: ' ', password: '' })).toEqual({
      username: 'Enter your username.',
      password: 'Enter your password.',
    });
  });

  it.each(['ab', '_employee', 'employee@example.com', 'a'.repeat(65)])(
    'rejects invalid username %s',
    (username) => {
      expect(
        validateCredentials({ username, password: 'password' }).username,
      ).toBeTruthy();
    },
  );

  it('limits the raw username to 128 UTF-8 bytes before normalization', () => {
    expect(
      validateCredentials({ username: ' '.repeat(126) + 'abc', password: 'password' })
        .username,
    ).toBeTruthy();
  });

  it('counts password UTF-8 bytes, not JavaScript UTF-16 length', () => {
    expect(
      validateCredentials({ username: 'employee', password: '🔒'.repeat(32) }),
    ).toEqual({});
    expect(
      validateCredentials({ username: 'employee', password: '🔒'.repeat(33) }).password,
    ).toBeTruthy();
    expect(
      validateCredentials({ username: 'employee', password: ' '.repeat(128) }),
    ).toEqual({});
  });
});
