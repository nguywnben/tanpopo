import {cpSync, existsSync, mkdirSync, rmSync} from 'node:fs';
import path from 'node:path';

const sourceDir = path.resolve('dist');
const targetDir = path.resolve('../../internal/webui/dist');

if (!existsSync(sourceDir)) {
  throw new Error('web/console/dist does not exist. Run npm run build first.');
}

rmSync(targetDir, {recursive: true, force: true});
mkdirSync(targetDir, {recursive: true});
cpSync(sourceDir, targetDir, {recursive: true});
console.log(`Synced embedded Web Console assets to ${targetDir}`);
