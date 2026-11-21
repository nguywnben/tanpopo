import type {Overrides, ProviderMessagesByLocale, RuntimeCatalog} from '@astryxdesign/core/i18n';
import type {LocaleCode} from './locales';

const astryxTagMap: Record<LocaleCode, string> = {
  'zh-Hans': 'zh-CN',
  'zh-Hant': 'zh-TW',
  de: 'de-DE',
  en: 'en',
  es: 'es-ES',
  fr: 'fr-FR',
  id: 'id',
  it: 'it-IT',
  ja: 'ja-JP',
  ko: 'ko-KR',
  pt: 'pt-BR',
  ru: 'ru-RU',
  th: 'th',
  tr: 'tr-TR',
  vi: 'vi-VN',
};

const astryxLoaders: Partial<Record<LocaleCode, () => Promise<{default: RuntimeCatalog}>>> = {
  'zh-Hans': () => import('@astryxdesign/core/locales/zh-CN.generated.js'),
  'zh-Hant': () => import('@astryxdesign/core/locales/zh-TW.generated.js'),
  de: () => import('@astryxdesign/core/locales/de-DE.generated.js'),
  es: () => import('@astryxdesign/core/locales/es-ES.generated.js'),
  fr: () => import('@astryxdesign/core/locales/fr-FR.generated.js'),
  it: () => import('@astryxdesign/core/locales/it-IT.generated.js'),
  ja: () => import('@astryxdesign/core/locales/ja-JP.generated.js'),
  ko: () => import('@astryxdesign/core/locales/ko-KR.generated.js'),
  pt: () => import('@astryxdesign/core/locales/pt-BR.generated.js'),
  ru: () => import('@astryxdesign/core/locales/ru-RU.generated.js'),
  tr: () => import('@astryxdesign/core/locales/tr-TR.generated.js'),
  vi: () => import('@astryxdesign/core/locales/vi-VN.generated.js'),
};

const loadedAstryxCatalogs = new Map<string, RuntimeCatalog>();

