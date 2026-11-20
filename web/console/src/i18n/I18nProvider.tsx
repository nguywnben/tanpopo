import {createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode} from 'react';
import {localeNames, localeOrder, type Locale} from './catalog';
import {loadLocaleMessages} from './messages';
import {setActiveLocale} from './locale-state';
import {setLocaleMessages, translateUi} from './translate';

const STORAGE_KEY = 'tanpopo.console.locale';
type Preference = Locale | 'auto';
type I18nValue = {locale: Locale; preference: Preference; setPreference: (value: Preference) => void; t: (key: string) => string};
const I18nContext = createContext<I18nValue | null>(null);

export function localeForLanguageTag(tag: string): Locale | undefined {
  const normalized = tag.trim().replaceAll('_', '-').toLowerCase();
  if (!normalized) return undefined;
  if (normalized === 'zh' || normalized.startsWith('zh-cn') || normalized.startsWith('zh-sg') || normalized.startsWith('zh-hans')) return 'zh-Hans';
  if (normalized.startsWith('zh-tw') || normalized.startsWith('zh-hk') || normalized.startsWith('zh-mo') || normalized.startsWith('zh-hant')) return 'zh-Hant';
  const primary = normalized.split('-')[0];
  return localeOrder.find(locale => locale.toLowerCase() === primary);
}

export function resolveBrowserLocale(languages: readonly string[]): Locale {
  for (const language of languages) {
    const match = localeForLanguageTag(language);
    if (match) return match;
  }
  return 'en';
}

function initialPreference(): Preference {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored === 'auto') return 'auto';
    if (stored && localeOrder.includes(stored as Locale)) return stored as Locale;
  } catch { /* Storage can be disabled by browser policy; the language remains usable. */ }
  return 'auto';
}

export function I18nProvider({children}: {children: ReactNode}) {
  const [preference, setPreferenceState] = useState<Preference>(initialPreference);
  const browserLanguages = typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];
  const locale = preference === 'auto' ? resolveBrowserLocale(browserLanguages) : preference;
  const [translationRevision, setTranslationRevision] = useState(0);
  setActiveLocale(locale);
  const setPreference = useCallback((next: Preference) => {
    setPreferenceState(next);
    try { window.localStorage.setItem(STORAGE_KEY, next); } catch { /* In-memory preference still applies. */ }
  }, []);

  useEffect(() => {
    document.documentElement.lang = locale;
    let current = true;
    setLocaleMessages(locale, {});
    loadLocaleMessages(locale).then(catalog => {
      if (!current) return;
      setLocaleMessages(locale, catalog);
      setTranslationRevision(value => value + 1);
    });
    return () => { current = false; };
  }, [locale]);

  const t = useCallback((key: string) => translateUi(key), [translationRevision]);
  const value = useMemo(() => ({locale, preference, setPreference, t}), [locale, preference, setPreference, t]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const context = useContext(I18nContext);
  if (!context) throw new Error('useI18n must be used within I18nProvider');
  return context;
}

export function LanguagePicker({className}: {className?: string}) {
  const {preference, setPreference, t} = useI18n();
  return (
    <label className={className ?? 'tp-language-picker'}>
      <span className="sr-only">{t('Language')}</span>
      <select aria-label={t('Language')} value={preference} onChange={event => setPreference(event.target.value as Preference)}>
        <option value="auto">{t('Auto')}</option>
        {localeOrder.map(locale => <option key={locale} value={locale}>{localeNames[locale]}</option>)}
      </select>
    </label>
  );
}
