import {useEffect, useId, useRef, useState, type ReactNode} from 'react';
import {X} from 'lucide-react';
import {Button, IconButton, TextField} from './primitives';

export function Modal({
  open,
  onClose,
  title,
  subtitle,
  size = 'md',
  dismissible = true,
  footer,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  size?: 'md' | 'lg';
  dismissible?: boolean;
  footer?: ReactNode;
  children: ReactNode;
}) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dismissible) onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      previous?.focus();
    };
  }, [open, dismissible, onClose]);

  if (!open) return null;

  return (
    <div
      className="tp-overlay"
      onMouseDown={event => {
        if (dismissible && event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="tp-modal"
        data-size={size}
      >
        <div className="tp-dialog-head">
          <div className="tp-dialog-title-group">
            <h2 id={titleId}>{title}</h2>
            {subtitle ? <p className="tp-muted">{subtitle}</p> : null}
          </div>
          {dismissible ? (
            <IconButton label="Close dialog" onClick={onClose}>
              <X size={16} aria-hidden="true" />
            </IconButton>
          ) : null}
        </div>
        <div className="tp-dialog-body">{children}</div>
        {footer ? <div className="tp-dialog-foot">{footer}</div> : null}
      </div>
    </div>
  );
}

export function Drawer({
  open,
  onClose,
  title,
  subtitle,
  size = 'md',
  footer,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  size?: 'md' | 'lg';
  footer?: ReactNode;
  children: ReactNode;
}) {
  const titleId = useId();
  const drawerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    drawerRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      previous?.focus();
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="tp-drawer-overlay"
      onMouseDown={event => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="tp-drawer"
        data-size={size}
      >
        <div className="tp-dialog-head">
          <div className="tp-dialog-title-group">
            <h2 id={titleId}>{title}</h2>
            {subtitle ? <p className="tp-muted">{subtitle}</p> : null}
          </div>
          <IconButton label="Close panel" onClick={onClose}>
            <X size={16} aria-hidden="true" />
          </IconButton>
        </div>
        <div className="tp-dialog-body">{children}</div>
        {footer ? <div className="tp-dialog-foot">{footer}</div> : null}
      </div>
    </div>
  );
}

export function ConfirmDialog({
  open,
  onClose,
  title,
  description,
  confirmLabel,
  confirmVariant = 'danger',
  requireMatch,
  matchLabel,
  busy = false,
  onConfirm,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  confirmLabel: string;
  confirmVariant?: 'danger' | 'primary';
  requireMatch?: string;
  matchLabel?: string;
  busy?: boolean;
  onConfirm: () => void;
  children?: ReactNode;
}) {
  const [typed, setTyped] = useState('');

  useEffect(() => {
    if (open) setTyped('');
  }, [open]);

  const canConfirm = !busy && (!requireMatch || typed === requireMatch);

  return (
    <Modal
      open={open}
      onClose={busy ? () => undefined : onClose}
      dismissible={!busy}
      title={title}
      footer={
        <>
          <Button variant="secondary" disabled={busy} onClick={onClose}>Cancel</Button>
          <Button variant={confirmVariant} loading={busy} disabled={!canConfirm} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <p className="tp-muted">{description}</p>
      {children}
      {requireMatch ? (
        <TextField
          label={matchLabel ?? `Type "${requireMatch}" to confirm`}
          value={typed}
          disabled={busy}
          onChange={event => setTyped(event.target.value)}
        />
      ) : null}
    </Modal>
  );
}
