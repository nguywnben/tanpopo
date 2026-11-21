// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package postgres

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestPostgresInternalErrorMappers(t *testing.T) {
	if mapPGError(nil) != nil || mapPGErrorForDelete(nil) != nil {
		t.Fatalf("expected nil pass-through")
	}
	if !errors.Is(mapPGError(sql.ErrNoRows), port.ErrNotFound) {
		t.Fatalf("expected ErrNoRows -> ErrNotFound")
	}
	for _, tc := range []struct {
		code string
		want error
	}{
		{"23505", port.ErrAlreadyExists},
		{"23503", port.ErrNotFound},
		{"23514", port.ErrInvalidInput},
		{"42501", port.ErrInvalidInput},
	} {
		err := mapPGError(&pgconn.PgError{Code: tc.code, Message: "msg"})
		if !errors.Is(err, tc.want) {
			t.Fatalf("code %s = %v, want %v", tc.code, err, tc.want)
		}
	}
	if err := mapPGErrorForDelete(&pgconn.PgError{Code: "23503", Message: "fk"}); !errors.Is(err, port.ErrResourceInUse) {
		t.Fatalf("delete 23503 = %v, want ErrResourceInUse", err)
	}
	if err := mapPGErrorForDelete(&pgconn.PgError{Code: "42501", Message: "rls"}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("delete 42501 = %v, want ErrInvalidInput", err)
	}
	if !isRetryablePGError(&pgconn.PgError{Code: "40001"}) || !isRetryablePGError(&pgconn.PgError{Code: "40P01"}) || isRetryablePGError(errors.New("other")) {
		t.Fatalf("unexpected isRetryablePGError result")
	}
}
