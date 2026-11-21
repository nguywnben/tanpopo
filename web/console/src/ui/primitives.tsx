import {
  Children,
  isValidElement,
  useState,
  type ButtonHTMLAttributes,
  type ChangeEvent,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import {Eye, EyeOff} from 'lucide-react';
import {Badge as AstryxBadge} from '@astryxdesign/core/Badge';
import {Banner as AstryxBanner} from '@astryxdesign/core/Banner';
import {Button as AstryxButton} from '@astryxdesign/core/Button';
import {CheckboxInput as AstryxCheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {CodeBlock as AstryxCodeBlock} from '@astryxdesign/core/CodeBlock';
import type {DropdownMenuOption} from '@astryxdesign/core/DropdownMenu';
import {EmptyState as AstryxEmptyState} from '@astryxdesign/core/EmptyState';
import {IconButton as AstryxIconButton} from '@astryxdesign/core/IconButton';
import {InputGroup as AstryxInputGroup} from '@astryxdesign/core/InputGroup';
import {MetadataList, MetadataListItem} from '@astryxdesign/core/MetadataList';
import {MoreMenu as AstryxMoreMenu} from '@astryxdesign/core/MoreMenu';
import {Tab, TabList} from '@astryxdesign/core/TabList';
import {TextArea as AstryxTextArea} from '@astryxdesign/core/TextArea';
import {TextInput as AstryxTextInput, type TextInputType} from '@astryxdesign/core/TextInput';
import {formatLocalizedMicroUSD} from '../i18n/catalog';
import {getActiveLocale, translateRuntime, useI18n} from '../i18n/I18nProvider';
export {SelectField, type SelectFieldOption, type SelectOption} from './SelectField';

export type StatusTone = 'ok' | 'warn' | 'danger' | 'info' | 'neutral';

export function TanpopoMark({size = 22}: {size?: number}) {
  return (
    <svg width={size} height={size} viewBox="0 0 512 512" fill="none" aria-hidden="true">
      <rect x="12" y="12" width="488" height="488" rx="108" fill="#141417" stroke="#2c2c32" strokeWidth="24" />
      <circle cx="242" cy="268" r="44" fill="#ffffff" />
      <g stroke="#ffffff" strokeWidth="26" strokeLinecap="round">
        <line x1="242" y1="196" x2="242" y2="136" />
        <line x1="179.6" y1="232" x2="127.7" y2="202" />
        <line x1="179.6" y1="304" x2="127.7" y2="334" />
        <line x1="242" y1="340" x2="242" y2="400" />
        <line x1="304.4" y1="304" x2="356.3" y2="334" />
      </g>
      <g fill="#a1a1aa">
        <circle cx="242" cy="96" r="18" />
        <circle cx="93" cy="182" r="18" />
        <circle cx="93" cy="354" r="18" />
        <circle cx="242" cy="440" r="18" />
        <circle cx="391" cy="354" r="18" />
      </g>
      <line x1="334" y1="215" x2="376" y2="190.8" stroke="#a1a1aa" strokeWidth="22" strokeLinecap="round" />
      <circle cx="412" cy="170" r="24" fill="#ffffff" />
    </svg>
  );
}

function extractText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(extractText).filter(Boolean).join(' ').trim();
  if (isValidElement<{children?: ReactNode}>(node)) return extractText(node.props.children);
  return '';
}

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-outline';

export function Button({
  variant = 'secondary',
  size = 'md',
  loading = false,
  className = '',
  children,
  disabled,
  type = 'button',
  onClick,
  'aria-label': ariaLabel,
  id,
  name,
  value,
  form,
  title,
  style,
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: 'sm' | 'md';
  loading?: boolean;
}) {
  const astryxVariant =
    variant === 'danger' ? 'destructive' :
    variant === 'danger-outline' ? 'secondary' :
    variant;
  const computedClass =
    variant === 'danger-outline'
      ? `tp-btn-danger-outline ${className}`.trim()
      : className || undefined;

  const isPlainText =
    typeof children === 'string' ||
    typeof children === 'number' ||
    (Array.isArray(children) && Children.toArray(children).every(child => typeof child === 'string' || typeof child === 'number'));

  const textLabel = ariaLabel ?? (extractText(children) || translateRuntime('common.action'));

  return (
    <AstryxButton
      type={type}
      variant={astryxVariant}
      size={size}
      label={textLabel}
      isDisabled={Boolean(disabled)}
      isLoading={loading}
      onClick={onClick}
      className={computedClass}
      id={id}
      name={name}
      value={value}
      form={form}
      tooltip={title}
      style={style}
    >
      {isPlainText ? undefined : <span className="tp-btn-content">{children}</span>}
    </AstryxButton>
  );
}

export function IconButton({
  label,
  variant = 'ghost',
  className = '',
  children,
  type = 'button',
  disabled,
  onClick,
  style,
  'aria-expanded': ariaExpanded,
}: ButtonHTMLAttributes<HTMLButtonElement> & {label: string; variant?: 'ghost' | 'secondary'}) {
  return (
    <AstryxIconButton
      type={type}
      label={label}
      tooltip={label}
      icon={children}
      variant={variant}
      size="sm"
      isDisabled={Boolean(disabled)}
      onClick={onClick}
      className={className || undefined}
      style={style}
      aria-expanded={ariaExpanded}
    />
  );
}

export function RowActionsMenu({
  label,
  items,
  disabled = false,
}: {
  label: string;
  items: DropdownMenuOption[];
  disabled?: boolean;
}) {
  if (items.length === 0) return null;
  return (
    <AstryxMoreMenu
      label={label}
      items={items}
      variant="ghost"
      size="sm"
      alignment="end"
      isDisabled={disabled}
    />
  );
}

const allowedTextTypes = new Set<TextInputType>(['text', 'password', 'email', 'search', 'tel', 'url']);

export function TextField({
  label,
  hint,
  error,
  id,
  className = '',
  type = 'text',
  value,
  onChange,
  placeholder,
  required,
  disabled,
  readOnly,
  autoFocus,
  autoComplete,
  name,
  min,
  max,
  step,
  inputMode,
  fieldSize = 'md',
}: InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  hint?: string;
  error?: string;
  fieldSize?: 'sm' | 'md';
}) {
  const {t} = useI18n();
  const [revealed, setRevealed] = useState(false);
  const isPassword = type === 'password';
  const isNumber = type === 'number';
  const mappedType: TextInputType = isPassword
    ? (revealed ? 'text' : 'password')
    : allowedTextTypes.has(type as TextInputType)
      ? (type as TextInputType)
      : 'text';
  const resolvedInputMode = inputMode ?? (isNumber ? 'decimal' : undefined);
  const stringValue = value === undefined || value === null ? '' : String(value);

  const handleInputChange = (_: string, event: ChangeEvent<HTMLInputElement>) => {
    if (!onChange) return;
    if (isNumber && min !== undefined && event.target.value !== '') {
      const parsed = Number(event.target.value);
      if (!Number.isNaN(parsed) && typeof min === 'number' && parsed < min) {
        onChange(event);
        return;
      }
    }
    onChange(event);
  };

  if (isPassword) {
    const toggleLabel = revealed ? t('common.hideSecret') : t('common.showSecret');
    return (
      <AstryxInputGroup
        label={label}
        description={hint}
        status={error ? {type: 'error', message: error} : undefined}
        isRequired={Boolean(required)}
        isDisabled={Boolean(disabled)}
        size={fieldSize}
        className={`tp-secret-group ${className}`.trim()}
      >
        <AstryxTextInput
          id={id}
          label={label}
          isLabelHidden
          type={mappedType}
          inputMode={resolvedInputMode}
          value={stringValue}
          onChange={handleInputChange}
          placeholder={placeholder}
          isRequired={Boolean(required)}
          isDisabled={Boolean(disabled)}
          isReadOnly={Boolean(readOnly)}
          hasAutoFocus={Boolean(autoFocus)}
          autoComplete={autoComplete}
          htmlName={name}
          width="100%"
          size={fieldSize}
          controlClassName="tp-secret-input"
        />
        <button
          type="button"
          className="tp-secret-eye"
          aria-label={toggleLabel}
          aria-pressed={revealed}
          title={toggleLabel}
          disabled={Boolean(disabled)}
          onClick={() => setRevealed(prev => !prev)}
        >
          {revealed ? <EyeOff size={15} strokeWidth={1.75} aria-hidden="true" /> : <Eye size={15} strokeWidth={1.75} aria-hidden="true" />}
        </button>
      </AstryxInputGroup>
    );
  }

  return (
    <AstryxTextInput
      id={id}
      label={label}
      type={mappedType}
      inputMode={resolvedInputMode}
      value={stringValue}
      onChange={handleInputChange}
      placeholder={placeholder}
      description={hint}
      status={error ? {type: 'error', message: error} : undefined}
      isRequired={Boolean(required)}
      isDisabled={Boolean(disabled)}
      isReadOnly={Boolean(readOnly)}
      hasAutoFocus={Boolean(autoFocus)}
      autoComplete={autoComplete}
      htmlName={name}
      width="100%"
      size={fieldSize}
      containerClassName={className || undefined}
      data-min={min}
      data-max={max}
      data-step={step}
    />
  );
}

