import {StrictMode} from 'react';
import {createRoot} from 'react-dom/client';
import '@astryxdesign/core/reset.css';
import '@astryxdesign/core/astryx.css';
import './astryx-themes';
import './styles/tokens.css';
import './styles/base.css';
import './styles/layout.css';
import './styles/components.css';
import {App} from './App';
import {preloadAstryxLocale} from './i18n/astryxLocales';
import {preloadLocale} from './i18n/catalog';
import {loadStoredLocalePreference, resolveEffectiveLocale} from './i18n/locales';

const root = document.getElementById('root');
if (!root) {
  throw new Error('Tanpopo console mount element #root was not found.');
}

const initialLocale = resolveEffectiveLocale(loadStoredLocalePreference());
document.documentElement.lang = initialLocale;

void Promise.all([preloadLocale(initialLocale), preloadAstryxLocale(initialLocale)])
  .catch(() => undefined)
  .finally(() => {
    createRoot(root).render(
      <StrictMode>
        <App />
      </StrictMode>,
    );
  });
