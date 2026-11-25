import { expect, test } from '@playwright/test';
import { startTestServer, waitForImageLoaded, type RunningTestServer } from '../helpers/server';

test.describe('Journey 2 — Library Import, RAW+JPEG Stacking, Duplicates & Filters', () => {
  let server: RunningTestServer;

  test.beforeEach(async () => {
    server = await startTestServer({ authDisabled: true });
  });

  test.afterEach(async () => {
    await server?.stop();
  });

  test('syncs library on /library, stacks CR3+JPG, reports duplicates, and filters gallery', async ({ page }) => {
    // Deep link to /library verifies SPA fallback routing.
    await page.goto(`${server.baseUrl}/library`);
    await expect(page.getByRole('heading', { name: 'Library Coverage' })).toBeVisible();

    await page.getByRole('button', { name: 'Re-import / Sync Library' }).click();
    await expect(page.getByRole('status')).toContainText('Sync completed:');

    const coverageSection = page.locator('section', { has: page.getByRole('heading', { name: 'Library Coverage' }) });
    await expect(coverageSection.locator('.stat-box', { hasText: 'Total Photos' }).locator('.stat-val')).toHaveText('5');
    await expect(coverageSection.locator('.stat-box', { hasText: 'Never Seen (0)' }).locator('.stat-val')).toHaveText('5');
    await expect(coverageSection.locator('.stat-box', { hasText: 'Duplicates' }).locator('.stat-val')).toHaveText('1');

    const dupSection = page.locator('section', { has: page.getByRole('heading', { name: /Duplicate Files Inspector/ }) });
    await expect(dupSection.locator('tbody tr')).toHaveCount(1);
    await expect(dupSection.locator('tbody tr').first()).toContainText('2026/Trip/Mountain_Lake.jpg');
    await expect(dupSection.locator('tbody tr').first()).toContainText('2026/Backup/Mountain_Lake_Copy.jpg');

    await page.getByRole('button', { name: 'Gallery' }).click();
    await expect(page).toHaveURL(new RegExp(`^${server.baseUrl}/?$`));

    const cards = page.locator('button.photo-card');
    await expect(cards).toHaveCount(5);

    for (let i = 0; i < 5; i++) {
      await waitForImageLoaded(cards.nth(i).locator('img'));
    }

    const stackedCard = cards.filter({ has: page.locator('.card-title', { hasText: /^IMG_0042$/ }) });
    await expect(stackedCard).toHaveCount(1);
    await expect(stackedCard.locator('.badge-format')).toHaveText('CR3 + JPG');
    await expect(stackedCard.locator('.badge-views')).toContainText('0');

    await page.getByRole('button', { name: 'Never Seen (0)' }).click();
    await expect(cards).toHaveCount(5);

    await page.getByRole('button', { name: 'Seen (1+)' }).click();
    await expect(page.getByText('No photos match the current filters.')).toBeVisible();

    await page.getByRole('button', { name: 'All', exact: true }).click();
    await expect(cards).toHaveCount(5);

    await page.getByRole('button', { name: 'CR3', exact: true }).click();
    await expect(cards).toHaveCount(1);
    await expect(cards.first().locator('.card-title')).toHaveText('IMG_0042');

    await page.getByRole('button', { name: 'Reset Filters' }).click();
    await expect(cards).toHaveCount(5);
  });
});
