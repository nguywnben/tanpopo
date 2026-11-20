import type {Locale} from './catalog';

let activeLocale: Locale = 'en';

export function setActiveLocale(locale: Locale) {
  activeLocale = locale;
}

export function getActiveLocale(): Locale {
  return activeLocale;
}
