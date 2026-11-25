import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
export const EXAMPLE_PHOTOS_DIR = path.resolve(__dirname, '..', '..', 'examples', 'photos');

function copyFixture(srcRelative: string, destDir: string, destRelative: string): void {
  const src = path.join(EXAMPLE_PHOTOS_DIR, srcRelative);
  const dst = path.join(destDir, destRelative);
  fs.mkdirSync(path.dirname(dst), { recursive: true });
  fs.copyFileSync(src, dst);
}

/**
 * Seeds an isolated temporary library directory with:
 * - IMG_0042.CR3 + IMG_0042.JPG (stacked RAW + JPEG pair)
 * - Mountain_Lake.jpg + Backup/Mountain_Lake_Copy.jpg (exact SHA-256 duplicate)
 * - Forest_Trail.heic (HEIC photo)
 * - Street_Cafe.jpg (standard JPEG)
 * - Night_Skyline.png (PNG)
 * Total: 7 files -> 5 logical photos.
 */
export function seedSampleLibrary(libraryDir: string): void {
  copyFixture('2026/Vacation/Beach_Sunset.CR3', libraryDir, '2026/Trip/IMG_0042.CR3');
  copyFixture('2026/Vacation/Beach_Sunset.jpg', libraryDir, '2026/Trip/IMG_0042.JPG');
  copyFixture('2026/Vacation/Mountain_Lake.jpg', libraryDir, '2026/Trip/Mountain_Lake.jpg');
  copyFixture('2026/Backup/Mountain_Lake_Copy.jpg', libraryDir, '2026/Backup/Mountain_Lake_Copy.jpg');
  copyFixture('2026/Nature/Forest_Trail.heic', libraryDir, '2026/Nature/Forest_Trail.heic');
  copyFixture('2026/City/Street_Cafe.jpg', libraryDir, '2026/City/Street_Cafe.jpg');
  copyFixture('2026/City/Night_Skyline.png', libraryDir, '2026/City/Night_Skyline.png');
}
