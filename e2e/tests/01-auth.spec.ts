import { expect, test } from '@playwright/test';
import { startTestServer, type RunningTestServer } from '../helpers/server';

test.describe('Journey 1 — Authentication Flow', () => {
  let server: RunningTestServer;

  test.beforeEach(async () => {
    server = await startTestServer({ authDisabled: false, password: 'e2e-secret-password' });
  });

  test.afterEach(async () => {
    await server?.stop();
  });

  test('redirects to /login, sets HttpOnly cookie, persists across reload, and signs out', async ({
    page,
    context,
    request,
  }) => {
    await page.goto(server.baseUrl);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByRole('heading', { name: 'Sign in to Photo View Tracker' })).toBeVisible();

    const passwordInput = page.getByLabel('Password', { exact: true });
    await passwordInput.fill('wrong-password');
    await page.getByRole('button', { name: 'Sign In' }).click();
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page).toHaveURL(/\/login$/);

    await passwordInput.fill('e2e-secret-password');
    await page.getByRole('button', { name: 'Sign In' }).click();

    await expect(page).toHaveURL(new RegExp(`^${server.baseUrl}/?$`));
    await expect(page.getByRole('button', { name: 'Gallery' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Library & Sync' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Sign Out' })).toBeVisible();

    const cookies = await context.cookies(server.baseUrl);
    const sessionCookie = cookies.find((c) => c.name === 'pt_session');
    expect(sessionCookie).toBeDefined();
    expect(sessionCookie?.httpOnly).toBe(true);
    expect(sessionCookie?.sameSite).toBe('Lax');

    await page.reload();
    await expect(page).toHaveURL(new RegExp(`^${server.baseUrl}/?$`));
    await expect(page.getByRole('button', { name: 'Sign Out' })).toBeVisible();

    await page.getByRole('button', { name: 'Sign Out' }).click();
    await expect(page).toHaveURL(/\/login$/);

    await page.reload();
    await expect(page).toHaveURL(/\/login$/);

    const apiRes = await request.get(`${server.baseUrl}/api/v1/photos`);
    expect(apiRes.status()).toBe(401);
  });
});
