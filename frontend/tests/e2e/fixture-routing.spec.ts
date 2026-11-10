import { once } from 'node:events';
import { createServer } from 'node:http';
import { expect, test } from '@playwright/test';
import { completedAssessment } from '../support/assessments';
import { routeFixtureServices } from '../support/fixture-routing';
import { analyst, detail, first } from '../support/incidents';

test('keeps Go and AI reads on the fixture instead of expiring its session elsewhere', async ({
  page,
}) => {
  const requests: string[] = [];
  const server = createServer((request, response) => {
    const path = new URL(request.url!, 'http://fixture.invalid').pathname;
    requests.push(path);
    response.setHeader('Content-Type', 'application/json');
    response.setHeader('Cache-Control', 'no-store');
    if (path === '/api/auth/me') response.end(JSON.stringify({ user: analyst }));
    else if (path === `/api/incidents/${first.id}`)
      response.end(JSON.stringify({ incident: { ...detail, severity: null } }));
    else if (path === `/ai/incidents/${first.id}/assessment`)
      response.end(JSON.stringify(completedAssessment));
    else {
      response.statusCode = 404;
      response.end('{}');
    }
  });
  try {
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('Missing fixture port.');
    let outsideReads = 0;
    // Simulate an ordinary dev AI backend that does not recognize fixture sessions.
    // The fixture bridge, registered afterward, must take precedence over this route.
    await page.route('**/ai/**', (route) => {
      outsideReads++;
      return route.fulfill({ status: 401, json: { detail: 'Unknown dev session' } });
    });
    await routeFixtureServices(page, new URL(`http://127.0.0.1:${address.port}`));
    await page.goto(`/incidents/${first.id}`);
    await expect(page.getByRole('heading', { name: 'Draft summary' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible();
    expect(outsideReads).toBe(0);
    expect(requests).toContain('/api/auth/me');
    expect(requests).toContain(`/api/incidents/${first.id}`);
    expect(requests).toContain(`/ai/incidents/${first.id}/assessment`);
    expect(requests.every((path) => /^\/(api|ai)\//.test(path))).toBe(true);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
