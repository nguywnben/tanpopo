import {readdirSync, readFileSync, statSync, writeFileSync} from 'node:fs';
import path from 'node:path';
import {gzipSync} from 'node:zlib';

const distDir = path.resolve('dist');
const compressibleExtensions = new Set(['.js', '.css', '.html', '.svg', '.json', '.txt']);

function walk(directory) {
  for (const entry of readdirSync(directory)) {
    const fullPath = path.join(directory, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      walk(fullPath);
      continue;
    }
    if (fullPath.endsWith('.gz')) continue;
    if (!compressibleExtensions.has(path.extname(fullPath).toLowerCase())) continue;
    const raw = readFileSync(fullPath);
    writeFileSync(`${fullPath}.gz`, gzipSync(raw, {level: 9}));
  }
}

walk(distDir);
console.log('Generated gzip siblings in web/console/dist');
