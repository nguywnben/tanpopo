import {useEffect, useState, type ReactNode} from 'react';
import {AlertDialog} from '@astryxdesign/core/AlertDialog';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Drawer as AstryxDrawer, DrawerHeader} from '@astryxdesign/core/Drawer';
import {Layout, LayoutContent, LayoutFooter} from '@astryxdesign/core/Layout';
import {Button, TextField} from './primitives';

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
  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && dismissible) onClose();
  };

  return (
    <Dialog
      isOpen={open}
      onOpenChange={handleOpenChange}
      width={size === 'lg' ? 720 : 540}
      maxHeight="85dvh"
      purpose={dismissible ? 'info' : 'required'}
    >
      <Layout
        header={
          <DialogHeader
            title={title}
            subtitle={subtitle}
            hasDivider
            onOpenChange={dismissible ? handleOpenChange : undefined}
          />
        }
        content={
          <LayoutContent>
            <div className="tp-overlay-body">{children}</div>
          </LayoutContent>
        }
        footer={
          footer ? (
            <LayoutFooter hasDivider>
              <div className="tp-overlay-footer">{footer}</div>
            </LayoutFooter>
          ) : undefined
        }
      />
    </Dialog>
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
  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) onClose();
  };

  return (
    <AstryxDrawer
      isOpen={open}
      onOpenChange={handleOpenChange}
      side="end"
      width={size === 'lg' ? 640 : 480}
      label={title}
      purpose="info"
    >
      <Layout
        header={
          <DrawerHeader
            title={title}
            subtitle={subtitle}
            hasDivider
            onOpenChange={handleOpenChange}
          />
        }
        content={
          <LayoutContent>
            <div className="tp-overlay-body">{children}</div>
          </LayoutContent>
        }
        footer={
          footer ? (
            <LayoutFooter hasDivider>
              <div className="tp-overlay-footer">{footer}</div>
            </LayoutFooter>
          ) : undefined
        }
      />
    </AstryxDrawer>
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

  if (!requireMatch && !children) {
    return (
      <AlertDialog
        isOpen={open}
        onOpenChange={nextOpen => {
          if (!nextOpen && !busy) onClose();
        }}
        title={title}
        description={description}
        cancelLabel="Cancel"
        actionLabel={confirmLabel}
        actionVariant={confirmVariant === 'danger' ? 'destructive' : 'primary'}
        isActionLoading={busy}
        onAction={onConfirm}
        width={440}
      />
    );
  }

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