export function TextAreaField({
  label,
  hint,
  error,
  id,
  className = '',
  value,
  onChange,
  onKeyDown,
  placeholder,
  rows,
  required,
  disabled,
  readOnly,
  autoFocus,
  name,
}: TextareaHTMLAttributes<HTMLTextAreaElement> & {
  label: string;
  hint?: string;
  error?: string;
}) {
  const stringValue = value === undefined || value === null ? '' : String(value);
  return (
    <AstryxTextArea
      id={id}
      label={label}
      value={stringValue}
      onChange={(_, event) => onChange?.(event)}
      onKeyDown={onKeyDown}
      placeholder={placeholder}
      rows={rows}
      description={hint}
      status={error ? {type: 'error', message: error} : undefined}
      isRequired={Boolean(required)}
      isDisabled={Boolean(disabled)}
      isReadOnly={Boolean(readOnly)}
      hasAutoFocus={Boolean(autoFocus)}
      htmlName={name}
      width="100%"
      containerClassName={className || undefined}
    />
  );
}

export function CheckboxField({
  label,
  description,
  checked,
  disabled,
  onChange,
  id,
}: {
  label: string;
  description?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
  id?: string;
}) {
  return (
    <AstryxCheckboxInput
      id={id}
      label={label}
      description={description}
      value={checked}
      isDisabled={Boolean(disabled)}
      onChange={next => onChange(next)}
      size="sm"
    />
  );
}

