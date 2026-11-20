import type {ReactNode} from 'react';
import {EmptyState} from './primitives';

export type DataColumn<T> = {
  id: string;
  header: string;
  width?: string;
  primary?: boolean;
  hideOnMobile?: boolean;
  render: (row: T) => ReactNode;
};

export function DataTable<T>({
  ariaLabel,
  rows,
  rowKey,
  columns,
  loading = false,
  emptyTitle = 'No records',
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
  if (loading && rows.length === 0) {
    return (
      <div className="tp-surface tp-surface-pad tp-stack" role="status" aria-busy="true" aria-label={`Loading ${ariaLabel}`}>
        <div className="tp-skeleton" style={{width: '45%'}} />
        <div className="tp-skeleton" style={{width: '100%'}} />
        <div className="tp-skeleton" style={{width: '92%'}} />
        <div className="tp-skeleton" style={{width: '78%'}} />
      </div>
    );
  }

  if (rows.length === 0) {
    return <EmptyState title={emptyTitle} description={emptyDescription} action={emptyAction} />;
  }

  const primaryCol = columns.find(col => col.primary) ?? columns[0];
  const secondaryCols = columns.filter(col => col.id !== primaryCol.id && !col.hideOnMobile);

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
          <div key={rowKey(row)} className="tp-mobile-row">
            <div className="tp-mobile-row-top">
              <div style={{minWidth: 0, flex: '1 1 auto'}}>{primaryCol.render(row)}</div>
            </div>
            <div className="tp-mobile-kv-grid">
              {secondaryCols.map(col => (
                <div key={col.id} className="tp-mobile-kv">
                  <span className="tp-mobile-kv-label">{col.header}</span>
                  <div className="tp-mobile-kv-val">{col.render(row)}</div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
