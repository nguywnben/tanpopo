import {useMemo} from 'react';
import {
  Selector,
  type SelectorOptionData,
  type SelectorOptionType,
  type SelectorPresentation,
  type SelectorSize,
} from '@astryxdesign/core/Selector';

export type SelectOption = SelectorOptionData;
export type SelectFieldOption = SelectorOptionType;

function countSelectableOptions(options: SelectFieldOption[]): number {
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

export function SelectField({
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
  isLabelHidden = false,
  hasSearch,
  searchPlaceholder,
  size = 'md',
  variant = 'input',
  width = '100%',
  presentation = 'popover',
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: SelectFieldOption[];
  placeholder?: string;
  hint?: string;
  error?: string;
  id?: string;
  name?: string;
  className?: string;
  disabled?: boolean;
  disabledMessage?: string;
  required?: boolean;
  isLabelHidden?: boolean;
  hasSearch?: boolean;
  searchPlaceholder?: string;
  size?: SelectorSize;
  variant?: 'input' | 'secondary' | 'ghost';
  width?: number | string;
  presentation?: SelectorPresentation;
}) {
  const selectableCount = useMemo(() => countSelectableOptions(options), [options]);
  const enableSearch = hasSearch ?? selectableCount > 8;

  return (
    <Selector
      id={id}
      label={label}
      isLabelHidden={isLabelHidden}
      options={options}
      value={value}
      onChange={onChange}
      placeholder={placeholder}
      description={hint}
      status={error ? {type: 'error', message: error} : undefined}
      isDisabled={disabled}
      disabledMessage={disabledMessage}
      isRequired={required}
      hasSearch={enableSearch}
      searchPlaceholder={searchPlaceholder}
      size={size}
      variant={variant}
      width={width}
      presentation={presentation}
      htmlName={name}
      containerClassName={className || undefined}
    />
  );
}
