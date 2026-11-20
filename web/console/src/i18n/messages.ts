import type {Locale, MessageCatalog} from './catalog';

const loaders: Record<Locale, () => Promise<MessageCatalog>> = {
  'zh-Hans': () => import('./messages/zh-Hans.json').then(module => module.default),
  'zh-Hant': () => import('./messages/zh-Hant.json').then(module => module.default),
  de: () => import('./messages/de.json').then(module => module.default),
  en: async () => ({}),
  es: () => import('./messages/es.json').then(module => module.default),
  fr: () => import('./messages/fr.json').then(module => module.default),
  id: () => import('./messages/id.json').then(module => module.default),
  it: () => import('./messages/it.json').then(module => module.default),
  ja: () => import('./messages/ja.json').then(module => module.default),
  ko: () => import('./messages/ko.json').then(module => module.default),
  pt: () => import('./messages/pt.json').then(module => module.default),
  ru: () => import('./messages/ru.json').then(module => module.default),
  th: () => import('./messages/th.json').then(module => module.default),
  tr: () => import('./messages/tr.json').then(module => module.default),
  vi: () => import('./messages/vi.json').then(module => module.default),
};

export function loadLocaleMessages(locale: Locale): Promise<MessageCatalog> {
  return loaders[locale]();
}
