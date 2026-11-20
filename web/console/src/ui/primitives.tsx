import {
  Children,
  isValidElement,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import {Badge as AstryxBadge} from '@astryxdesign/core/Badge';
import {Banner as AstryxBanner} from '@astryxdesign/core/Banner';
import {Button as AstryxButton} from '@astryxdesign/core/Button';
import {CheckboxInput as AstryxCheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {CodeBlock as AstryxCodeBlock} from '@astryxdesign/core/CodeBlock';
import type {DropdownMenuOption} from '@astryxdesign/core/DropdownMenu';
import {EmptyState as AstryxEmptyState} from '@astryxdesign/core/EmptyState';
import {IconButton as AstryxIconButton} from '@astryxdesign/core/IconButton';
import {MetadataList, MetadataListItem} from '@astryxdesign/core/MetadataList';
import {MoreMenu as AstryxMoreMenu} from '@astryxdesign/core/MoreMenu';
import {StatusDot, type StatusDotVariant} from '@astryxdesign/core/StatusDot';
import {Tab, TabList} from '@astryxdesign/core/TabList';
import {TextArea as AstryxTextArea} from '@astryxdesign/core/TextArea';
import {TextInput as AstryxTextInput, type TextInputType} from '@astryxdesign/core/TextInput';
export {SelectField, type SelectFieldOption, type SelectOption} from './SelectField';

export type StatusTone = 'ok' | 'warn' | 'danger' | 'info' | 'neutral';

export function TanpopoMark({size = 22}: {size?: number}) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.75" fill="var(--tp-ink-primary)" />
      <path
        d="M12 3v4.2M12 16.8V21M3 12h4.2M16.8 12H21M5.64 5.64l2.97 2.97M15.39 15.39l2.97 2.97M18.36 5.64l-2.97 2.97M8.61 15.39l-2.97 2.97"
        stroke="var(--tp-ink-primary)"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
      <circle cx="12" cy="3" r="1.15" fill="var(--tp-ink-secondary)" />
      <circle cx="21" cy="12" r="1.15" fill="var(--tp-ink-secondary)" />
      <circle cx="12" cy="21" r="1.15" fill="var(--tp-ink-secondary)" />
      <circle cx="3" cy="12" r="1.15" fill="var(--tp-ink-secondary)" />
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

  const textLabel = ariaLabel ?? (extractText(children) || 'Action');

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
  className = '',
  children,
  type = 'button',
  disabled,
  onClick,
  style,
  'aria-expanded': ariaExpanded,
}: ButtonHTMLAttributes<HTMLButtonElement> & {label: string}) {
  return (
    <AstryxIconButton
      type={type}
      label={label}
      tooltip={label}
      icon={children}
      variant="ghost"
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
}: InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  hint?: string;
  error?: string;
}) {
  const isNumber = type === 'number';
  const mappedType: TextInputType = allowedTextTypes.has(type as TextInputType) ? (type as TextInputType) : 'text';
  const resolvedInputMode = inputMode ?? (isNumber ? 'decimal' : undefined);
  const stringValue = value === undefined || value === null ? '' : String(value);

  return (
    <AstryxTextInput
      id={id}
      label={label}
      type={mappedType}
      inputMode={resolvedInputMode}
      value={stringValue}
      onChange={(_, event) => {
        if (!onChange) return;
        if (isNumber && min !== undefined && event.target.value !== '') {
          const parsed = Number(event.target.value);
          if (!Number.isNaN(parsed) && typeof min === 'number' && parsed < min) {
            onChange(event);
            return;
          }
        }
        onChange(event);
      }}
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
      size="md"
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

const dotVariantMap: Record<StatusTone, StatusDotVariant> = {
  ok: 'success',
  warn: 'warning',
  danger: 'error',
  info: 'accent',
  neutral: 'neutral',
};

export function StatusPill({
  tone = 'neutral',
  label,
  icon,
}: {
  tone?: StatusTone;
  label: string;
  icon?: ReactNode;
}) {
  return (
    <span className="tp-status-inline" data-tone={tone}>
      {icon ?? <StatusDot variant={dotVariantMap[tone]} label={label} aria-hidden="true" />}
      <span className="tp-status-label">{label}</span>
    </span>
  );
}

export function CountPill({count, tone = 'neutral'}: {count: number; tone?: 'neutral' | 'warn' | 'danger'}) {
  const variant = tone === 'danger' ? 'error' : tone === 'warn' ? 'warning' : 'neutral';
  return <AstryxBadge variant={variant} label={count} />;
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
  const resolvedTitle = title ?? (children ?? 'Notice');
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
  items: Array<{id: string; label: string; count?: number; countTone?: 'neutral' | 'warn' | 'danger'}>;
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
            endContent={item.count !== undefined && item.count > 0 ? <CountPill count={item.count} tone={item.countTone} /> : undefined}
          />
        ))}
      </TabList>
    </div>
  );
}

export function SectionHeader({
  title,
  count,
  countTone,
  actions,
}: {
  title: string;
  count?: number;
  countTone?: 'neutral' | 'warn' | 'danger';
  actions?: ReactNode;
}) {
  return (
    <div className="tp-section-head">
      <div className="tp-section-head-left">
        <h2>{title}</h2>
        {count !== undefined ? <CountPill count={count} tone={countTone} /> : null}
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

const operationalStateLabels: Record<string, string> = {
  ACTIVE_HEALTHY: 'Healthy',
  CIRCUIT_OPEN_UNHEALTHY: 'Circuit open',
  THROTTLED_COOLDOWN: 'Cooldown',
  DRAINING_MAINTENANCE: 'Draining',
  HALF_OPEN_PROBING: 'Probing',
  DISABLED: 'Disabled',
  PROVISIONED: 'Provisioned',
  CREATED_ACTIVE: 'Active',
  ENABLED_ACTIVE: 'Active',
  ROTATING_GRACE: 'Rotating',
  REVOKED: 'Revoked',
  ACTIVE: 'Active',
  DEGRADED: 'Degraded',
  EXACT: 'Exact',
  ESTIMATED: 'Estimated',
  UNMETERED_FLAGGED: 'Unmetered',
  VERIFIED: 'Verified',
  TESTED: 'Tested',
  DECLARED: 'Declared',
  OFFICIAL: 'Official',
  UNVERIFIED_EXTERNAL: 'Unverified',
  PENDING_APPROVAL: 'Pending approval',
  DRIFT_LOCKED: 'Drift locked',
  QUARANTINED: 'Quarantined',
  FAIL_CLOSED: 'Fail closed',
  OK: 'OK',
  FAILED: 'Failed',
};

export function formatOperationalState(raw: string): string {
  const key = raw.toUpperCase();
  if (key in operationalStateLabels) return operationalStateLabels[key];
  return formatLabel(raw);
}

export function formatLabel(raw: string): string {
  return raw.replaceAll('_', ' ').toLowerCase().replace(/(^|\s)\S/g, match => match.toUpperCase());
}

export function formatMicroUSD(value: number): string {
  if (!Number.isSafeInteger(value)) return 'Unavailable';
  const amount = BigInt(value);
  const sign = amount < 0n ? '-' : '';
  const abs = amount < 0n ? -amount : amount;
  const whole = abs / 1_000_000n;
  const fraction = String(abs % 1_000_000n).padStart(6, '0');
  return `${sign}$${whole.toLocaleString('en-US')}.${fraction}`;
}
