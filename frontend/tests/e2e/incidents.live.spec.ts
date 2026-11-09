import { test } from '@playwright/test';
import { runLiveIncidents } from '../support/live-incidents';

test('real disposable-backend submission, persistence, employee isolation and staff visibility', async ({
  browserName,
  baseURL,
}) => {
  test.setTimeout(180_000);
  const api = process.env.E2E_INCIDENT_API_ORIGIN;
  const secrets = process.env.E2E_INCIDENT_SECRETS_DIR;
  if (!api || !secrets)
    throw new Error(
      'E2E_INCIDENT_API_ORIGIN and E2E_INCIDENT_SECRETS_DIR must identify an explicitly disposable fixture.',
    );
  await runLiveIncidents(browserName, baseURL!, api, secrets);
});
