import { expect, test } from '@playwright/test';
import {
  revealLightboxHUD,
  startTestServer,
  syncLibraryViaAPI,
  waitForImageLoaded,
  type RunningTestServer,
} from '../helpers/server';

test.describe('Journey 4 — Slideshow Auto-Advance', () => {
  let server: RunningTestServer;

  test.beforeEach(async () => {
    server = await startTestServer({ authDisabled: true });
    await syncLibraryViaAPI(server.baseUrl);
  });

  test.afterEach(async () => {
    await server?.stop();
  });

  test('auto-advances through photos and records views along the way', async ({ page }) => {
    await page.goto(server.baseUrl);
    const cards = page.locator('button.photo-card');
    await expect(cards).toHaveCount(5);

    const firstTitle = (await cards.nth(0).locator('.card-title').innerText()).trim();
    const secondTitle = (await cards.nth(1).locator('.card-title').innerText()).trim();

    await cards.nth(0).click();
    const dialog = page.locator('dialog#photo-lightbox-dialog[open]');
    await expect(dialog).toBeVisible();
    await expect(dialog.locator('#lightbox-title')).toHaveText(firstTitle);
    await waitForImageLoaded(dialog.locator('.stage img'));

    await revealLightboxHUD(page);
    await dialog.getByLabel('Slideshow interval').selectOption('3');

    // Wait for second photo's slideshow dwell POST /views after auto-advance.
    const secondViewPromise = page.waitForResponse(
      (res) =>
        res.url().includes('/views') &&
        res.request().method() === 'POST' &&
        res.status() === 200 &&
        (res.request().postData()?.includes('"slideshow"') ?? false),
      { timeout: 15_000 },
    );

    await dialog.getByRole('button', { name: /Slideshow/ }).click();
    await expect(dialog.getByRole('button', { name: /Pause/ })).toHaveAttribute('aria-pressed', 'true');

    await expect(dialog.locator('#lightbox-title')).toHaveText(secondTitle, { timeout: 10_000 });
    await secondViewPromise;

    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();

    await page.reload();
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: firstTitle }) }).locator('.badge-views')).toContainText('1');
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: secondTitle }) }).locator('.badge-views')).toContainText('1');
  });
});
