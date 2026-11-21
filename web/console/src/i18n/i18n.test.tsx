import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import {afterEach, beforeEach, describe, expect, it} from 'vitest';
import {ThemeProvider} from '../styles/theme';
import {
  formatLocalizedMicroUSD,
  formatLocalizedNumber,
  preloadLocale,
} from './catalog';
import {I18nProvider, getActiveLocale, useI18n} from './I18nProvider';
import {LanguageSelector} from './LanguageSelector';
import {
  LOCALE_STORAGE_KEY,
  SUPPORTED_LOCALES,
  loadStoredLocalePreference,
  parseLocaleTag,
  resolveBrowserLocale,
} from './locales';

const EXPECTED_NATIVE_NAMES = [
  '中文(简体)',
  '中文(繁體)',
  'Deutsch',
  'English',
  'Español',
  'Français',
  'Indonesia',
  'Italiano',
  '日本語',
  '한국어',
  'Português',
  'Русский',
  'ภาษาไทย',
  'Türkçe',
  'Tiếng Việt',
];

function ProbeView() {
  const {locale, t, setLocale} = useI18n();
  return (
    <div>
      <span data-testid="locale">{locale}</span>
      <span data-testid="revoke">{t('access.action.revoke')}</span>
      <span data-testid="alerts">{t('overview.activeAlertsTitle')}</span>
      <span data-testid="circuit">{t('state.CIRCUIT_OPEN_UNHEALTHY')}</span>
      <LanguageSelector />
      <button type="button" onClick={() => setLocale('ja')}>
        pick-ja
      </button>
    </div>
  );
}

function renderWithProviders() {
  return render(
    <ThemeProvider>
      <I18nProvider>
        <ProbeView />
      </I18nProvider>
    </ThemeProvider>,
  );
}

function mockBrowserLanguages(languages: readonly string[]) {
  Object.defineProperty(navigator, 'languages', {
    configurable: true,
    value: languages,
  });
  Object.defineProperty(navigator, 'language', {
    configurable: true,
    value: languages[0] ?? '',
  });
}

