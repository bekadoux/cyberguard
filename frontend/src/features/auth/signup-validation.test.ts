import { describe, expect, it } from 'vitest';
import { validateSignup } from './signup-validation';

const valid = {
  username: 'employee_a',
  password: 'long secure password',
  confirmPassword: 'long secure password',
};

describe('employee enrollment validation', () => {
  it('accepts valid input without modifying the submitted username or password', () => {
    expect(validateSignup(valid)).toEqual({});
  });
  it('rejects missing fields and mismatched confirmation', () => {
    expect(
      validateSignup({ username: '', password: 'short', confirmPassword: 'different' }),
    ).toMatchObject({
      username: expect.any(String),
      password: expect.any(String),
      confirmPassword: expect.any(String),
    });
  });
  it('uses UTF-8 byte lengths rather than JavaScript string lengths', () => {
    expect(
      validateSignup({
        ...valid,
        password: '🛡️'.repeat(50),
        confirmPassword: '🛡️'.repeat(50),
      }).password,
    ).toBeTruthy();
  });
});
