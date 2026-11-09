import { once } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { runLiveAuth } from '../support/live-auth';

for (const failure of ['disabled password', 'stalled login'] as const) {
  test(`withholds live credentials after ${failure}`, async ({ browserName }) => {
    const directory = await mkdtemp(join(tmpdir(), 'cyberguard-live-auth-'));
    const passwordFile = join(directory, 'password');
    // Synthetic only; real passwords never enter the test-runner process.
    const password = 'synthetic-live-password-not-a-real-secret';
    await writeFile(passwordFile, password, { mode: 0o600 });
    const server = createServer((_request, response) => {
      response.setHeader('Content-Type', 'text/html');
      response.end(`<!doctype html><html lang="en"><body>
        <label>Username<input autocomplete="username"></label>
        <label>Password<input type="password" autocomplete="current-password"
          ${failure === 'disabled password' ? 'disabled' : ''}></label>
        <button onclick="document.querySelectorAll('input').forEach(input => input.disabled = true)">Sign in</button>
      </body></html>`);
    });
    try {
      server.listen(0, '127.0.0.1');
      await once(server, 'listening');
      const address = server.address();
      if (!address || typeof address === 'string') throw new Error('Missing test port.');
      const error: unknown = await runLiveAuth({
        browserName,
        baseURL: `http://127.0.0.1:${address.port}`,
        passwordFile,
        username: 'employee_a',
      }).catch((error: unknown) => error);
      const checkpoint =
        failure === 'disabled password' ? 'entering credentials' : 'signing in';
      expect(error).toBeInstanceOf(Error);
      expect((error as Error).message).toBe(
        `Live authentication failed at ${checkpoint}. Sensitive diagnostics withheld.`,
      );
      expect((error as Error).stack).not.toContain(password);
      expect((error as Error).cause).toBeUndefined();
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
      await rm(directory, { recursive: true, force: true });
    }
  });
}
