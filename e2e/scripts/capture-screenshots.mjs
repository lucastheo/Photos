import { spawn } from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..');
const examplesDir = path.join(repoRoot, 'examples', 'photos');
const screenshotsDir = path.join(repoRoot, 'docs', 'screenshots');
const binaryPath = process.env.E2E_BINARY || path.join(repoRoot, 'bin', 'phototracker');

function copyDir(src, dst) {
  fs.mkdirSync(dst, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const s = path.join(src, entry.name);
    const d = path.join(dst, entry.name);
    if (entry.isDirectory()) {
      copyDir(s, d);
    } else {
      fs.copyFileSync(s, d);
    }
  }
}

function getFreePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (!addr || typeof addr === 'string') {
        srv.close(() => reject(new Error('failed to resolve ephemeral port')));
        return;
      }
      srv.close((err) => (err ? reject(err) : resolve(addr.port)));
    });
  });
}

async function waitForHealth(baseUrl, proc, logs) {
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    if (proc.exitCode !== null) {
      throw new Error(`phototracker exited early (${proc.exitCode}):\n${logs()}`);
    }
    try {
      const res = await fetch(`${baseUrl}/api/v1/health`);
      if (res.ok) return;
    } catch {
      // Wait for server listen
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`Timed out waiting for ${baseUrl}/api/v1/health\n${logs()}`);
}

