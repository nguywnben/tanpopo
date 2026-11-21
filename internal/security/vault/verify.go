// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// VerifyAllDEKEnvelopes authenticates every persisted organization and project
// DEK envelope without creating, rotating, or otherwise modifying key rows.
func (s *Store) VerifyAllDEKEnvelopes(ctx context.Context) error {
	if s == nil || s.db == nil || s.kek == nil {
		return fmt.Errorf("vault: store, database, and KEK provider are required")
	}
	if s.effectiveDialect(ctx) == port.DialectPostgres {
		orgIDs, err := s.listPGOrgIDs(ctx)
		if err != nil {
			return err
		}
		for _, orgID := range orgIDs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.withOrgReadTx(ctx, orgID, func(q sqlQueryRowExecutor, d port.Dialect) error {
				return s.verifyOrgDEKEnvelopesExec(ctx, q, d, orgID)
			}); err != nil {
				return err
			}
		}
		s.EvictAllDEKs()
		return nil
	}

	orgIDs, err := listIDsQuery(ctx, s.db, `SELECT org_id FROM org_encryption_keys ORDER BY org_id`, "organization DEK")
	if err != nil {
		return err
	}
	for _, orgID := range orgIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, dek, err := s.loadOrgDEKReadOnly(ctx, orgID); err != nil {
			return fmt.Errorf("vault: verify organization %q DEK envelope: %w", orgID, err)
		} else {
			clear(dek)
		}
	}
	// Enumerate project envelopes independently so a malformed/orphaned row
	// cannot hide behind an organization-key-driven nested scan.
	projectRows, err := s.db.QueryContext(ctx, `SELECT org_id, project_id FROM project_encryption_keys ORDER BY org_id, project_id`)
	if err != nil {
		return fmt.Errorf("vault: list project DEK envelopes: %w", err)
	}
	type projectKey struct{ orgID, projectID string }
	var projects []projectKey
	for projectRows.Next() {
		var project projectKey
		if err := projectRows.Scan(&project.orgID, &project.projectID); err != nil {
			_ = projectRows.Close()
			return fmt.Errorf("vault: read project DEK envelope key: %w", err)
		}
		projects = append(projects, project)
	}
	if err := projectRows.Err(); err != nil {
		_ = projectRows.Close()
		return fmt.Errorf("vault: iterate project DEK envelopes: %w", err)
	}
	if err := projectRows.Close(); err != nil {
		return fmt.Errorf("vault: close project DEK query: %w", err)
	}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, _, dek, err := s.loadProjectDEKReadOnly(ctx, project.orgID, project.projectID); err != nil {
			return fmt.Errorf("vault: verify project %q DEK envelope for organization %q: %w", project.projectID, project.orgID, err)
		} else {
			clear(dek)
		}
	}
	s.EvictAllDEKs()
	return nil
}

// VerifyAllSecretEnvelopes runs fail-closed AEAD trial decryption across every
// persisted secret_credentials row without ever returning or logging plaintext (CT-32).
func (s *Store) VerifyAllSecretEnvelopes(ctx context.Context) (int, error) {
	if s == nil || s.db == nil || s.kek == nil {
		return 0, fmt.Errorf("vault: store, database, and KEK provider are required")
	}
	var (
		orgIDs []string
		err    error
	)
	if s.effectiveDialect(ctx) == port.DialectPostgres {
		orgIDs, err = s.listPGOrgIDs(ctx)
	} else {
		orgIDs, err = listIDsQuery(ctx, s.db, `SELECT DISTINCT org_id FROM secret_credentials ORDER BY org_id ASC`, "secret org")
	}
	if err != nil {
		return 0, err
	}
	verified := 0
	for _, orgID := range orgIDs {
		if err := ctx.Err(); err != nil {
			return verified, err
		}
		var n int
		if err := s.withOrgReadTx(ctx, orgID, func(q sqlQueryRowExecutor, d port.Dialect) error {
			var verr error
			n, verr = s.verifyOrgSecretEnvelopesExec(ctx, q, d, orgID)
			return verr
		}); err != nil {
			return verified, err
		}
		verified += n
	}
	s.EvictAllDEKs()
	return verified, nil
}

