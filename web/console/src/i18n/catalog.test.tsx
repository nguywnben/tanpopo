import {cleanup, fireEvent, render, screen} from '@testing-library/react';
import {afterEach, describe, expect, it} from 'vitest';
import {localeNames, localeOrder, messages, sourceCatalog} from './catalog';
import {I18nProvider, LanguagePicker, localeForLanguageTag, resolveBrowserLocale, useI18n} from './I18nProvider';
import {formatDateAndTime, formatNumber} from './format';

function LocaleProbe() {
  const {locale} = useI18n();
  return <><output>{locale}</output><LanguagePicker /></>;
}

afterEach(() => {
  cleanup();
  localStorage.clear();
});

describe('console locale catalogs', () => {
  it('keeps the required language order and labels', () => {
    expect(localeOrder).toEqual(['zh-Hans', 'zh-Hant', 'de', 'en', 'es', 'fr', 'id', 'it', 'ja', 'ko', 'pt', 'ru', 'th', 'tr', 'vi']);
    expect(Object.keys(localeNames)).toEqual(localeOrder);
  });

  it('contains every source key in every shipped locale and preserves interpolation tokens', () => {
    const keys = Object.keys(sourceCatalog).sort();
    for (const locale of localeOrder) {
      const catalog = messages[locale];
      for (const key of keys) {
        const translated = catalog[key as keyof typeof sourceCatalog] ?? (locale === 'en' ? sourceCatalog[key as keyof typeof sourceCatalog] : undefined);
        expect(translated, `${locale}: ${key}`).toBeTruthy();
        const placeholders = (value: string) => [...value.matchAll(/\{([\w.]+)\}/g)].map(match => match[1]).sort();
        expect(placeholders(translated ?? '')).toEqual(placeholders(sourceCatalog[key as keyof typeof sourceCatalog]));
      }
    }
  });

  it('distinguishes Simplified and Traditional Chinese and chooses browser priority order', () => {
    expect(localeForLanguageTag('zh-CN')).toBe('zh-Hans');
    expect(localeForLanguageTag('zh-TW')).toBe('zh-Hant');
    expect(localeForLanguageTag('zh-HK')).toBe('zh-Hant');
    expect(resolveBrowserLocale(['fr-CA', 'zh-CN'])).toBe('fr');
    expect(resolveBrowserLocale(['de-DE', 'ja-JP'])).toBe('de');
    expect(resolveBrowserLocale(['nl-NL', 'en-GB'])).toBe('en');
  });

  it('persists a manual choice and returns to browser-selected Auto', () => {
    render(<I18nProvider><LocaleProbe /></I18nProvider>);
    const picker = screen.getByRole('combobox');
    fireEvent.change(picker, {target: {value: 'vi'}});
    expect(screen.getByText('vi')).toBeInTheDocument();
    expect(localStorage.getItem('tanpopo.console.locale')).toBe('vi');
    expect(document.documentElement.lang).toBe('vi');

    cleanup();
    render(<I18nProvider><LocaleProbe /></I18nProvider>);
    expect(screen.getByText('vi')).toBeInTheDocument();
    fireEvent.change(screen.getByRole('combobox'), {target: {value: 'auto'}});
    expect(localStorage.getItem('tanpopo.console.locale')).toBe('auto');
    expect(screen.getByText('en')).toBeInTheDocument();
  });

  it('formats counts and dates using the active locale', () => {
    document.documentElement.lang = 'de';
    expect(formatNumber(1234567.5)).toBe('1.234.567,5');
    expect(formatDateAndTime('2026-11-20T08:00:00Z')).toBe(new Intl.DateTimeFormat('de', {dateStyle: 'medium', timeStyle: 'short'}).format(new Date('2026-11-20T08:00:00Z')));
    document.documentElement.lang = 'en';
  });
});
