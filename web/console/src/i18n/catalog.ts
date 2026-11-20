import source from './source.json';

export const localeOrder = [
  'zh-Hans', 'zh-Hant', 'de', 'en', 'es', 'fr', 'id', 'it', 'ja', 'ko', 'pt', 'ru', 'th', 'tr', 'vi',
] as const;

export type Locale = typeof localeOrder[number];
export type MessageCatalog = Record<string, string>;
export type MessageKey = keyof typeof source;

export const localeNames: Record<Locale, string> = {
  'zh-Hans': '中文(简体)', 'zh-Hant': '中文(繁體)', de: 'Deutsch', en: 'English', es: 'Español', fr: 'Français',
  id: 'Indonesia', it: 'Italiano', ja: '日本語', ko: '한국어', pt: 'Português', ru: 'Русский', th: 'ภาษาไทย', tr: 'Türkçe', vi: 'Tiếng Việt',
};

export const sourceCatalog = source as MessageCatalog;
