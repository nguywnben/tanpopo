import {sourceCatalog, type MessageCatalog} from './catalog';
import {getActiveLocale, setActiveLocale} from './locale-state';

let activeMessages: MessageCatalog = {};

export function setLocaleMessages(locale: ReturnType<typeof getActiveLocale>, messages: MessageCatalog) {
  setActiveLocale(locale);
  activeMessages = messages;
}

export function translateUi(key: string, values: readonly unknown[] = []): string {
  const source = sourceCatalog[key];
  if (source === undefined) return interpolate(key, values);
  return interpolate(activeMessages[key] ?? source, values);
}

function interpolate(message: string, values: readonly unknown[]): string {
  return message.replace(/\{(\d+)\}/g, (token, rawIndex: string) => {
    const value = values[Number(rawIndex)];
    return value === undefined || value === null ? token : String(value);
  });
}
