import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

for (const theme of ['light', 'dark'] as const) {
  test(`has no automated accessibility violations in ${theme} mode`, async ({ page }) => {
    // Scan each palette via the system preference. Firefox 159's Ubuntu fallback
    // crashes inside axe after changing a native select's selected option. Theme
    // control interactions are covered separately; browser defaults/rules stay intact.
    await page.emulateMedia({ colorScheme: theme });
    await page.route('**/api/auth/me', (route) =>
      route.fulfill({
        status: 401,
        json: {
          error: { code: 'authentication_required', message: 'Authentication required.' },
        },
      }),
    );
    for (const [width, height] of [
      [1440, 900],
      [1280, 720],
      [375, 667],
      [320, 568],
    ] as const) {
      await test.step(`${width} × ${height}`, async () => {
        await page.setViewportSize({ width, height });
        await page.goto('/login');
        await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        const results = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
          .analyze();
        expect(results.violations).toEqual([]);
      });
    }
  });
}