// VerifyAllDEKEnvelopesTx authenticates all persisted org and project DEK envelopes
// within an uncommitted caller transaction (used by CT-32 sqlite-to-postgres migration --verify).
func (s *Store) VerifyAllDEKEnvelopesTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect) error {
	if s == nil || tx == nil || s.kek == nil {
		return fmt.Errorf("vault: store, tx, and KEK provider are required")
	}
	orgIDs, err := listOrgsInTx(ctx, tx, dialect)
	if err != nil {
		return err
	}
	for _, orgID := range orgIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := setOrgRLSTxQuery(ctx, tx, dialect, orgID); err != nil {
			return err
		}
		if err := s.verifyOrgDEKEnvelopesExec(ctx, tx, dialect, orgID); err != nil {
			return err
		}
	}
	s.EvictAllDEKs()
	return nil
}

// VerifyAllSecretEnvelopesTx verifies every persisted secret_credentials AEAD tag
// inside a caller-managed uncommitted transaction without returning plaintext (CT-32).
func (s *Store) VerifyAllSecretEnvelopesTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect) (int, error) {
	if s == nil || tx == nil || s.kek == nil {
		return 0, fmt.Errorf("vault: store, tx, and KEK provider are required")
	}
	orgIDs, err := listOrgsInTx(ctx, tx, dialect)
	if err != nil {
		return 0, err
	}
	verified := 0
	for _, orgID := range orgIDs {
		if err := ctx.Err(); err != nil {
			return verified, err
		}
		if err := setOrgRLSTxQuery(ctx, tx, dialect, orgID); err != nil {
			return verified, err
		}
		n, verr := s.verifyOrgSecretEnvelopesExec(ctx, tx, dialect, orgID)
		if verr != nil {
			return verified, verr
		}
		verified += n
	}
	s.EvictAllDEKs()
	return verified, nil
}

func (s *Store) verifyOrgDEKEnvelopesExec(ctx context.Context, q sqlQueryRowExecutor, d port.Dialect, orgID string) error {
	var present int
	perr := q.QueryRowContext(ctx, port.Rebind(d, `SELECT 1 FROM org_encryption_keys WHERE org_id = ?`), orgID).Scan(&present)
	if perr != nil && !errors.Is(perr, sql.ErrNoRows) {
		return fmt.Errorf("vault: probe org %q DEK row: %w", orgID, perr)
	}
	if perr == nil {
		_, dek, derr := s.loadOrgDEKWithExec(ctx, q, d, orgID)
		if derr != nil {
			return fmt.Errorf("vault: verify organization %q DEK envelope: %w", orgID, derr)
		}
		clear(dek)
	}
	projRows, rerr := q.QueryContext(ctx, port.Rebind(d, `SELECT project_id FROM project_encryption_keys WHERE org_id = ? ORDER BY project_id`), orgID)
	if rerr != nil {
		return fmt.Errorf("vault: list project DEK envelopes for org %q: %w", orgID, rerr)
	}
	var projIDs []string
	for projRows.Next() {
		var pid string
		if serr := projRows.Scan(&pid); serr != nil {
			_ = projRows.Close()
			return serr
		}
		projIDs = append(projIDs, pid)
	}
	ierr := projRows.Err()
	_ = projRows.Close()
	if ierr != nil {
		return ierr
	}
	for _, pid := range projIDs {
		_, _, dek, derr := s.loadProjectDEKWithExec(ctx, q, d, orgID, pid)
		if derr != nil {
			return fmt.Errorf("vault: verify project %q DEK envelope for organization %q: %w", pid, orgID, derr)
		}
		clear(dek)
	}
	return nil
}

