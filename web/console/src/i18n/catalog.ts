import enCatalog from './locales/en.json';
import type {LocaleCode} from './locales';

export type MessageCatalog = Record<string, string>;
export type TranslationVars = Record<string, string | number | undefined>;

const enMessages: MessageCatalog = enCatalog;
const loadedCatalogs = new Map<LocaleCode, MessageCatalog>([['en', enMessages]]);

const localeLoaders: Record<Exclude<LocaleCode, 'en'>, () => Promise<{default: MessageCatalog}>> = {
  'zh-Hans': () => import('./locales/zh-Hans.json'),
  'zh-Hant': () => import('./locales/zh-Hant.json'),
  de: () => import('./locales/de.json'),
  es: () => import('./locales/es.json'),
  fr: () => import('./locales/fr.json'),
  id: () => import('./locales/id.json'),
  it: () => import('./locales/it.json'),
  ja: () => import('./locales/ja.json'),
  ko: () => import('./locales/ko.json'),
  pt: () => import('./locales/pt.json'),
  ru: () => import('./locales/ru.json'),
  th: () => import('./locales/th.json'),
  tr: () => import('./locales/tr.json'),
  vi: () => import('./locales/vi.json'),
};

export function getEnglishCatalog(): MessageCatalog {
  return enMessages;
}

export function getLoadedCatalog(locale: LocaleCode): MessageCatalog {
  return loadedCatalogs.get(locale) ?? enMessages;
}

export async function preloadLocale(locale: LocaleCode): Promise<MessageCatalog> {
  const cached = loadedCatalogs.get(locale);
  if (cached) return cached;
  if (locale === 'en') return enMessages;
  const mod = await localeLoaders[locale]();
  const catalog = mod.default;
  loadedCatalogs.set(locale, catalog);
  return catalog;
}

export function interpolateMessage(template: string, vars?: TranslationVars): string {
  if (!vars) return template;
  return template.replace(/\{\{(\w+)\}\}/g, (match, key: string) => {
    const value = vars[key];
    return value === undefined ? match : String(value);
  });
}

export function translateWithCatalog(
  catalog: MessageCatalog,
  key: string,
  vars?: TranslationVars,
): string {
  const raw = catalog[key] ?? enMessages[key] ?? key;
  return interpolateMessage(raw, vars);
}

export function formatLocalizedNumber(
  locale: LocaleCode,
  value: number,
  options?: Intl.NumberFormatOptions,
): string {
  try {
    return new Intl.NumberFormat(locale, options).format(value);
  } catch {
    return value.toLocaleString('en-US');
  }
}

export function formatLocalizedPercent(locale: LocaleCode, ratio: number, fractionDigits = 2): string {
  try {
    return new Intl.NumberFormat(locale, {
      style: 'percent',
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(ratio);
  } catch {
    return `${(ratio * 100).toFixed(fractionDigits)}%`;
  }
}

export function formatLocalizedDateTime(
  locale: LocaleCode,
  input: string | number | Date,
  options?: Intl.DateTimeFormatOptions,
): string {
  const date = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(date.getTime())) return String(input);
  try {
    return new Intl.DateTimeFormat(
      locale,
      options ?? {dateStyle: 'medium', timeStyle: 'medium'},
    ).format(date);
  } catch {
    return date.toLocaleString();
  }
}

export function formatLocalizedTime(locale: LocaleCode, input: string | number | Date): string {
  const date = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(date.getTime())) return String(input);
  try {
    return new Intl.DateTimeFormat(locale, {timeStyle: 'medium'}).format(date);
  } catch {
    return date.toLocaleTimeString();
  }
}

/**
 * Formats integer micro-USD amounts with exact BigInt precision (zero floating-point
 * rounding) while applying locale-appropriate thousands and decimal separators.
 */
export function formatLocalizedMicroUSD(locale: LocaleCode, value: number, unavailableLabel = 'Unavailable'): string {
  if (!Number.isSafeInteger(value)) return unavailableLabel;
  const amount = BigInt(value);
  const sign = amount < 0n ? '-' : '';
  const abs = amount < 0n ? -amount : amount;
  const whole = abs / 1_000_000n;
  const fraction = String(abs % 1_000_000n).padStart(6, '0');

  let decimalSep = '.';
  let wholeFormatted = whole.toLocaleString('en-US');
  try {
    const parts = new Intl.NumberFormat(locale).formatToParts(1234.5);
    decimalSep = parts.find(part => part.type === 'decimal')?.value ?? '.';
    wholeFormatted = new Intl.NumberFormat(locale).format(whole);
  } catch {
    // Keep US fallback separators.
  }
  return `${sign}$${wholeFormatted}${decimalSep}${fraction}`;
}