export function StatusPill({
  tone = 'neutral',
  label,
}: {
  tone?: StatusTone;
  label: string;
}) {
  return (
    <span className="tp-status-text" data-tone={tone}>
      {label}
    </span>
  );
}

export function CountPill({count}: {count: number}) {
  return <AstryxBadge variant="neutral" label={count} />;
}

export function Banner({
  tone = 'info',
  title,
  children,
  action,
}: {
  tone?: 'info' | 'ok' | 'warn' | 'danger';
  title?: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  const status =
    tone === 'ok' ? 'success' :
    tone === 'warn' ? 'warning' :
    tone === 'danger' ? 'error' :
    'info';
  const resolvedTitle = title ?? (children ?? translateRuntime('common.notice'));
  const resolvedDescription = title ? children : undefined;
  const role = tone === 'danger' || tone === 'warn' ? 'alert' : undefined;

  return (
    <AstryxBanner
      status={status}
      title={resolvedTitle}
      description={resolvedDescription}
      endContent={action}
      container="card"
      collapsible={false}
      role={role}
    />
  );
}

export function MetricStrip({items}: {items: Array<{label: string; value: string; detail?: string}>}) {
  return (
    <div className="tp-metric-strip">
      {items.map(item => (
        <div key={item.label} className="tp-metric-cell">
          <div className="tp-metric-label">{item.label}</div>
          <div className="tp-metric-value">{item.value}</div>
          {item.detail ? <div className="tp-metric-detail">{item.detail}</div> : null}
        </div>
      ))}
    </div>
  );
}

export function Tabs({
  label,
  items,
  activeId,
  onSelect,
}: {
  label: string;
  items: Array<{id: string; label: string; count?: number}>;
  activeId: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="tp-tabs-wrap">
      <TabList
        role="tablist"
        aria-label={label}
        value={activeId}
        onChange={onSelect}
        hasDivider
        overflow="auto"
      >
        {items.map(item => (
          <Tab
            key={item.id}
            value={item.id}
            label={item.label}
            endContent={item.count !== undefined && item.count > 0 ? <CountPill count={item.count} /> : undefined}
          />
        ))}
      </TabList>
    </div>
  );
}

export function SectionHeader({
  title,
  count,
  actions,
}: {
  title: string;
  count?: number;
  actions?: ReactNode;
}) {
  return (
    <div className="tp-section-head">
      <div className="tp-section-head-left">
        <h2>{title}</h2>
        {count !== undefined ? <CountPill count={count} /> : null}
      </div>
      {actions ? <div className="tp-row">{actions}</div> : null}
    </div>
  );
}

export function CodeBlock({
  title,
  code,
}: {
  title?: string;
  code: string;
}) {
  return (
    <div className="tp-code-container">
      <AstryxCodeBlock
        code={code}
        title={title}
        width="100%"
        isWrapped
        maxHeight={380}
        hasCopyButton
        size="sm"
      />
    </div>
  );
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="tp-empty-wrap">
      <AstryxEmptyState title={title} description={description} actions={action} isCompact headingLevel={3} />
    </div>
  );
}

