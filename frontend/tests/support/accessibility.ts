import { execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

/** Isolate native Firefox/axe evaluation from Playwright Test's instrumentation. */
export async function auditIncidents(
  browserName: string,
  baseURL: string,
  colorScheme: 'light' | 'dark',
  screen: 'list' | 'detail' = 'list',
): Promise<unknown> {
  const { stdout } = await promisify(execFile)(
    process.execPath,
    [
      fileURLToPath(new URL('./accessibility-check.ts', import.meta.url)),
      browserName,
      baseURL,
      colorScheme,
      screen,
    ],
    {
      timeout: 45_000,
      maxBuffer: 1_000_000,
      env: { ...process.env, DEBUG: '', DEBUG_FILE: '', PWDEBUG: '0' },
    },
  );
  return JSON.parse(stdout);
}
