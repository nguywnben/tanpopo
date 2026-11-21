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
  const {preference, locale, t, setPreference} = useI18n();
  return (
    <div>
      <span data-testid="pref">{preference}</span>
      <span data-testid="locale">{locale}</span>
      <span data-testid="revoke">{t('access.action.revoke')}</span>
      <span data-testid="alerts">{t('overview.activeAlertsTitle')}</span>
      <span data-testid="circuit">{t('state.CIRCUIT_OPEN_UNHEALTHY')}</span>
      <LanguageSelector />
      <button type="button" onClick={() => setPreference('vi')}>
        pick-vi
      </button>
      <button type="button" onClick={() => setPreference('zh-Hant')}>
        pick-zh-hant
      </button>
      <button type="button" onClick={() => setPreference('auto')}>
        pick-auto
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
    expect(resolveBrowserLocale(['sv-SE', 'nb-NO'])).toBe('en');
  });

  it('defaults to Auto, resolves browser preference, persists manual choice, and restores Auto', async () => {
    Object.defineProperty(navigator, 'languages', {
      configurable: true,
      value: ['fr-FR', 'en-US'],
    });
    Object.defineProperty(navigator, 'language', {
      configurable: true,
      value: 'fr-FR',
    });

    await preloadLocale('fr');
    const {unmount} = renderWithProviders();

    expect(screen.getByTestId('pref')).toHaveTextContent('auto');
    expect(screen.getByTestId('locale')).toHaveTextContent('fr');
    expect(document.documentElement.lang).toBe('fr');
    expect(getActiveLocale()).toBe('fr');

    fireEvent.click(screen.getByText('pick-vi'));
    await waitFor(() => {
      expect(screen.getByTestId('locale')).toHaveTextContent('vi');
      expect(screen.getByTestId('revoke')).toHaveTextContent('Thu hồi khóa');
      expect(screen.getByTestId('alerts')).toHaveTextContent('Cảnh báo hiện tại');
      expect(screen.getByTestId('circuit')).toHaveTextContent('Đã ngắt mạch');
    });
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('vi');
    expect(loadStoredLocalePreference()).toBe('vi');

    unmount();

    // Simulate reload: persisted preference remains vi.
    renderWithProviders();
    await waitFor(() => {
      expect(screen.getByTestId('pref')).toHaveTextContent('vi');
      expect(screen.getByTestId('locale')).toHaveTextContent('vi');
    });

    // Return to Auto -> resolves back to browser preference (fr).
    fireEvent.click(screen.getByText('pick-auto'));
    await waitFor(() => {
      expect(screen.getByTestId('pref')).toHaveTextContent('auto');
      expect(screen.getByTestId('locale')).toHaveTextContent('fr');
    });
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('auto');
  });

  it('renders Astryx Selector with Auto first followed by all 15 native names in order', async () => {
    Object.defineProperty(navigator, 'languages', {
      configurable: true,
      value: ['en-US'],
    });
    renderWithProviders();

    const trigger = screen.getByRole('combobox', {name: /Language/i});
    fireEvent.click(trigger);

    const options = await screen.findAllByRole('option');
    const texts = options.map(opt => opt.textContent?.trim() ?? '');
    expect(texts[0]).toBe('Auto');
    expect(texts.slice(1)).toEqual(EXPECTED_NATIVE_NAMES);
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
