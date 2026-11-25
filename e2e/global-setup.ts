import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..');

function resolveGoBinary(): string {
  for (const candidate of ['go', '/snap/bin/go', '/usr/local/go/bin/go']) {
    if (candidate === 'go' || fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return 'go';
}

export default async function globalSetup(): Promise<void> {
  const configured = process.env.E2E_BINARY;
  if (configured && fs.existsSync(configured)) {
    return;
  }

  const distIndex = path.join(repoRoot, 'web', 'dist', 'index.html');
  if (!fs.existsSync(distIndex)) {
    execFileSync('npm', ['--prefix', 'web', 'run', 'build'], {
      cwd: repoRoot,
      stdio: 'inherit',
    });
  }

  const outBinary = path.join(os.tmpdir(), 'phototracker-e2e-bin');
  execFileSync(resolveGoBinary(), ['build', '-o', outBinary, './cmd/phototracker'], {
    cwd: repoRoot,
    stdio: 'inherit',
  });
  process.env.E2E_BINARY = outBinary;
}
