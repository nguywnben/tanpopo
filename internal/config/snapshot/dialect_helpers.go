// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type flexBool bool

func (b *flexBool) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b = false
		return nil
	case bool:
		*b = flexBool(v)
		return nil
	case int64:
		*b = v != 0
		return nil
	case int32:
		*b = v != 0
		return nil
	case int:
		*b = v != 0
		return nil
	case []byte:
		s := strings.TrimSpace(strings.ToLower(string(v)))
		*b = s == "1" || s == "t" || s == "true"
		return nil
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		*b = s == "1" || s == "t" || s == "true"
		return nil
	default:
		return fmt.Errorf("snapshot: unsupported bool scan type %T", src)
	}
}

type flexNullIntBool struct {
	sql.NullInt64
}

func (n *flexNullIntBool) Scan(src any) error {
	if src == nil {
		n.Valid = false
		n.Int64 = 0
		return nil
	}
	if b, ok := src.(bool); ok {
		n.Valid = true
		if b {
			n.Int64 = 1
		} else {
			n.Int64 = 0
		}
		return nil
	}
	return n.NullInt64.Scan(src)
}

func sqlBoolExpr(d port.Dialect, val bool) string {
	if d == port.DialectPostgres {
		if val {
			return "TRUE"
		}
		return "FALSE"
	}
	if val {
		return "1"
	}
	return "0"
}

func setOperatorRLSTx(ctx context.Context, tx *sql.Tx, d port.Dialect) error {
	if d != port.DialectPostgres {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true), set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return fmt.Errorf("snapshot: set operator RLS context: %w", err)
	}
	return nil
}

func setOrgRLSTx(ctx context.Context, tx *sql.Tx, d port.Dialect, orgID string) error {
	if d != port.DialectPostgres {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("snapshot: set org %q RLS context: %w", orgID, err)
	}
	return nil
}

func listOrgIDsTx(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("snapshot: list organizations: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("snapshot: scan organization id: %w", serr)
		}
		ids = append(ids, id)
	}
	if rerr := rows.Err(); rerr != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("snapshot: iterate organizations: %w", rerr)
	}
	_ = rows.Close()
	return ids, nil
}
