// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type sqlQueryRowExecutor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type dialectExec struct {
	exec    sqlExecutor
	qExec   sqlQueryRowExecutor
	dialect port.Dialect
}

func (s *Store) effectiveDialect(ctx context.Context) port.Dialect {
	if s != nil && s.dialect != "" {
		return s.dialect
	}
	if s != nil && s.db != nil {
		return port.DetectDialect(ctx, s.db)
	}
	return port.DialectSQLite
}

func (s *Store) q(ctx context.Context, query string) string {
	return port.Rebind(s.effectiveDialect(ctx), query)
}

func (s *Store) setOrgRLSTx(ctx context.Context, tx *sql.Tx, orgID string) error {
	if s.effectiveDialect(ctx) != port.DialectPostgres || strings.TrimSpace(orgID) == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("vault: set app.current_org_id: %w", err)
	}
	return nil
}

func (s *Store) setOperatorRLSTx(ctx context.Context, tx *sql.Tx) error {
	if s.effectiveDialect(ctx) != port.DialectPostgres {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return fmt.Errorf("vault: set app.is_instance_operator: %w", err)
	}
	return nil
}

func (s *Store) withOrgReadTx(ctx context.Context, orgID string, fn func(q sqlQueryRowExecutor, d port.Dialect) error) error {
	d := s.effectiveDialect(ctx)
	if d != port.DialectPostgres {
		return fn(s.db, d)
	}
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("vault: begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("vault: set app.current_org_id: %w", err)
	}
	return fn(tx, d)
}

func (s *Store) withOperatorReadTx(ctx context.Context, fn func(q sqlQueryRowExecutor, d port.Dialect) error) error {
	d := s.effectiveDialect(ctx)
	if d != port.DialectPostgres {
		return fn(s.db, d)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("vault: begin operator read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
		return fmt.Errorf("vault: set app.is_instance_operator: %w", err)
	}
	return fn(tx, d)
}
