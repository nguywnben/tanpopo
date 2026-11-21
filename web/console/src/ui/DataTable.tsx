import type {ReactNode} from 'react';
import {Skeleton} from '@astryxdesign/core/Skeleton';
import {useI18n} from '../i18n/I18nProvider';
import {EmptyState} from './primitives';

export type DataColumn<T> = {
  id: string;
  header: string;
  width?: string;
  primary?: boolean;
  hideOnMobile?: boolean;
  render: (row: T) => ReactNode;
};

const ACTION_COL_IDS = new Set(['actions', 'inspect', 'action']);
const PRIORITY_COL_IDS = new Set(['state', 'status', 'health', 'region', 'strategy', 'assurance', 'trust', 'role', 'roles']);

export function DataTable<T>({
  ariaLabel,
  rows,
  rowKey,
  columns,
  loading = false,
  emptyTitle,
  emptyDescription,
  emptyAction,
  onRowClick,
}: {
  ariaLabel: string;
  rows: T[];
  rowKey: (row: T) => string;
  columns: DataColumn<T>[];
  loading?: boolean;
  emptyTitle?: string;
  emptyDescription?: string;
  emptyAction?: ReactNode;
  onRowClick?: (row: T) => void;
}) {
  const {t} = useI18n();
  const resolvedEmptyTitle = emptyTitle ?? t('common.noRecords');

  if (loading && rows.length === 0) {
    return (
      <div className="tp-surface tp-surface-pad tp-stack" role="status" aria-busy="true" aria-label={t('common.loadingTarget', {target: ariaLabel})}>
        <Skeleton width="42%" height={18} radius={1} index={0} />
        <Skeleton width="100%" height={16} radius={1} index={1} />
        <Skeleton width="92%" height={16} radius={1} index={2} />
        <Skeleton width="78%" height={16} radius={1} index={3} />
      </div>
    );
  }

  if (rows.length === 0) {
    return <EmptyState title={resolvedEmptyTitle} description={emptyDescription} action={emptyAction} />;
  }

  const primaryCol = columns.find(col => col.primary) ?? columns[0];
  const actionsCol = columns.find(col => col.id !== primaryCol.id && ACTION_COL_IDS.has(col.id));
  const detailCandidates = columns.filter(col => col.id !== primaryCol.id && col.id !== actionsCol?.id);

  // On mobile, show up to 2 highest-signal summary fields immediately (prioritizing operational status/state)
  // and place all remaining fields inside an expandable Details section so 100% of data stays accessible.
  const visibleCandidates = detailCandidates.filter(col => !col.hideOnMobile);
  const prioritized = [
    ...visibleCandidates.filter(col => PRIORITY_COL_IDS.has(col.id)),
    ...visibleCandidates.filter(col => !PRIORITY_COL_IDS.has(col.id)),
  ];
  const summaryCols = prioritized.slice(0, 2);
  const summaryIds = new Set(summaryCols.map(col => col.id));
  const extraCols = detailCandidates.filter(col => !summaryIds.has(col.id));

  return (
    <div className="tp-table-wrap">
      <div className="tp-table-desktop">
        <table className="tp-table" aria-label={ariaLabel}>
          <thead>
            <tr>
              {columns.map(col => (
                <th key={col.id} scope="col" style={col.width ? {width: col.width} : undefined}>
                  {col.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map(row => (
              <tr
                key={rowKey(row)}
                data-interactive={onRowClick ? 'true' : undefined}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
              >
                {columns.map(col => (
                  <td key={col.id}>{col.render(row)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="tp-table-mobile" aria-label={ariaLabel}>
        {rows.map(row => (
          <div
            key={rowKey(row)}
            className="tp-mobile-row"
            data-interactive={onRowClick ? 'true' : undefined}
            onClick={
              onRowClick
                ? event => {
                    const target = event.target as HTMLElement | null;
                    if (target?.closest('button, a, summary, details, input, select, [role="button"], [role="combobox"]')) return;
                    onRowClick(row);
                  }
                : undefined
            }
          >
            <div className="tp-mobile-row-top">
              <div className="tp-mobile-row-primary">{primaryCol.render(row)}</div>
              {actionsCol ? <div className="tp-mobile-row-actions">{actionsCol.render(row)}</div> : null}
            </div>

            {summaryCols.length > 0 ? (
              <div className="tp-mobile-kv-grid">
                {summaryCols.map(col => (
                  <div key={col.id} className="tp-mobile-kv">
                    <span className="tp-mobile-kv-label">{col.header}</span>
                    <div className="tp-mobile-kv-val">{col.render(row)}</div>
                  </div>
                ))}
              </div>
            ) : null}

            {extraCols.length > 0 ? (
              <details className="tp-mobile-details">
                <summary className="tp-mobile-details-summary">
                  <span>{t('common.detail')} ({extraCols.length})</span>
                </summary>
                <div className="tp-mobile-kv-grid tp-mobile-details-body">
                  {extraCols.map(col => (
                    <div key={col.id} className="tp-mobile-kv">
                      <span className="tp-mobile-kv-label">{col.header}</span>
                      <div className="tp-mobile-kv-val">{col.render(row)}</div>
                    </div>
                  ))}
                </div>
              </details>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}
