import { test } from '@playwright/test';
import { runLiveAuth } from '../support/live-auth';

test('real Go/Caddy login, HttpOnly cookie, persistence and server-side logout', async ({
  browserName,
  baseURL,
}) => {
  test.setTimeout(60_000);
  const file = process.env.E2E_PASSWORD_FILE;
  if (!file)
    throw new Error(
      'E2E_PASSWORD_FILE must reference a local demo account password file.',
    );
  await runLiveAuth({
    browserName,
    baseURL: baseURL!,
    passwordFile: file,
    username: process.env.E2E_USERNAME ?? 'employee_a',
  });
});
