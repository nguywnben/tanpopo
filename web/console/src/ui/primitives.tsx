import {useId, useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes} from 'react';
import {AlertCircle, AlertTriangle, Check, CheckCircle2, Copy, Info, ShieldAlert} from 'lucide-react';

export type StatusTone = 'ok' | 'warn' | 'danger' | 'info' | 'neutral';

export function TanpopoMark({size = 22}: {size?: number}) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="2.75" fill="var(--brand-accent-bright)" />
      <path
        d="M12 3v4.2M12 16.8V21M3 12h4.2M16.8 12H21M5.64 5.64l2.97 2.97M15.39 15.39l2.97 2.97M18.36 5.64l-2.97 2.97M8.61 15.39l-2.97 2.97"
        stroke="var(--ink-primary)"
        strokeWidth="1.75"
        strokeLinecap="round"
      />
      <circle cx="12" cy="3" r="1.15" fill="var(--brand-accent)" />
      <circle cx="21" cy="12" r="1.15" fill="var(--brand-accent)" />
      <circle cx="12" cy="21" r="1.15" fill="var(--brand-accent)" />
      <circle cx="3" cy="12" r="1.15" fill="var(--brand-accent)" />
    </svg>
  );
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
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: 'sm' | 'md';
  loading?: boolean;
}) {
  const variantClass = `tp-btn-${variant}`;
  const sizeClass = size === 'sm' ? 'tp-btn-sm' : '';
  return (
    <button
      type={type}
      className={`tp-btn ${variantClass} ${sizeClass} ${className}`.trim()}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {children}
    </button>
  );
}