describe('i18n locale resolution & LanguageSelector', () => {
  const originalLanguages = Object.getOwnPropertyDescriptor(navigator, 'languages');
  const originalLanguage = Object.getOwnPropertyDescriptor(navigator, 'language');

  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    if (originalLanguages) {
      Object.defineProperty(navigator, 'languages', originalLanguages);
    }
    if (originalLanguage) {
      Object.defineProperty(navigator, 'language', originalLanguage);
    }
    document.documentElement.lang = 'en';
  });

  it('exposes all 15 supported locales in the exact required native order', () => {
    expect(SUPPORTED_LOCALES.map(item => item.nativeName)).toEqual(EXPECTED_NATIVE_NAMES);
  });

  it('distinguishes Simplified and Traditional Chinese and falls back cleanly', () => {
    expect(parseLocaleTag('zh-CN')).toBe('zh-Hans');
    expect(parseLocaleTag('zh-SG')).toBe('zh-Hans');
    expect(parseLocaleTag('zh-Hans-CN')).toBe('zh-Hans');
    expect(parseLocaleTag('zh-TW')).toBe('zh-Hant');
    expect(parseLocaleTag('zh-HK')).toBe('zh-Hant');
    expect(parseLocaleTag('zh-MO')).toBe('zh-Hant');
    expect(parseLocaleTag('zh-Hant-TW')).toBe('zh-Hant');
    expect(parseLocaleTag('in-ID')).toBe('id');
    expect(parseLocaleTag('vi-VN')).toBe('vi');
    expect(parseLocaleTag('xx-YY')).toBeNull();

    expect(resolveBrowserLocale(['sv-SE', 'zh-TW', 'en-US'])).toBe('zh-Hant');
    expect(resolveBrowserLocale(['nl-NL', 'zh-CN', 'ja-JP'])).toBe('zh-Hans');
    expect(resolveBrowserLocale(['vi-VN', 'en-US'])).toBe('vi');
    expect(resolveBrowserLocale(['sv-SE', 'nb-NO'])).toBe('en');
  });

  it('auto-detects browser language without writing storage, handles legacy auto, and persists manual selection', async () => {
    mockBrowserLanguages(['vi-VN', 'en-US']);
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'auto');

    // Legacy "auto" is treated as no manual selection.
    expect(loadStoredLocalePreference()).toBeNull();

    await preloadLocale('vi');
    const {unmount} = renderWithProviders();

    await waitFor(() => {
      expect(screen.getByTestId('locale')).toHaveTextContent('vi');
      expect(screen.getByTestId('revoke')).toHaveTextContent('Thu hồi khóa');
      expect(screen.getByTestId('alerts')).toHaveTextContent('Cảnh báo hiện tại');
      expect(screen.getByTestId('circuit')).toHaveTextContent('Đã ngắt mạch');
    });
    expect(document.documentElement.lang).toBe('vi');
    expect(getActiveLocale()).toBe('vi');

    // Selector trigger shows active native name and marks it selected in menu without Auto.
    const trigger = screen.getByRole('combobox', {name: /Ngôn ngữ/i});
    expect(trigger).toHaveTextContent('Tiếng Việt');
    fireEvent.click(trigger);

    const options = await screen.findAllByRole('option');
    expect(options.map(opt => opt.textContent?.trim() ?? '')).toEqual(EXPECTED_NATIVE_NAMES);
    const selectedOption = options.find(opt => opt.getAttribute('aria-selected') === 'true');
    expect(selectedOption?.textContent?.trim()).toBe('Tiếng Việt');

    // Selecting manually persists to localStorage and survives reload.
    fireEvent.click(screen.getByText('pick-ja'));
    await waitFor(() => {
      expect(screen.getByTestId('locale')).toHaveTextContent('ja');
      expect(screen.getByRole('combobox')).toHaveTextContent('日本語');
    });
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('ja');
    expect(loadStoredLocalePreference()).toBe('ja');

    unmount();

    renderWithProviders();
    await waitFor(() => {
      expect(screen.getByTestId('locale')).toHaveTextContent('ja');
      expect(screen.getByRole('combobox')).toHaveTextContent('日本語');
    });
  });

  it('falls back to English when browser has only unsupported languages and does not write localStorage', async () => {
    mockBrowserLanguages(['sv-SE', 'nb-NO']);
    renderWithProviders();

    expect(screen.getByTestId('locale')).toHaveTextContent('en');
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBeNull();

    const trigger = screen.getByRole('combobox', {name: /Language/i});
    expect(trigger).toHaveTextContent('English');
    fireEvent.click(trigger);

    const options = await screen.findAllByRole('option');
    expect(options).toHaveLength(15);
    expect(options.map(opt => opt.textContent?.trim() ?? '')).toEqual(EXPECTED_NATIVE_NAMES);
    const selectedOption = options.find(opt => opt.getAttribute('aria-selected') === 'true');
    expect(selectedOption?.textContent?.trim()).toBe('English');
  });

  it('formats exact integer micro-USD amounts without float drift across locales', () => {
    expect(formatLocalizedMicroUSD('en', 1_250_000, 'Unavailable')).toBe('$1.250000');
    expect(formatLocalizedMicroUSD('en', -500, 'Unavailable')).toBe('-$0.000500');
    expect(formatLocalizedMicroUSD('de', 1_250_000, 'Nicht verfügbar')).toContain('1,250000');
    expect(formatLocalizedNumber('de', 12345)).toBe('12.345');
  });

  it('loads all 15 locale catalogs cleanly offline', async () => {
    const enCat = await preloadLocale('en');
    const enKeys = Object.keys(enCat).sort();

    for (const item of SUPPORTED_LOCALES) {
      const cat = await preloadLocale(item.code);
      expect(Object.keys(cat).sort()).toEqual(enKeys);
    }
  });
});