async function startServer({ authDisabled, password }) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'phototracker-shots-'));
  const libraryDir = path.join(tempDir, 'library');
  const cacheDir = path.join(tempDir, 'cache');
  const dbPath = path.join(tempDir, 'photos.db');
  fs.mkdirSync(cacheDir, { recursive: true });
  copyDir(examplesDir, libraryDir);

  const port = await getFreePort();
  const baseUrl = `http://127.0.0.1:${port}`;
  const out = [];
  const proc = spawn(binaryPath, [], {
    cwd: tempDir,
    env: {
      ...process.env,
      LIBRARY_PATH: libraryDir,
      DB_PATH: dbPath,
      CACHE_PATH: cacheDir,
      HOST: '127.0.0.1',
      PORT: String(port),
      AUTH_DISABLED: String(authDisabled),
      AUTH_PASSWORD: password || 'demo-password',
      AUTH_PASSWORD_HASH: '',
      AUTH_SECRET: '',
      COOKIE_SECURE: 'false',
      LOG_FORMAT: 'text',
      LOG_LEVEL: 'warn',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  proc.stdout.on('data', (c) => out.push(c.toString('utf8')));
  proc.stderr.on('data', (c) => out.push(c.toString('utf8')));
  await waitForHealth(baseUrl, proc, () => out.join(''));

  return {
    baseUrl,
    stop: async () => {
      if (proc.exitCode === null) {
        await new Promise((resolve) => {
          const t = setTimeout(() => {
            proc.kill('SIGKILL');
            resolve();
          }, 5000);
          proc.once('exit', () => {
            clearTimeout(t);
            resolve();
          });
          proc.kill('SIGTERM');
        });
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
    },
  };
}

async function apiJson(baseUrl, reqPath, init = {}) {
  const res = await fetch(`${baseUrl}${reqPath}`, {
    ...init,
    headers: {
      Origin: baseUrl,
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init.headers || {}),
    },
  });
  if (!res.ok) {
    throw new Error(`${init.method || 'GET'} ${reqPath} failed (${res.status}): ${await res.text()}`);
  }
  return res.json();
}

async function waitForAllImages(locator) {
  const count = await locator.count();
  for (let i = 0; i < count; i++) {
    await locator.nth(i).evaluate(
      (el) =>
        new Promise((resolve, reject) => {
          if (el.complete) {
            if (el.naturalWidth > 0) resolve();
            else reject(new Error(`Broken image: ${el.src}`));
            return;
          }
          el.addEventListener('load', () => resolve(), { once: true });
          el.addEventListener('error', () => reject(new Error(`Image load error: ${el.src}`)), { once: true });
        }),
    );
  }
}

async function main() {
  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Compiled binary not found at ${binaryPath}. Run 'make build' first.`);
  }
  fs.mkdirSync(screenshotsDir, { recursive: true });

  const browser = await chromium.launch({ headless: true });
  const srv = await startServer({ authDisabled: true });

  try {
    await apiJson(srv.baseUrl, '/api/v1/library/sync', { method: 'POST' });
    const list = await apiJson(srv.baseUrl, '/api/v1/photos?page_size=50');
    const beach = list.items.find((p) => p.base_name === 'Beach_Sunset');
    const mountain = list.items.find((p) => p.base_name.startsWith('Mountain_Lake'));
    const cafe = list.items.find((p) => p.base_name === 'Street_Cafe');

    if (beach) {
      await apiJson(srv.baseUrl, `/api/v1/photos/${beach.id}/views`, {
        method: 'POST',
        body: JSON.stringify({ view_id: 'shot-b1', source: 'dwell' }),
      });
      await apiJson(srv.baseUrl, `/api/v1/photos/${beach.id}/views`, {
        method: 'PATCH',
        body: JSON.stringify({ action: 'increment' }),
      });
      await apiJson(srv.baseUrl, `/api/v1/photos/${beach.id}/tags`, {
        method: 'POST',
        body: JSON.stringify({ emoji: '❤️' }),
      });
      await apiJson(srv.baseUrl, `/api/v1/photos/${beach.id}/tags`, {
        method: 'POST',
        body: JSON.stringify({ emoji: '🏖️' }),
      });
    }
    if (mountain) {
      await apiJson(srv.baseUrl, `/api/v1/photos/${mountain.id}/views`, {
        method: 'POST',
        body: JSON.stringify({ view_id: 'shot-m1', source: 'dwell' }),
      });
      await apiJson(srv.baseUrl, `/api/v1/photos/${mountain.id}/tags`, {
        method: 'POST',
        body: JSON.stringify({ emoji: '❤️' }),
      });
      await apiJson(srv.baseUrl, `/api/v1/photos/${mountain.id}/tags`, {
        method: 'POST',
        body: JSON.stringify({ emoji: '⭐' }),
      });
    }
    if (cafe) {
      for (const id of ['shot-c1', 'shot-c2', 'shot-c3']) {
        await apiJson(srv.baseUrl, `/api/v1/photos/${cafe.id}/views`, {
          method: 'POST',
          body: JSON.stringify({ view_id: id, source: 'dwell' }),
        });
      }
      await apiJson(srv.baseUrl, `/api/v1/photos/${cafe.id}/tags`, {
        method: 'POST',
        body: JSON.stringify({ emoji: '🎉' }),
      });
    }

    const context = await browser.newContext({
      viewport: { width: 1440, height: 900 },
      deviceScaleFactor: 2,
      colorScheme: 'light',
    });
    const page = await context.newPage();

    // 1. Gallery view screenshot
    await page.goto(srv.baseUrl);
    await page.locator('button.photo-card').first().waitFor({ state: 'visible' });
    const hideBtn = page.getByRole('button', { name: /Show Filters|Hide Filters/ });
    if ((await hideBtn.getAttribute('aria-expanded')) === 'false') {
      await hideBtn.click();
    }
    await waitForAllImages(page.locator('button.photo-card img'));
    await page.waitForTimeout(250);
    await page.screenshot({ path: path.join(screenshotsDir, 'gallery.png') });

    // 2. Lightbox screenshot (open Beach_Sunset and reveal floating HUD + drawer)
    await page.locator('button.photo-card', { has: page.locator('.card-title', { hasText: 'Beach_Sunset' }) }).click();
    const dialog = page.locator('dialog#photo-lightbox-dialog[open]');
    await dialog.waitFor({ state: 'visible' });
    await waitForAllImages(dialog.locator('.stage img'));
    for (let step = 0; step < 20; step++) {
      await page.mouse.move(700 + (step % 3) * 20, 440 + (step % 2) * 16);
      if (await page.locator('dialog#photo-lightbox-dialog.ui-visible').isVisible()) {
        break;
      }
      await page.waitForTimeout(100);
    }
    await dialog.locator('aside.drawer').waitFor({ state: 'visible' });
    await page.waitForTimeout(300);
    await page.screenshot({ path: path.join(screenshotsDir, 'lightbox.png') });

    // 3. Library & Sync view screenshot
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Library & Sync' }).click();
    await page.getByRole('heading', { name: 'Library Coverage' }).waitFor({ state: 'visible' });
    await page.locator('section', { has: page.getByRole('heading', { name: /Duplicate Files Inspector/ }) }).locator('tbody tr').first().waitFor({ state: 'visible' });
    await page.waitForTimeout(250);
    await page.screenshot({ path: path.join(screenshotsDir, 'library.png') });

    // 4. Interactive GPS Map bounds popover screenshot
    await page.getByRole('button', { name: 'Gallery' }).click();
    await page.locator('button.photo-card').first().waitFor({ state: 'visible' });
    await page.getByRole('button', { name: 'GPS Bounds' }).click();
    const mapStage = page.locator('div.map-stage');
    await mapStage.waitFor({ state: 'visible' });
    await page.waitForFunction(() => {
      const picker = document
        .querySelector('photo-tracker-app')
        ?.shadowRoot?.querySelector('photo-gallery-view')
        ?.shadowRoot?.querySelector('photo-gps-map-picker');
      const imgs = Array.from(picker?.shadowRoot?.querySelectorAll('img.map-tile') ?? []);
      return imgs.length > 0 && imgs.every((img) => img.complete);
    }, { timeout: 10_000 }).catch(() => {});
    await page.waitForTimeout(300);
    const box = await mapStage.boundingBox();
    if (box) {
      const x1 = box.x + box.width * 0.25;
      const y1 = box.y + box.height * 0.28;
      const x2 = box.x + box.width * 0.56;
      const y2 = box.y + box.height * 0.68;
      await page.mouse.move(x1, y1);
      await page.mouse.down();
      await page.mouse.move(x2, y2, { steps: 10 });
      await page.mouse.up();
    }
    await page.waitForTimeout(300);
    await page.screenshot({ path: path.join(screenshotsDir, 'gps-map.png') });
    await context.close();

    // 5. Login view screenshot (separate auth-enabled temp server)
    const authSrv = await startServer({ authDisabled: false, password: 'demo-password' });
    try {
      const authCtx = await browser.newContext({
        viewport: { width: 1280, height: 720 },
        deviceScaleFactor: 2,
        colorScheme: 'light',
      });
      const authPage = await authCtx.newPage();
      await authPage.goto(`${authSrv.baseUrl}/login`);
      await authPage.getByRole('heading', { name: 'Sign in to Photo View Tracker' }).waitFor({ state: 'visible' });
      await authPage.waitForTimeout(200);
      await authPage.screenshot({ path: path.join(screenshotsDir, 'login.png') });
      await authCtx.close();
    } finally {
      await authSrv.stop();
    }
  } finally {
    await browser.close();
    await srv.stop();
  }
  console.log('Captured screenshots in', screenshotsDir);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
