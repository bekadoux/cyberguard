import AxeBuilder from '@axe-core/playwright';
import { chromium, firefox } from '@playwright/test';
import { employee, first } from './incidents.ts';

// Synthetic fixtures only. Keep every axe rule enabled; no live secrets or data.
const browserType = process.argv[2] === 'firefox' ? firefox : chromium;
const browser = await browserType.launch();
try {
  const context = await browser.newContext({
    baseURL: process.argv[3],
    colorScheme: process.argv[4] === 'dark' ? 'dark' : 'light',
  });
  const page = await context.newPage();
  await page.route('**/api/auth/me', (route) =>
    route.fulfill({ json: { user: employee } }),
  );
  await page.route('**/api/incidents?*', (route) =>
    route.fulfill({
      json: {
        incidents: [{ ...first, title: 'A very long incident title '.repeat(7) }],
        next_cursor: null,
      },
    }),
  );
  const failures: unknown[] = [];
  for (const [width, height] of [
    [1280, 720],
    [375, 667],
    [320, 568],
  ] as const) {
    await page.setViewportSize({ width, height });
    await page.goto('/incidents');
    await page.getByText('1 incident loaded · Newest first').waitFor();
    const result = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze();
    if (result.violations.length)
      failures.push({ viewport: `${width}x${height}`, violations: result.violations });
  }
  process.stdout.write(JSON.stringify(failures));
} finally {
  await browser.close();
}
