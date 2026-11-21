import {useMemo} from 'react';
import {Selector} from '@astryxdesign/core/Selector';
import {SUPPORTED_LOCALES, isSupportedLocale, type LocalePreference} from './locales';
import {useI18n} from './I18nProvider';

export function LanguageSelector({id = 'language-selector'}: {id?: string}) {
  const {preference, setPreference, t} = useI18n();

  const options = useMemo(
    () => [
      {value: 'auto', label: 'Auto'},
      ...SUPPORTED_LOCALES.map(item => ({value: item.code, label: item.nativeName})),
    ],
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
      value={preference}
      onChange={next => {
        if (next === 'auto' || isSupportedLocale(next)) {
          setPreference(next as LocalePreference);
        }
      }}
      containerClassName="tp-lang-selector"
    />
  );
}
