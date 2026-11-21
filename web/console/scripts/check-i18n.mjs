import {readFileSync, readdirSync, existsSync} from 'node:fs';
import {join, resolve} from 'node:path';

const REQUIRED_LOCALES = [
  'zh-Hans',
  'zh-Hant',
  'de',
  'en',
  'es',
  'fr',
  'id',
  'it',
  'ja',
  'ko',
  'pt',
  'ru',
  'th',
  'tr',
  'vi',
];

const consoleRoot = resolve(import.meta.dirname, '..');
const repoRoot = resolve(consoleRoot, '..', '..');
const webLocalesDir = join(consoleRoot, 'src', 'i18n', 'locales');
const goLocalesDir = join(repoRoot, 'internal', 'pkg', 'i18n', 'locales');
const guidesDir = join(repoRoot, 'docs', 'guides');

const onlyEn = process.argv.includes('--en-only');
const targetLocales = onlyEn ? ['en'] : REQUIRED_LOCALES;

const errors = [];

function extractPlaceholders(str) {
  return [...new Set([...str.matchAll(/\{\{(\w+)\}\}/g)].map(m => m[1]))].sort();
}

function walkFiles(dir, out = []) {
  for (const entry of readdirSync(dir, {withFileTypes: true})) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      walkFiles(full, out);
    } else if (/\.(ts|tsx)$/.test(entry.name) && !entry.name.endsWith('.test.ts') && !entry.name.endsWith('.test.tsx')) {
      out.push(full);
    }
  }
  return out;
}

function checkCatalogSet(label, dir) {
  const enPath = join(dir, 'en.json');
  if (!existsSync(enPath)) {
    errors.push(`[${label}] Missing canonical en.json at ${enPath}`);
    return null;
  }
  const en = JSON.parse(readFileSync(enPath, 'utf8'));
  const enKeys = Object.keys(en).sort();
  if (enKeys.length === 0) {
    errors.push(`[${label}] en.json is empty`);
    return null;
  }
  for (const key of enKeys) {
    if (typeof en[key] !== 'string' || en[key].trim() === '') {
      errors.push(`[${label}] en.json key "${key}" is empty or not a string`);
    }
  }

  for (const loc of targetLocales) {
    const filePath = join(dir, `${loc}.json`);
    if (!existsSync(filePath)) {
      errors.push(`[${label}] Missing locale catalog: ${loc}.json`);
      continue;
    }
    let data;
    try {
      data = JSON.parse(readFileSync(filePath, 'utf8'));
    } catch (err) {
      errors.push(`[${label}] Invalid JSON in ${loc}.json: ${err.message}`);
      continue;
    }
    const keys = Object.keys(data).sort();
    const keySet = new Set(keys);
    const enSet = new Set(enKeys);

    const missing = enKeys.filter(k => !keySet.has(k));
    const extra = keys.filter(k => !enSet.has(k));
    if (missing.length > 0) {
      errors.push(`[${label}] ${loc}.json missing ${missing.length} key(s): ${missing.slice(0, 8).join(', ')}`);
    }
    if (extra.length > 0) {
      errors.push(`[${label}] ${loc}.json has ${extra.length} extra key(s): ${extra.slice(0, 8).join(', ')}`);
    }

    for (const key of enKeys) {
      if (!keySet.has(key)) continue;
      const val = data[key];
      if (typeof val !== 'string' || val.trim() === '') {
        errors.push(`[${label}] ${loc}.json key "${key}" is empty or not a string`);
        continue;
      }
      const expectedVars = extractPlaceholders(en[key]);
      const actualVars = extractPlaceholders(val);
      if (expectedVars.join(',') !== actualVars.join(',')) {
        errors.push(
          `[${label}] ${loc}.json key "${key}" placeholder mismatch: expected [${expectedVars.join(', ')}], got [${actualVars.join(', ')}]`,
        );
      }
    }
  }
  return en;
}

const webEn = checkCatalogSet('web', webLocalesDir);
checkCatalogSet('go', goLocalesDir);

// Verify all static t('...') / translateRuntime('...') keys used in web/console/src exist in web en.json
if (webEn) {
  const srcFiles = walkFiles(join(consoleRoot, 'src'));
  const keyCallRe = /\b(?:t|translateRuntime)\(\s*['"]([a-zA-Z0-9_.-]+)['"]/g;
  const labelKeyRe = /\b(?:labelKey|titleKey|sectionKey)\s*:\s*['"]([a-zA-Z0-9_.-]+)['"]/g;
  const usedKeys = new Set();
  for (const file of srcFiles) {
    const content = readFileSync(file, 'utf8');
    for (const re of [keyCallRe, labelKeyRe]) {
      let match;
      while ((match = re.exec(content)) !== null) {
        usedKeys.add(match[1]);
      }
    }
  }
  for (const key of usedKeys) {
    if (!(key in webEn)) {
      errors.push(`[web-usage] Key "${key}" referenced in source is missing from web/console/src/i18n/locales/en.json`);
    }
  }
}

// Verify user guides
for (const loc of targetLocales) {
  const guidePath = join(guidesDir, loc, 'user-guide.md');
  if (!existsSync(guidePath)) {
    errors.push(`[guides] Missing user guide: docs/guides/${loc}/user-guide.md`);
  } else {
    const body = readFileSync(guidePath, 'utf8').trim();
    if (body.length < 400) {
      errors.push(`[guides] User guide docs/guides/${loc}/user-guide.md is unexpectedly short (${body.length} chars)`);
    }
  }
}

if (errors.length > 0) {
  console.error(`i18n check failed with ${errors.length} error(s):`);
  for (const err of errors) {
    console.error(`  - ${err}`);
  }
  process.exit(1);
}

console.log(`i18n check passed (${targetLocales.length} locale(s), web + go catalogs + user guides).`);