func (s *Store) verifyOrgSecretEnvelopesExec(ctx context.Context, q sqlQueryRowExecutor, d port.Dialect, orgID string) (int, error) {
	type secretRow struct {
		id         string
		ownerProj  *string
		kind       string
		aeadAlg    string
		nonce      []byte
		ciphertext []byte
	}
	rows, rerr := q.QueryContext(ctx, port.Rebind(d, `
		SELECT id, owner_project_id, secret_kind, aead_alg, nonce, ciphertext
		FROM secret_credentials
		WHERE org_id = ?
		ORDER BY id ASC`), orgID)
	if rerr != nil {
		return 0, fmt.Errorf("vault: list secret envelopes for org %q: %w", orgID, rerr)
	}
	var items []secretRow
	for rows.Next() {
		var (
			item     secretRow
			projNull sql.NullString
		)
		if serr := rows.Scan(&item.id, &projNull, &item.kind, &item.aeadAlg, &item.nonce, &item.ciphertext); serr != nil {
			_ = rows.Close()
			return 0, serr
		}
		if projNull.Valid {
			v := projNull.String
			item.ownerProj = &v
		}
		items = append(items, item)
	}
	ierr := rows.Err()
	_ = rows.Close()
	if ierr != nil {
		return 0, ierr
	}
	verified := 0
	for _, item := range items {
		if item.aeadAlg != "AES-256-GCM" {
			return verified, fmt.Errorf("%w: unsupported algorithm %q for secret %q", ErrDecryptionFailed, item.aeadAlg, item.id)
		}
		var (
			aad []byte
			dek []byte
			err error
		)
		if item.ownerProj == nil {
			aad, err = OrgSecretAAD(orgID, item.id, item.kind)
			if err == nil {
				_, dek, err = s.loadOrgDEKWithExec(ctx, q, d, orgID)
			}
		} else {
			aad, err = ProjectRecordAAD(orgID, *item.ownerProj, RecordKindCred, item.id)
			if err == nil {
				_, _, dek, err = s.loadProjectDEKWithExec(ctx, q, d, orgID, *item.ownerProj)
			}
		}
		if err != nil {
			return verified, fmt.Errorf("vault: load DEK for secret %q (%q): %w", item.id, orgID, err)
		}
		pt, oerr := openAESGCM(dek, aad, item.nonce, item.ciphertext)
		clear(dek)
		if oerr != nil {
			clear(pt)
			return verified, fmt.Errorf("vault: verify secret %q (%q) AEAD: %w", item.id, orgID, oerr)
		}
		clear(pt)
		verified++
	}
	return verified, nil
}

func (s *Store) listPGOrgIDs(ctx context.Context) ([]string, error) {
	var orgIDs []string
	err := s.withOperatorReadTx(ctx, func(q sqlQueryRowExecutor, _ port.Dialect) error {
		var lerr error
		orgIDs, lerr = listIDsQuery(ctx, q, `SELECT id FROM organizations ORDER BY id ASC`, "organization")
		return lerr
	})
	return orgIDs, err
}

func listOrgsInTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect) ([]string, error) {
	if dialect == port.DialectPostgres {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'true', true)`); err != nil {
			return nil, fmt.Errorf("vault: set app.is_instance_operator: %w", err)
		}
	}
	return listIDsQuery(ctx, tx, `SELECT id FROM organizations ORDER BY id ASC`, "organization")
}

func setOrgRLSTxQuery(ctx context.Context, tx *sql.Tx, dialect port.Dialect, orgID string) error {
	if dialect != port.DialectPostgres {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID); err != nil {
		return fmt.Errorf("vault: set app.current_org_id: %w", err)
	}
	return nil
}

func listIDsQuery(ctx context.Context, q sqlQueryRowExecutor, query, label string) ([]string, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("vault: list %s: %w", label, err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("vault: scan %s id: %w", label, serr)
		}
		ids = append(ids, id)
	}
	ierr := rows.Err()
	_ = rows.Close()
	if ierr != nil {
		return nil, fmt.Errorf("vault: iterate %s: %w", label, ierr)
	}
	return ids, nil
}
