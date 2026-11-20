import {gzipSync} from 'node:zlib';
import {readFileSync} from 'node:fs';
import path from 'node:path';

const buildDir = path.resolve('dist');
const html = readFileSync(path.join(buildDir, 'index.html'), 'utf8');
const scriptPaths = [...html.matchAll(/<script[^>]+src="([^"]+)"/g)].map(match => match[1]);
if (scriptPaths.length === 0) {
  throw new Error('Could not identify any initial JavaScript entry in dist/index.html.');
}

let compressedBytes = 0;
for (const scriptPath of scriptPaths) {
  const entry = readFileSync(path.join(buildDir, scriptPath.replace(/^\//, '')));
  compressedBytes += gzipSync(entry).byteLength;
}

const maximumBytes = 250_000;
console.log(`Initial JavaScript gzip (${scriptPaths.join(' + ')}): ${(compressedBytes / 1_000).toFixed(2)} KB / 250 KB`);
if (compressedBytes > maximumBytes) {
  throw new Error(`Initial JavaScript exceeds the 250 KB gzip limit by ${compressedBytes - maximumBytes} bytes.`);
}
