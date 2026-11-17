import {gzipSync} from 'node:zlib';
import {readFileSync} from 'node:fs';
import path from 'node:path';

const buildDir = path.resolve('dist');
const html = readFileSync(path.join(buildDir, 'index.html'), 'utf8');
const entryPath = html.match(/<script[^>]+src="([^"]+)"/)?.[1];
if (!entryPath) throw new Error('Could not identify the initial JavaScript entry in dist/index.html.');

const entry = readFileSync(path.join(buildDir, entryPath.replace(/^\//, '')));
const compressedBytes = gzipSync(entry).byteLength;
const maximumBytes = 250_000;
console.log(`Initial JavaScript gzip: ${(compressedBytes / 1_000).toFixed(2)} KB / 250 KB`);
if (compressedBytes > maximumBytes) {
  throw new Error(`Initial JavaScript exceeds the 250 KB gzip limit by ${compressedBytes - maximumBytes} bytes.`);
}
