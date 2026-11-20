import {useMemo} from 'react';
import {
  MultiSelector,
  type MultiSelectorOptionData,
  type MultiSelectorOptionType,
  type MultiSelectorPresentation,
  type MultiSelectorSize,
} from '@astryxdesign/core/MultiSelector';

export type MultiSelectOption = MultiSelectorOptionData;
export type MultiSelectFieldOption = MultiSelectorOptionType;

function countOptions(options: MultiSelectFieldOption[]): number {
  let count = 0;
  for (const option of options) {
    if (typeof option === 'string') {
      count += 1;
    } else if ('type' in option) {
      if (option.type === 'section') {
        count += option.options.length;
      }
    } else {
      count += 1;
    }
  }
  return count;
}

export function MultiSelectField({
  label,
  value,
  onChange,
  options,
  placeholder,
  hint,
  error,
  id,
  name,
  className = '',
  disabled = false,
  disabledMessage,
  required = false,
  hasSelectAll = false,
  hasClear = false,
  hasSearch,
  triggerDisplay = 'labels',
  size = 'md',
  width = '100%',
  presentation = 'popover',
}: {
  label: string;
  value: string[];
  onChange: (values: string[]) => void;
  options: MultiSelectFieldOption[];
  placeholder?: string;
  hint?: string;
  error?: string;
  id?: string;
  name?: string;
  className?: string;
  disabled?: boolean;
  disabledMessage?: string;
  required?: boolean;
  hasSelectAll?: boolean;
  hasClear?: boolean;
  hasSearch?: boolean;
  triggerDisplay?: 'count' | 'labels' | 'badges';
  size?: MultiSelectorSize;
  width?: number | string;
  presentation?: MultiSelectorPresentation;
}) {
  const optionCount = useMemo(() => countOptions(options), [options]);
  const enableSearch = hasSearch ?? optionCount > 8;

  return (
    <MultiSelector
      id={id}
      label={label}
      options={options}
      value={value}
      onChange={next => onChange(next)}
      placeholder={placeholder}
      description={hint}
      status={error ? {type: 'error', message: error} : undefined}
      isDisabled={disabled}
      disabledMessage={disabledMessage}
      isRequired={required}
      hasSelectAll={hasSelectAll}
      hasClear={hasClear}
      hasSearch={enableSearch}
      triggerDisplay={triggerDisplay}
      size={size}
      width={width}
      presentation={presentation}
      htmlName={name}
      containerClassName={className || undefined}
    />
  );
}
