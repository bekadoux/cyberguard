import { spawn } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export const incidentChecks = [
  'setup',
  'signing in reporter',
  'submitting report',
  'reading saved report',
  'reloading report',
  'checking reporter list',
  'checking employee isolation',
  'checking staff visibility',
  'checking AI assessment',
  'checking staff own-report access',
  'signing out',
] as const;

export async function runLiveIncidents(
  browserName: string,
  baseURL: string,
  apiOrigin: string,
  secretsDir: string,
  packaged = false,
  assessments = false,
): Promise<void> {
  const code = await new Promise<number | null>((resolveExit) => {
    const child = spawn(
      process.execPath,
      [
        fileURLToPath(new URL('./live-incidents-check.ts', import.meta.url)),
        browserName,
        baseURL,
      ],
      {
        env: {
          ...process.env,
          E2E_INCIDENT_API_ORIGIN: apiOrigin,
          E2E_INCIDENT_SECRETS_DIR: resolve(secretsDir),
          E2E_INCIDENT_PACKAGED: packaged ? '1' : '0',
          E2E_INCIDENT_ASSESSMENTS: assessments ? '1' : '0',
          DEBUG: '',
          DEBUG_FILE: '',
          PWDEBUG: '0',
        },
        stdio: 'ignore',
        timeout: 150_000,
        killSignal: 'SIGKILL',
      },
    );
    child.once('error', () => resolveExit(null));
    child.once('close', resolveExit);
  });
  if (code !== 0)
    throw new Error(
      `Live incident flow failed at ${code === null ? 'browser process' : (incidentChecks[code - 1] ?? 'browser process')}. Sensitive diagnostics withheld.`,
    );
}
