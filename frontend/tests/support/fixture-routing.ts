import type { Page } from '@playwright/test';

/** Keep both services on the explicit fixture, without intercepting Vite modules. */
export async function routeFixtureServices(page: Page, api: URL): Promise<void> {
  await page.route(
    (url) => url.pathname.startsWith('/api/') || url.pathname.startsWith('/ai/'),
    async (route) => {
      const url = new URL(route.request().url());
      const response = await route.fetch({
        url: `${api.origin}${url.pathname}${url.search}`,
        timeout: 15_000,
      });
      await route.fulfill({ response });
    },
  );
}
