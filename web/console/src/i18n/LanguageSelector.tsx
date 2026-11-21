import {useMemo} from 'react';
import {Selector} from '@astryxdesign/core/Selector';
import {SUPPORTED_LOCALES, isSupportedLocale} from './locales';
import {useI18n} from './I18nProvider';

export function LanguageSelector({id = 'language-selector'}: {id?: string}) {
  const {locale, setLocale, t} = useI18n();

  const options = useMemo(
    () => SUPPORTED_LOCALES.map(item => ({value: item.code, label: item.nativeName})),
    [],
  );

  return (
    <Selector
      id={id}
      label={t('common.language')}
      isLabelHidden
      size="sm"
      variant="input"
      hasSearch={false}
      options={options}
      value={locale}
      onChange={next => {
        if (isSupportedLocale(next)) {
          setLocale(next);
        }
      }}
      containerClassName="tp-lang-selector"
    />
  );
}
