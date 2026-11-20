import {describe, expect, it} from 'vitest';
import {isI18nSourceFile} from './vite-plugin';

describe('i18n Vite transform path filtering', () => {
  it('recognizes i18n modules from both Windows and POSIX Vite IDs', () => {
    expect(isI18nSourceFile('D:/a/tanpopo/web/console/src/i18n/I18nProvider.tsx')).toBe(true);
    expect(isI18nSourceFile('D:\\a\\tanpopo\\web\\console\\src\\i18n\\I18nProvider.tsx')).toBe(true);
    expect(isI18nSourceFile('/workspace/tanpopo/web/console/src/i18n/I18nProvider.tsx')).toBe(true);
  });

  it('does not exclude regular pages outside the i18n module directory', () => {
    expect(isI18nSourceFile('D:/a/tanpopo/web/console/src/pages/setup/SetupPage.tsx')).toBe(false);
  });
});
