import { expect, test } from '@playwright/test';
import {
  revealLightboxHUD,
  startTestServer,
  syncLibraryViaAPI,
  waitForImageLoaded,
  type RunningTestServer,
} from '../helpers/server';

test.describe('Journey 3 — 1.5s Dwell Counter, Variant Switcher, Emoji Tagging & Manual Views', () => {
  let server: RunningTestServer;

  test.beforeEach(async () => {
    server = await startTestServer({ authDisabled: true });
    await syncLibraryViaAPI(server.baseUrl);
  });

  test.afterEach(async () => {
    await server?.stop();
  });

  test('skips <1.5s, counts >=1.5s dwell, switches CR3/JPG without extra view, adjusts views & tags', async ({ page }) => {
    await page.goto(server.baseUrl);
    const cards = page.locator('button.photo-card');
    await expect(cards).toHaveCount(5);

    const firstTitle = (await cards.nth(0).locator('.card-title').innerText()).trim();
    const secondTitle = (await cards.nth(1).locator('.card-title').innerText()).trim();

    // Open first card, wait <1.5s after media load, then navigate right so it stays at 0 views.
    await cards.nth(0).click();
    const dialog = page.locator('dialog#photo-lightbox-dialog[open]');
    await expect(dialog).toBeVisible();
    await expect(dialog.locator('#lightbox-title')).toHaveText(firstTitle);
    await waitForImageLoaded(dialog.locator('.stage img'));
    await page.waitForTimeout(450);

    const dwellResponsePromise = page.waitForResponse(
      (res) => res.url().includes('/views') && res.request().method() === 'POST' && res.status() === 200,
    );
    await page.keyboard.press('ArrowRight');
    await expect(dialog.locator('#lightbox-title')).toHaveText(secondTitle);
    await waitForImageLoaded(dialog.locator('.stage img'));

    // Wait >=1.5s dwell to complete and record view in SQLite.
    await dwellResponsePromise;
    await revealLightboxHUD(page);
    await expect(dialog.locator('.view-count-big')).toContainText('1');
    await expect(dialog.locator('ul.history-list li')).toHaveCount(1);
    await expect(dialog.locator('ul.history-list li').first()).toContainText('(dwell)');

    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();

    // Card 0 stayed 0; card 1 updated in-place to 1 and persists after reload.
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: firstTitle }) }).locator('.badge-views')).toContainText('0');
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: secondTitle }) }).locator('.badge-views')).toContainText('1');

    await page.reload();
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: firstTitle }) }).locator('.badge-views')).toContainText('0');
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: secondTitle }) }).locator('.badge-views')).toContainText('1');

    // Open stacked IMG_0042, dwell for 1 view, then switch CR3 -> JPG without counting an extra view.
    const stackedCard = cards.filter({ has: page.locator('.card-title', { hasText: /^IMG_0042$/ }) });
    const stackDwellPromise = page.waitForResponse(
      (res) => res.url().includes('/views') && res.request().method() === 'POST' && res.status() === 200,
    );
    await stackedCard.click();
    await expect(dialog).toBeVisible();
    await waitForImageLoaded(dialog.locator('.stage img'));
    await stackDwellPromise;

    await revealLightboxHUD(page);
    const variantGroup = dialog.locator('.variant-group[aria-label="Format variant"]');
    await expect(variantGroup.getByRole('button', { name: 'CR3' })).toHaveAttribute('aria-pressed', 'true');
    await variantGroup.getByRole('button', { name: 'JPG' }).click();
    await expect(dialog.locator('.stage img')).toHaveAttribute('src', /format=jpg/);
    await waitForImageLoaded(dialog.locator('.stage img'));
    await page.waitForTimeout(1700);

    await revealLightboxHUD(page);
    await expect(dialog.locator('.view-count-big')).toContainText('1');

    // Test manual +1, -1, and Reset to 0 synchronized with view_history.
    await dialog.getByRole('button', { name: '+1', exact: true }).click();
    await expect(dialog.locator('.view-count-big')).toContainText('2');
    await expect(dialog.locator('ul.history-list li')).toHaveCount(2);
    await expect(dialog.locator('ul.history-list')).toContainText('(dwell)');
    await expect(dialog.locator('ul.history-list')).toContainText('(manual)');

    await revealLightboxHUD(page);
    await dialog.getByRole('button', { name: '-1', exact: true }).click();
    await expect(dialog.locator('.view-count-big')).toContainText('1');
    await expect(dialog.locator('ul.history-list li')).toHaveCount(1);

    await revealLightboxHUD(page);
    await dialog.getByRole('button', { name: 'Reset to 0' }).click();
    await expect(dialog.locator('.view-count-big')).toContainText('0');
    await expect(dialog.locator('ul.history-list')).toContainText('No recorded views yet.');

    // Add heart emoji tag and filter gallery by it.
    await revealLightboxHUD(page);
    const emojiCard = dialog.locator('section.drawer-card', { has: page.getByRole('heading', { name: 'Emoji Tags' }) });
    await emojiCard.getByRole('button', { name: '❤️' }).click();
    await expect(emojiCard.getByRole('button', { name: /Remove ❤/ })).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();

    await page.getByRole('button', { name: /❤\s+1/ }).click();
    await expect(cards).toHaveCount(1);
    await expect(cards.first().locator('.card-title')).toHaveText('IMG_0042');
  });
});
