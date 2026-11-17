import * as stylex from '@stylexjs/stylex';

export const styles = stylex.create({
  contextControls: {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--spacing-2)',
    flexWrap: 'wrap',
  },
  contextSelect: {
    fontFamily: 'inherit',
    color: 'var(--color-text-primary)',
    backgroundColor: 'var(--color-surface)',
    borderColor: 'var(--color-border)',
    borderStyle: 'solid',
    borderWidth: 1,
    borderRadius: 'var(--radius-element)',
    paddingBlock: 'var(--spacing-2)',
    paddingInline: 'var(--spacing-3)',
  },
});
