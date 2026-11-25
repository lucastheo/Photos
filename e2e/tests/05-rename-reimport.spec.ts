import fs from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import {
  revealLightboxHUD,
  startTestServer,
  syncLibraryViaAPI,
  waitForImageLoaded,
  type RunningTestServer,
} from '../helpers/server';

test.describe('Journey 5 — Disk Rename/Move + Incremental Re-import', () => {
  let server: RunningTestServer;

  test.beforeEach(async () => {
    server = await startTestServer({ authDisabled: true });
    await syncLibraryViaAPI(server.baseUrl);
  });

  test.afterEach(async () => {
    await server?.stop();
  });

  test('preserves view_count and emoji tags after file is moved and renamed on disk', async ({ page }) => {
    await page.goto(server.baseUrl);
    const cards = page.locator('button.photo-card');
    const cafeCard = cards.filter({ has: page.locator('.card-title', { hasText: /^Street_Cafe$/ }) });
    await expect(cafeCard).toHaveCount(1);

    await cafeCard.click();
    const dialog = page.locator('dialog#photo-lightbox-dialog[open]');
    await expect(dialog).toBeVisible();
    await waitForImageLoaded(dialog.locator('.stage img'));

    // Increment twice manually and attach ⭐ tag.
    await revealLightboxHUD(page);
    await dialog.getByRole('button', { name: '+1', exact: true }).click();
    await expect(dialog.locator('.view-count-big')).toContainText('1');

    await revealLightboxHUD(page);
    await dialog.getByRole('button', { name: '+1', exact: true }).click();
    await expect(dialog.locator('.view-count-big')).toContainText('2');

    await revealLightboxHUD(page);
    const emojiCard = dialog.locator('section.drawer-card', { has: page.getByRole('heading', { name: 'Emoji Tags' }) });
    await emojiCard.getByRole('button', { name: '⭐' }).click();
    await expect(emojiCard.getByRole('button', { name: /Remove ⭐/ })).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(cafeCard.locator('.badge-views')).toContainText('2');

    // Move and rename file on disk inside LIBRARY_PATH.
    const oldPath = path.join(server.libraryDir, '2026', 'City', 'Street_Cafe.jpg');
    const newPath = path.join(server.libraryDir, '2026', 'Moved', 'Renamed_Cafe.jpg');
    fs.mkdirSync(path.dirname(newPath), { recursive: true });
    fs.renameSync(oldPath, newPath);

    await page.getByRole('button', { name: 'Library & Sync' }).click();
    await page.getByRole('button', { name: 'Re-import / Sync Library' }).click();
    await expect(page.getByRole('status')).toContainText('Sync completed:');

    const syncSection = page.locator('section', { has: page.getByRole('heading', { name: 'Re-import / Sync Library' }) });
    await expect(syncSection.locator('.stat-box', { hasText: 'Moved' }).locator('.stat-val')).toHaveText('1');
    await expect(syncSection.locator('.stat-box', { hasText: 'Missing' }).locator('.stat-val')).toHaveText('0');

    await page.getByRole('button', { name: 'Gallery' }).click();
    await expect(cards).toHaveCount(5);
    await expect(cards.filter({ has: page.locator('.card-title', { hasText: /^Street_Cafe$/ }) })).toHaveCount(0);

    const renamedCard = cards.filter({ has: page.locator('.card-title', { hasText: /^Renamed_Cafe$/ }) });
    await expect(renamedCard).toHaveCount(1);
    await expect(renamedCard.locator('.badge-views')).toContainText('2');
    await expect(renamedCard.locator('.card-meta')).toContainText('2026/Moved');
    await expect(renamedCard.locator('.card-meta')).toContainText('⭐');
  });
});