export function KeyValueList({items}: {items: Array<{label: string; value: ReactNode; mono?: boolean}>}) {
  return (
    <MetadataList columns="single" label={{position: 'top'}} className="tp-metadata-list">
      {items.map(item => (
        <MetadataListItem key={item.label} label={item.label}>
          <span className={`tp-kv-value ${item.mono ? 'mono' : ''}`.trim()}>{item.value}</span>
        </MetadataListItem>
      ))}
    </MetadataList>
  );
}

export function stateTone(state: string): StatusTone {
  const normalized = state.toUpperCase();
  if (['ACTIVE', 'ACTIVE_HEALTHY', 'ENABLED_ACTIVE', 'CREATED_ACTIVE', 'VERIFIED', 'OFFICIAL', 'EXACT', 'APPROVED', 'OK', 'HEALTHY'].includes(normalized)) {
    return 'ok';
  }
  if (['THROTTLED_COOLDOWN', 'DRAINING_MAINTENANCE', 'STAGED', 'PENDING', 'PENDING_APPROVAL', 'ROTATING_GRACE', 'ESTIMATED', 'TESTED', 'UNVERIFIED_EXTERNAL', 'DRIFT_LOCKED', 'DEGRADED'].includes(normalized)) {
    return 'warn';
  }
  if (['CIRCUIT_OPEN_UNHEALTHY', 'REVOKED', 'QUARANTINED', 'FAILED', 'REJECTED', 'UNMETERED_FLAGGED', 'FAIL_CLOSED'].includes(normalized)) {
    return 'danger';
  }
  if (['DISABLED', 'PROVISIONED', 'DECLARED', 'DRAFT', 'INACTIVE'].includes(normalized)) {
    return 'neutral';
  }
  return 'info';
}

export function formatOperationalState(raw: string): string {
  const key = raw.toUpperCase();
  const stateKey = `state.${key}`;
  const translated = translateRuntime(stateKey);
  if (translated !== stateKey) return translated;
  return formatLabel(raw);
}

export function formatLabel(raw: string): string {
  return raw.replaceAll('_', ' ').toLowerCase().replace(/(^|\s)\S/g, match => match.toUpperCase());
}

export function formatMicroUSD(value: number): string {
  return formatLocalizedMicroUSD(getActiveLocale(), value, translateRuntime('common.unavailable'));
}