export const ASTRYX_LOCALE_OVERRIDES: Overrides = {
  id: {
    '@astryx.alertDialog.cancel': 'Batal',
    '@astryx.banner.dismiss': 'Tutup',
    '@astryx.banner.collapse': 'Ciutkan',
    '@astryx.banner.expand': 'Luaskan',
    '@astryx.codeBlock.copied': 'Disalin',
    '@astryx.codeBlock.copyCode': 'Salin kode',
    '@astryx.codeBlock.copyFailed': 'Gagal menyalin ke papan klip.',
    '@astryx.codeBlock.code': 'Kode',
    '@astryx.commandPalette.emptySearch': 'Tidak ada hasil',
    '@astryx.commandPalette.emptyBootstrap': 'Ketik untuk mencari',
    '@astryx.commandPalette.loading': 'Memuat',
    '@astryx.commandPalette.label': 'Palet perintah',
    '@astryx.commandPalette.input.placeholder': 'Cari…',
    '@astryx.commandPalette.input.label': 'Cari perintah',
    '@astryx.commandPalette.list.label': 'Perintah',
    '@astryx.commandPalette.footer.navigate': 'Navigasi',
    '@astryx.commandPalette.footer.select': 'Pilih',
    '@astryx.commandPalette.footer.close': 'Tutup',
    '@astryx.dialog.close': 'Tutup',
    '@astryx.field.optional': 'Opsional',
    '@astryx.field.required': 'Wajib',
    '@astryx.moreMenu.label': 'Opsi lainnya',
    '@astryx.multiSelector.selectAll': 'Pilih semua',
    '@astryx.multiSelector.searchPlaceholder': 'Cari…',
    '@astryx.multiSelector.searchOptions': 'Cari opsi',
    '@astryx.multiSelector.empty': 'Tidak ada opsi',
    '@astryx.multiSelector.selectPlaceholder': 'Pilih…',
    '@astryx.multiSelector.emptySearchResults': 'Tidak ada hasil',
    '@astryx.selector.searchPlaceholder': 'Cari…',
    '@astryx.selector.searchOptions': 'Cari opsi',
    '@astryx.selector.empty': 'Tidak ada opsi',
    '@astryx.selector.placeholder': 'Pilih…',
    '@astryx.selector.emptySearchResults': 'Tidak ada hasil',
    '@astryx.typeahead.emptySearchResults': 'Tidak ada hasil',
    '@astryx.typeahead.loading': 'Memuat',
    '@astryx.typeahead.searchResults': 'Hasil pencarian',
    '@astryx.typeahead.clearSelection': 'Hapus pilihan',
    '@astryx.typeahead.searchPlaceholder': 'Cari…',
  },
  th: {
    '@astryx.alertDialog.cancel': 'ยกเลิก',
    '@astryx.banner.dismiss': 'ปิด',
    '@astryx.banner.collapse': 'ย่อ',
    '@astryx.banner.expand': 'ขยาย',
    '@astryx.codeBlock.copied': 'คัดลอกแล้ว',
    '@astryx.codeBlock.copyCode': 'คัดลอกโค้ด',
    '@astryx.codeBlock.copyFailed': 'คัดลอกไปยังคลิปบอร์ดไม่สำเร็จ',
    '@astryx.codeBlock.code': 'โค้ด',
    '@astryx.commandPalette.emptySearch': 'ไม่พบผลลัพธ์',
    '@astryx.commandPalette.emptyBootstrap': 'พิมพ์เพื่อค้นหา',
    '@astryx.commandPalette.loading': 'กำลังโหลด',
    '@astryx.commandPalette.label': 'แถบคำสั่ง',
    '@astryx.commandPalette.input.placeholder': 'ค้นหา…',
    '@astryx.commandPalette.input.label': 'ค้นหาคำสั่ง',
    '@astryx.commandPalette.list.label': 'คำสั่ง',
    '@astryx.commandPalette.footer.navigate': 'เลื่อนดู',
    '@astryx.commandPalette.footer.select': 'เลือก',
    '@astryx.commandPalette.footer.close': 'ปิด',
    '@astryx.dialog.close': 'ปิด',
    '@astryx.field.optional': 'ไม่บังคับ',
    '@astryx.field.required': 'จำเป็น',
    '@astryx.moreMenu.label': 'ตัวเลือกเพิ่มเติม',
    '@astryx.multiSelector.selectAll': 'เลือกทั้งหมด',
    '@astryx.multiSelector.searchPlaceholder': 'ค้นหา…',
    '@astryx.multiSelector.searchOptions': 'ค้นหาตัวเลือก',
    '@astryx.multiSelector.empty': 'ไม่มีตัวเลือก',
    '@astryx.multiSelector.selectPlaceholder': 'เลือก…',
    '@astryx.multiSelector.emptySearchResults': 'ไม่พบผลลัพธ์',
    '@astryx.selector.searchPlaceholder': 'ค้นหา…',
    '@astryx.selector.searchOptions': 'ค้นหาตัวเลือก',
    '@astryx.selector.empty': 'ไม่มีตัวเลือก',
    '@astryx.selector.placeholder': 'เลือก…',
    '@astryx.selector.emptySearchResults': 'ไม่พบผลลัพธ์',
    '@astryx.typeahead.emptySearchResults': 'ไม่พบผลลัพธ์',
    '@astryx.typeahead.loading': 'กำลังโหลด',
    '@astryx.typeahead.searchResults': 'ผลการค้นหา',
    '@astryx.typeahead.clearSelection': 'ล้างการเลือก',
    '@astryx.typeahead.searchPlaceholder': 'ค้นหา…',
  },
};

export function toAstryxLocaleTag(locale: LocaleCode): string {
  return astryxTagMap[locale];
}

export function getLoadedAstryxMessages(locale: LocaleCode): ProviderMessagesByLocale | undefined {
  const tag = astryxTagMap[locale];
  const cat = loadedAstryxCatalogs.get(tag);
  return cat ? {[tag]: cat} : undefined;
}

export async function preloadAstryxLocale(locale: LocaleCode): Promise<ProviderMessagesByLocale | undefined> {
  const tag = astryxTagMap[locale];
  const cached = loadedAstryxCatalogs.get(tag);
  if (cached) {
    return {[tag]: cached};
  }
  const loader = astryxLoaders[locale];
  if (!loader) {
    return undefined;
  }
  const mod = await loader();
  loadedAstryxCatalogs.set(tag, mod.default);
  return {[tag]: mod.default};
}
