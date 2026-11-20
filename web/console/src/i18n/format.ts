import type {Locale} from './catalog';

export function activeLocale(): Locale {
  const value = typeof document === 'undefined' ? 'en' : document.documentElement.lang;
  return (value || 'en') as Locale;
}

export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(activeLocale(), options).format(value);
}

export function formatDateTime(value: string | number | Date, options?: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat(activeLocale(), options).format(new Date(value));
}

export function formatDate(value: string | number | Date): string {
  return formatDateTime(value, {dateStyle: 'medium'});
}

export function formatTime(value: string | number | Date): string {
  return formatDateTime(value, {timeStyle: 'medium'});
}

export function formatDateAndTime(value: string | number | Date): string {
  return formatDateTime(value, {dateStyle: 'medium', timeStyle: 'short'});
}

export function formatCurrency(value: number, currency: string): string {
  return new Intl.NumberFormat(activeLocale(), {style: 'currency', currency}).format(value);
}

export function formatMinorCurrency(micros: number, currency: string, fractionDigits: number): string {
  if (!Number.isSafeInteger(micros)) return 'Unavailable';
  const value = BigInt(micros);
  const negative = value < 0n;
  const absolute = negative ? -value : value;
  const scale = 10n ** BigInt(fractionDigits);
  const whole = absolute / scale;
  const fraction = String(absolute % scale).padStart(fractionDigits, '0');
  const locale = activeLocale();
  const groupedWhole = new Intl.NumberFormat(locale, {maximumFractionDigits: 0}).format(Number(whole));
  const digits = new Intl.NumberFormat(locale, {useGrouping: false});
  const localizedFraction = fraction.replace(/\d/g, digit => digits.format(Number(digit)));
  const template = new Intl.NumberFormat(locale, {
    style: 'currency', currency, minimumFractionDigits: fractionDigits, maximumFractionDigits: fractionDigits,
  }).formatToParts(negative ? -0.123456 : 0.123456);
  let integerReplaced = false;
  let fractionReplaced = false;
  return template.map(part => {
    if (part.type === 'integer' && !integerReplaced) { integerReplaced = true; return groupedWhole; }
    if (part.type === 'group') return '';
    if (part.type === 'fraction' && !fractionReplaced) { fractionReplaced = true; return localizedFraction; }
    return part.value;
  }).join('');
}
