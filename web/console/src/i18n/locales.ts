export const SUPPORTED_LOCALES = [
  {code: 'zh-Hans', nativeName: '中文(简体)'},
  {code: 'zh-Hant', nativeName: '中文(繁體)'},
  {code: 'de', nativeName: 'Deutsch'},
  {code: 'en', nativeName: 'English'},
  {code: 'es', nativeName: 'Español'},
  {code: 'fr', nativeName: 'Français'},
  {code: 'id', nativeName: 'Indonesia'},
  {code: 'it', nativeName: 'Italiano'},
  {code: 'ja', nativeName: '日本語'},
  {code: 'ko', nativeName: '한국어'},
  {code: 'pt', nativeName: 'Português'},
  {code: 'ru', nativeName: 'Русский'},
  {code: 'th', nativeName: 'ภาษาไทย'},
  {code: 'tr', nativeName: 'Türkçe'},
  {code: 'vi', nativeName: 'Tiếng Việt'},
] as const;

export type LocaleCode = (typeof SUPPORTED_LOCALES)[number]['code'];
export type LocalePreference = 'auto' | LocaleCode;

export const LOCALE_STORAGE_KEY = 'tanpopo-console-locale';

const supportedCodeSet = new Set<string>(SUPPORTED_LOCALES.map(item => item.code));

export function isSupportedLocale(value: string | null | undefined): value is LocaleCode {
  return typeof value === 'string' && supportedCodeSet.has(value);
}

/**
 * Normalizes a BCP-47 language tag (or POSIX locale) into one of the 15
 * supported Tanpopo locales, properly distinguishing Simplified Chinese
 * (zh-Hans, zh-CN, zh-SG) from Traditional Chinese (zh-Hant, zh-TW, zh-HK, zh-MO).
 */
export function parseLocaleTag(raw: string | null | undefined): LocaleCode | null {
  if (!raw) return null;
  const cleaned = raw.trim().split(/[.@]/, 1)[0].replaceAll('_', '-').toLowerCase();
  if (!cleaned || cleaned === 'c' || cleaned === 'posix' || cleaned === 'auto') {
    return null;
  }

  const parts = cleaned.split('-');
  const lang = parts[0];

  if (lang === 'in') {
    return 'id';
  }

  if (lang === 'zh') {
    for (const sub of parts.slice(1)) {
      if (sub === 'hant' || sub === 'tw' || sub === 'hk' || sub === 'mo') {
        return 'zh-Hant';
      }
      if (sub === 'hans' || sub === 'cn' || sub === 'sg') {
        return 'zh-Hans';
      }
    }
    return 'zh-Hans';
  }

  switch (lang) {
    case 'de': return 'de';
    case 'en': return 'en';
    case 'es': return 'es';
    case 'fr': return 'fr';
    case 'id': return 'id';
    case 'it': return 'it';
    case 'ja': return 'ja';
    case 'ko': return 'ko';
    case 'pt': return 'pt';
    case 'ru': return 'ru';
    case 'th': return 'th';
    case 'tr': return 'tr';
    case 'vi': return 'vi';
    default: return null;
  }
}

export function resolveBrowserLocale(languages?: readonly string[]): LocaleCode {
  const candidates =
    languages ??
    (typeof navigator !== 'undefined'
      ? (Array.isArray(navigator.languages) && navigator.languages.length > 0
          ? navigator.languages
          : navigator.language
            ? [navigator.language]
            : [])
      : []);

  for (const tag of candidates) {
    const matched = parseLocaleTag(tag);
    if (matched) return matched;
  }
  return 'en';
}

export function loadStoredLocalePreference(): LocalePreference {
  if (typeof window === 'undefined') return 'auto';
  try {
    const saved = window.localStorage.getItem(LOCALE_STORAGE_KEY);
    if (saved === 'auto') return 'auto';
    if (isSupportedLocale(saved)) return saved;
  } catch {
    return 'auto';
  }
  return 'auto';
}

export function saveLocalePreference(preference: LocalePreference) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, preference);
  } catch {
    // Ignore storage restrictions in private browsing modes.
  }
}

export function resolveEffectiveLocale(preference: LocalePreference, browserLanguages?: readonly string[]): LocaleCode {
  return preference === 'auto' ? resolveBrowserLocale(browserLanguages) : preference;
}
