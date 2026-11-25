import { spawn, type ChildProcess } from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { seedSampleLibrary } from './fixtures';

export interface StartServerOptions {
  authDisabled?: boolean;
  password?: string;
}

export interface RunningTestServer {
  baseUrl: string;
  tempDir: string;
  libraryDir: string;
  cacheDir: string;
  dbPath: string;
  stop: () => Promise<void>;
}

function getFreePort(): Promise<number> {
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
      const { port } = addr;
      srv.close((err) => (err ? reject(err) : resolve(port)));
    });
  });
}

async function waitForHealth(baseUrl: string, proc: ChildProcess, logs: () => string): Promise<void> {
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    if (proc.exitCode !== null) {
      throw new Error(`phototracker exited early with code ${proc.exitCode}\n${logs()}`);
    }
    try {
      const res = await fetch(`${baseUrl}/api/v1/health`);
      if (res.ok) {
        return;
      }
    } catch {
      // Server not listening yet.
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`Timed out waiting for ${baseUrl}/api/v1/health\n${logs()}`);
}

export async function startTestServer(opts: StartServerOptions = {}): Promise<RunningTestServer> {
  const binary = process.env.E2E_BINARY;
  if (!binary || !fs.existsSync(binary)) {
    throw new Error(`E2E_BINARY not found: ${binary ?? '<unset>'}`);
  }

  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'phototracker-e2e-'));
  const libraryDir = path.join(tempDir, 'library');
  const cacheDir = path.join(tempDir, 'cache');
  const dbPath = path.join(tempDir, 'photos.db');
  fs.mkdirSync(libraryDir, { recursive: true });
  fs.mkdirSync(cacheDir, { recursive: true });
  seedSampleLibrary(libraryDir);

  const port = await getFreePort();
  const baseUrl = `http://127.0.0.1:${port}`;
  const authDisabled = opts.authDisabled ?? true;
  const password = opts.password ?? 'e2e-secret';

  const output: string[] = [];
  const proc = spawn(binary, [], {
    cwd: tempDir, // Avoid reading repository root .env
    env: {
      ...process.env,
      LIBRARY_PATH: libraryDir,
      DB_PATH: dbPath,
      CACHE_PATH: cacheDir,
      HOST: '127.0.0.1',
      PORT: String(port),
      AUTH_DISABLED: String(authDisabled),
      AUTH_PASSWORD: password,
      AUTH_PASSWORD_HASH: '',
      AUTH_SECRET: '',
      COOKIE_SECURE: 'false',
      LOG_FORMAT: 'text',
      LOG_LEVEL: 'warn',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  proc.stdout.on('data', (chunk: Buffer) => output.push(chunk.toString('utf8')));
  proc.stderr.on('data', (chunk: Buffer) => output.push(chunk.toString('utf8')));

  const getLogs = () => output.join('');
  try {
    await waitForHealth(baseUrl, proc, getLogs);
  } catch (err) {
    proc.kill('SIGKILL');
    fs.rmSync(tempDir, { recursive: true, force: true });
    throw err;
  }

  const stop = async (): Promise<void> => {
    if (proc.exitCode === null) {
      await new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          proc.kill('SIGKILL');
          resolve();
        }, 5_000);
        proc.once('exit', () => {
          clearTimeout(timer);
          resolve();
        });
        proc.kill('SIGTERM');
      });
    }
    fs.rmSync(tempDir, { recursive: true, force: true });
  };

  return { baseUrl, tempDir, libraryDir, cacheDir, dbPath, stop };
}

export async function syncLibraryViaAPI(baseUrl: string): Promise<void> {
  const res = await fetch(`${baseUrl}/api/v1/library/sync`, {
    method: 'POST',
    headers: { Origin: baseUrl },
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`POST /api/v1/library/sync failed (${res.status}): ${body}`);
  }
}

export async function revealLightboxHUD(page: Page): Promise<void> {
  const visibleDialog = page.locator('dialog#photo-lightbox-dialog.ui-visible');
  const deadline = performance.now() + 10_000;
  let step = 0;
  while (performance.now() < deadline) {
    const x = 420 + (step % 3) * 24;
    const y = 420 + (step % 2) * 18;
    await page.mouse.move(x, y);
    if (await visibleDialog.isVisible()) {
      return;
    }
    step += 1;
    await page.waitForTimeout(100);
  }
  await visibleDialog.waitFor({ state: 'visible' });
}

export async function waitForImageLoaded(img: Locator): Promise<void> {
  await img.waitFor({ state: 'visible' });
  await img.evaluate(
    (el) =>
      new Promise<void>((resolve, reject) => {
        const image = el as HTMLImageElement;
        if (image.complete) {
          if (image.naturalWidth > 0) {
            resolve();
          } else {
            reject(new Error(`Image failed to decode: ${image.src}`));
          }
          return;
        }
        image.addEventListener(
          'load',
          () => (image.naturalWidth > 0 ? resolve() : reject(new Error(`Zero-width image: ${image.src}`))),
          { once: true },
        );
        image.addEventListener('error', () => reject(new Error(`Image error event: ${image.src}`)), { once: true });
      }),
  );
}
