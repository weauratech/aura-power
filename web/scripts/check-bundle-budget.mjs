/* global console */
import { gzipSync } from 'node:zlib';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const manifestPath = join('dist', '.vite', 'manifest.json');
const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
const entry = Object.values(manifest).find((chunk) => chunk.isEntry);
if (!entry?.file) throw new Error(`No JavaScript entry found in ${manifestPath}`);

const assetDir = join('dist', 'assets');
const javascript = readdirSync(assetDir)
  .filter((file) => file.endsWith('.js'))
  .map((file) => {
    const bytes = statSync(join(assetDir, file)).size;
    const gzipBytes = gzipSync(readFileSync(join(assetDir, file))).byteLength;
    return { file, bytes, gzipBytes };
  })
  .sort((a, b) => b.bytes - a.bytes);

const entryAsset = javascript.find(({ file }) => `assets/${file}` === entry.file);
if (!entryAsset) throw new Error(`Entry asset ${entry.file} was not emitted`);

const limits = {
  entryBytes: 100 * 1024,
  entryGzipBytes: 35 * 1024,
  chunkBytes: 400 * 1024,
};
const failures = [];
if (entryAsset.bytes > limits.entryBytes) failures.push(`entry ${entryAsset.bytes} > ${limits.entryBytes} bytes`);
if (entryAsset.gzipBytes > limits.entryGzipBytes) failures.push(`entry gzip ${entryAsset.gzipBytes} > ${limits.entryGzipBytes} bytes`);
for (const asset of javascript) {
  if (asset.bytes > limits.chunkBytes) failures.push(`${asset.file} ${asset.bytes} > ${limits.chunkBytes} bytes`);
}

console.log(JSON.stringify({ entry: entryAsset, largestChunks: javascript.slice(0, 8), limits }, null, 2));
if (failures.length) throw new Error(`Bundle budget exceeded:\n- ${failures.join('\n- ')}`);