export function IconButton({
  label,
  className = '',
  children,
  type = 'button',
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {label: string}) {
  return (
    <button type={type} aria-label={label} title={label} className={`tp-icon-btn ${className}`.trim()} {...rest}>
      {children}
    </button>
  );
}

export function TextField({
  label,
  hint,
  error,
  id,
  className = '',
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  hint?: string;
  error?: string;
}) {
  const autoId = useId();
  const fieldId = id ?? autoId;
  const descIds = [hint ? `${fieldId}-hint` : '', error ? `${fieldId}-err` : ''].filter(Boolean).join(' ') || undefined;
  return (
    <div className={`tp-field ${className}`.trim()}>
      <label className="tp-label" htmlFor={fieldId}>{label}</label>
      <input id={fieldId} className="tp-input" aria-describedby={descIds} aria-invalid={error ? true : undefined} {...rest} />
      {hint ? <p id={`${fieldId}-hint`} className="tp-hint">{hint}</p> : null}
      {error ? <p id={`${fieldId}-err`} className="tp-field-error" role="alert">{error}</p> : null}
    </div>
  );
}

export function SelectField({
  label,
  hint,
  error,
  id,
  className = '',
  children,
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement> & {
  label: string;
  hint?: string;
  error?: string;
}) {
  const autoId = useId();
  const fieldId = id ?? autoId;
  const descIds = [hint ? `${fieldId}-hint` : '', error ? `${fieldId}-err` : ''].filter(Boolean).join(' ') || undefined;
  return (
    <div className={`tp-field ${className}`.trim()}>
      <label className="tp-label" htmlFor={fieldId}>{label}</label>
      <select id={fieldId} className="tp-select" aria-describedby={descIds} aria-invalid={error ? true : undefined} {...rest}>
        {children}
      </select>
      {hint ? <p id={`${fieldId}-hint`} className="tp-hint">{hint}</p> : null}
      {error ? <p id={`${fieldId}-err`} className="tp-field-error" role="alert">{error}</p> : null}
    </div>
  );
}

export function TextAreaField({
  label,
  hint,
  error,
  id,
  className = '',
  ...rest
}: TextareaHTMLAttributes<HTMLTextAreaElement> & {
  label: string;
  hint?: string;
  error?: string;
}) {
  const autoId = useId();
  const fieldId = id ?? autoId;
  const descIds = [hint ? `${fieldId}-hint` : '', error ? `${fieldId}-err` : ''].filter(Boolean).join(' ') || undefined;
  return (
    <div className={`tp-field ${className}`.trim()}>
      <label className="tp-label" htmlFor={fieldId}>{label}</label>
      <textarea id={fieldId} className="tp-textarea" aria-describedby={descIds} aria-invalid={error ? true : undefined} {...rest} />
      {hint ? <p id={`${fieldId}-hint`} className="tp-hint">{hint}</p> : null}
      {error ? <p id={`${fieldId}-err`} className="tp-field-error" role="alert">{error}</p> : null}
    </div>
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
  const autoId = useId();
  const fieldId = id ?? autoId;
  return (
    <label className="tp-checkbox-row" htmlFor={fieldId}>
      <input
        id={fieldId}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={event => onChange(event.target.checked)}
      />
      <span className="tp-checkbox-text">
        <span>{label}</span>
        {description ? <span className="tp-hint">{description}</span> : null}
      </span>
    </label>
  );
}

export function StatusPill({tone = 'neutral', label}: {tone?: StatusTone; label: string}) {
  const Icon =
    tone === 'ok' ? CheckCircle2 :
    tone === 'warn' ? AlertTriangle :
    tone === 'danger' ? ShieldAlert :
    Info;
  return (
    <span className="tp-status" data-tone={tone}>
      <Icon size={13} strokeWidth={2} aria-hidden="true" />
      <span>{label}</span>
    </span>
  );
}

export function CountPill({count, tone = 'neutral'}: {count: number; tone?: 'neutral' | 'warn' | 'danger'}) {
  return <span className="tp-count" data-tone={tone}>{count}</span>;
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
  const Icon =
    tone === 'ok' ? CheckCircle2 :
    tone === 'warn' ? AlertTriangle :
    tone === 'danger' ? AlertCircle :
    Info;
  const role = tone === 'danger' || tone === 'warn' ? 'alert' : 'status';
  return (
    <div className="tp-banner" data-tone={tone} role={role}>
      <Icon size={16} strokeWidth={2} style={{flexShrink: 0, marginTop: 2}} aria-hidden="true" />
      <div className="tp-banner-body">
        {title ? <div className="tp-banner-title">{title}</div> : null}
        {children ? <div>{children}</div> : null}
      </div>
      {action ? <div>{action}</div> : null}
    </div>
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
    <div className="tp-tabs" role="tablist" aria-label={label}>
      {items.map(item => (
        <button
          key={item.id}
          type="button"
          role="tab"
          aria-selected={activeId === item.id}
          className="tp-tab"
          onClick={() => onSelect(item.id)}
        >
          <span>{item.label}</span>
          {item.count !== undefined && item.count > 0 ? <CountPill count={item.count} tone={item.countTone} /> : null}
        </button>
      ))}
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
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1800);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="tp-code-wrap">
      <div className="tp-code-head">
        <span>{title ?? 'Output'}</span>
        <Button variant="ghost" size="sm" onClick={() => void handleCopy()}>
          {copied ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
          <span>{copied ? 'Copied' : 'Copy'}</span>
        </Button>
      </div>
      <pre className="tp-code-pre">{code}</pre>
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
    <div className="tp-empty" role="status">
      <h3>{title}</h3>
      {description ? <p className="tp-muted">{description}</p> : null}
      {action ? <div>{action}</div> : null}
    </div>
  );
}

export function KeyValueList({items}: {items: Array<{label: string; value: ReactNode; mono?: boolean}>}) {
  return (
    <dl className="tp-kv-list">
      {items.map(item => (
        <div key={item.label} className="tp-kv-item">
          <dt className="tp-kv-term">{item.label}</dt>
          <dd className={`tp-kv-desc ${item.mono ? 'mono' : ''}`.trim()}>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function stateTone(state: string): StatusTone {
  const normalized = state.toUpperCase();
  if (['ACTIVE', 'ACTIVE_HEALTHY', 'ENABLED_ACTIVE', 'CREATED_ACTIVE', 'VERIFIED', 'OFFICIAL', 'EXACT', 'APPROVED', 'OK', 'HEALTHY'].includes(normalized)) {
    return 'ok';
  }
  if (['THROTTLED_COOLDOWN', 'DRAINING_MAINTENANCE', 'STAGED', 'PENDING', 'PENDING_APPROVAL', 'ROTATING_GRACE', 'ESTIMATED', 'TESTED', 'UNVERIFIED_EXTERNAL', 'DRIFT_LOCKED'].includes(normalized)) {
    return 'warn';
  }
  if (['CIRCUIT_OPEN_UNHEALTHY', 'DISABLED', 'REVOKED', 'QUARANTINED', 'FAILED', 'REJECTED', 'UNMETERED_FLAGGED', 'FAIL_CLOSED'].includes(normalized)) {
    return 'danger';
  }
  return 'info';
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
