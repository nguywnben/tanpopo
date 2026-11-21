import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import {InternationalizationProvider, type ProviderMessagesByLocale} from '@astryxdesign/core/i18n';
import {
  ASTRYX_LOCALE_OVERRIDES,
  getLoadedAstryxMessages,
  preloadAstryxLocale,
  toAstryxLocaleTag,
} from './astryxLocales';
import {
  formatLocalizedDateTime,
  formatLocalizedMicroUSD,
  formatLocalizedNumber,
  formatLocalizedPercent,
  formatLocalizedTime,
  getLoadedCatalog,
  preloadLocale,
  translateWithCatalog,
  type MessageCatalog,
  type TranslationVars,
} from './catalog';
import {
  loadStoredLocalePreference,
  resolveEffectiveLocale,
  saveLocalePreference,
  type LocaleCode,
  type StoredLocale,
} from './locales';

export type I18nContextValue = {
  locale: LocaleCode;
  setLocale: (next: LocaleCode) => void;
  t: (key: string, vars?: TranslationVars) => string;
  formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string;
  formatPercent: (ratio: number, fractionDigits?: number) => string;
  formatDateTime: (input: string | number | Date, options?: Intl.DateTimeFormatOptions) => string;
  formatTime: (input: string | number | Date) => string;
  formatMicroUSD: (value: number) => string;
};

let activeLocaleRuntime: LocaleCode = resolveEffectiveLocale(loadStoredLocalePreference());

export function getActiveLocale(): LocaleCode {
  return activeLocaleRuntime;
}

export function translateRuntime(key: string, vars?: TranslationVars): string {
  return translateWithCatalog(getLoadedCatalog(activeLocaleRuntime), key, vars);
}

const defaultValue: I18nContextValue = {
  locale: 'en',
  setLocale: () => undefined,
  t: (key, vars) => translateWithCatalog(getLoadedCatalog('en'), key, vars),
  formatNumber: (value, options) => formatLocalizedNumber('en', value, options),
  formatPercent: (ratio, digits) => formatLocalizedPercent('en', ratio, digits),
  formatDateTime: (input, options) => formatLocalizedDateTime('en', input, options),
  formatTime: input => formatLocalizedTime('en', input),
  formatMicroUSD: value => formatLocalizedMicroUSD('en', value, translateWithCatalog(getLoadedCatalog('en'), 'common.unavailable')),
};

const I18nContext = createContext<I18nContextValue>(defaultValue);

export function I18nProvider({children}: {children: ReactNode}) {
  const [storedLocale, setStoredLocale] = useState<StoredLocale>(loadStoredLocalePreference);
  const [browserLanguages, setBrowserLanguages] = useState<readonly string[] | undefined>(undefined);

  const locale = useMemo(
    () => resolveEffectiveLocale(storedLocale, browserLanguages),
    [storedLocale, browserLanguages],
  );

  const [catalog, setCatalog] = useState<MessageCatalog>(() => getLoadedCatalog(locale));
  const [astryxMessages, setAstryxMessages] = useState<ProviderMessagesByLocale | undefined>(() =>
    getLoadedAstryxMessages(locale),
  );

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const onLanguageChange = () => {
      const langs = Array.isArray(navigator.languages) && navigator.languages.length > 0
        ? [...navigator.languages]
        : navigator.language
          ? [navigator.language]
          : [];
      setBrowserLanguages(langs);
    };
    window.addEventListener('languagechange', onLanguageChange);
    return () => window.removeEventListener('languagechange', onLanguageChange);
  }, []);

  useLayoutEffect(() => {
    activeLocaleRuntime = locale;
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale;
    }
  }, [locale]);

  useEffect(() => {
    let active = true;
    const immediate = getLoadedCatalog(locale);
    setCatalog(immediate);
    setAstryxMessages(getLoadedAstryxMessages(locale));
    if (locale !== 'en') {
      void preloadLocale(locale).then(loaded => {
        if (active) setCatalog(loaded);
      });
      void preloadAstryxLocale(locale).then(loaded => {
        if (active) setAstryxMessages(loaded);
      });
    }
    return () => {
      active = false;
    };
  }, [locale]);

  const setLocale = useCallback((next: LocaleCode) => {
    saveLocalePreference(next);
    setStoredLocale(next);
  }, []);

  const t = useCallback(
    (key: string, vars?: TranslationVars) => translateWithCatalog(catalog, key, vars),
    [catalog],
  );

  const formatNumber = useCallback(
    (value: number, options?: Intl.NumberFormatOptions) => formatLocalizedNumber(locale, value, options),
    [locale],
  );

  const formatPercent = useCallback(
    (ratio: number, fractionDigits = 2) => formatLocalizedPercent(locale, ratio, fractionDigits),
    [locale],
  );

  const formatDateTime = useCallback(
    (input: string | number | Date, options?: Intl.DateTimeFormatOptions) =>
      formatLocalizedDateTime(locale, input, options),
    [locale],
  );

  const formatTime = useCallback(
    (input: string | number | Date) => formatLocalizedTime(locale, input),
    [locale],
  );

  const formatMicroUSD = useCallback(
    (value: number) => formatLocalizedMicroUSD(locale, value, translateWithCatalog(catalog, 'common.unavailable')),
    [locale, catalog],
  );

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      setLocale,
      t,
      formatNumber,
      formatPercent,
      formatDateTime,
      formatTime,
      formatMicroUSD,
    }),
    [locale, setLocale, t, formatNumber, formatPercent, formatDateTime, formatTime, formatMicroUSD],
  );

  return (
    <I18nContext.Provider value={value}>
      <InternationalizationProvider
        locale={toAstryxLocaleTag(locale)}
        messages={astryxMessages}
        overrides={ASTRYX_LOCALE_OVERRIDES}
      >
        {children}
      </InternationalizationProvider>
    </I18nContext.Provider>
  );
}

export function useI18n() {
  return useContext(I18nContext);
}
