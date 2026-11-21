import {useMemo, useRef} from 'react';
import {Typeahead, TypeaheadItem, type SearchableItem, type SearchSource} from '@astryxdesign/core/Typeahead';
import {useI18n} from '../i18n/I18nProvider';

type RouteItem = SearchableItem<{custom?: boolean; description?: string}>;

export function RouteTypeahead({
  label,
  value,
  onChange,
  suggestions,
  placeholder,
  hint,
  error,
  disabled = false,
  required = false,
  suggestionDescription,
  customDescription,
  id,
  className = '',
  size = 'md',
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  suggestions: string[];
  placeholder?: string;
  hint?: string;
  error?: string;
  disabled?: boolean;
  required?: boolean;
  suggestionDescription?: string;
  customDescription?: string;
  id?: string;
  className?: string;
  size?: 'sm' | 'md';
}) {
  const {t} = useI18n();
  const resolvedSuggestionDesc = suggestionDescription ?? t('common.configuredRoute');
  const resolvedCustomDesc = customDescription ?? t('common.useCustomRouteSlug');
  const draftQueryRef = useRef('');
  const escapedRef = useRef(false);

  const uniqueSuggestions = useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const raw of suggestions) {
      const slug = raw.trim();
      if (!slug || seen.has(slug)) continue;
      seen.add(slug);
      out.push(slug);
    }
    return out;
  }, [suggestions]);

  const searchSource = useMemo<SearchSource<RouteItem>>(() => {
    const toItem = (slug: string): RouteItem => ({
      id: slug,
      label: slug,
      auxiliaryData: {description: resolvedSuggestionDesc},
    });
    return {
      bootstrap: () => uniqueSuggestions.map(toItem),
      search: (query: string) => {
        const trimmed = query.trim();
        const lower = trimmed.toLowerCase();
        const matches = uniqueSuggestions
          .filter(slug => !lower || slug.toLowerCase().includes(lower))
          .map(toItem);
        if (trimmed && !uniqueSuggestions.some(slug => slug.toLowerCase() === lower)) {
          matches.push({
            id: trimmed,
            label: trimmed,
            auxiliaryData: {custom: true, description: resolvedCustomDesc},
          });
        }
        return matches;
      },
    };
  }, [resolvedCustomDesc, resolvedSuggestionDesc, uniqueSuggestions]);

  const selectedItem = useMemo<RouteItem | null>(() => {
    const trimmed = value.trim();
    return trimmed ? {id: trimmed, label: trimmed} : null;
  }, [value]);

  return (
    <Typeahead
      id={id}
      label={label}
      searchSource={searchSource}
      value={selectedItem}
      renderItem={item => <TypeaheadItem item={item} description={item.auxiliaryData?.description} />}
      onChange={item => {
        draftQueryRef.current = '';
        escapedRef.current = false;
        onChange(item?.id ?? '');
      }}
      onChangeQuery={query => {
        draftQueryRef.current = query;
        escapedRef.current = false;
      }}
      onKeyDown={event => {
        if (event.key === 'Escape') {
          escapedRef.current = true;
          draftQueryRef.current = '';
        }
      }}
      onBlur={event => {
        if (escapedRef.current) {
          escapedRef.current = false;
          return;
        }
        const related = event.relatedTarget as HTMLElement | null;
        if (related && (related.closest('.astryx-typeahead') || related.closest('[role="listbox"]'))) {
          return;
        }
        const candidate = draftQueryRef.current.trim();
        draftQueryRef.current = '';
        if (candidate && candidate !== value.trim()) {
          onChange(candidate);
        }
      }}
      placeholder={placeholder}
      description={hint}
      status={error ? {type: 'error', message: error} : undefined}
      isDisabled={disabled}
      isRequired={required}
      hasEntriesOnFocus
      hasClear
      minQueryLength={0}
      debounceMs={0}
      emptySearchText={t('common.typeAnyRouteSlug')}
      width="100%"
      size={size}
      containerClassName={className || undefined}
    />
  );
}
