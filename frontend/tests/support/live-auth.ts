import { spawn } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Only these fixed labels cross the credential-bearing process boundary.
export const liveAuthChecks = [
  'setup',
  'opening the login page',
  'entering credentials',
  'signing in',
  'checking cookie policy',
  'checking browser storage',
  'restoring the session',
  'signing out',
  'rejecting the revoked cookie',
  'confirming sign-out after reload',
] as const;

export async function runLiveAuth(options: {
  browserName: string;
  baseURL: string;
  passwordFile: string;
  username: string;
}): Promise<void> {
  const code = await new Promise<number | null>((resolveExit) => {
    const child = spawn(
      process.execPath,
      [
        fileURLToPath(new URL('./live-auth-check.ts', import.meta.url)),
        options.browserName,
        options.baseURL,
      ],
      {
        env: {
          ...process.env,
          E2E_PASSWORD_FILE: resolve(options.passwordFile),
          E2E_USERNAME: options.username,
          DEBUG: '',
          DEBUG_FILE: '',
          PWDEBUG: '0',
        },
        // Never collect action errors, browser logs or credentials in runner artifacts.
        stdio: 'ignore',
        timeout: 45_000,
        killSignal: 'SIGKILL',
      },
    );
    child.once('error', () => resolveExit(null));
    child.once('close', resolveExit);
  });
  if (code !== 0) {
    const check = code === null ? undefined : liveAuthChecks[code - 1];
    throw new Error(
      `Live authentication failed at ${check ?? 'browser process'}. Sensitive diagnostics withheld.`,
    );
  }
}
