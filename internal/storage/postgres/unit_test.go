// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type unitPlainSink struct{}

func (*unitPlainSink) Append(context.Context, port.AuditEvent) error { return nil }

type unitTxSink struct {
	events []port.AuditEvent
}

func (u *unitTxSink) Append(_ context.Context, ev port.AuditEvent) error {
	u.events = append(u.events, ev)
	return nil
}

func (u *unitTxSink) AppendTx(_ context.Context, _ *sql.Tx, ev port.AuditEvent) error {
	u.events = append(u.events, ev)
	return nil
}

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

func TestPostgresAuditSinkAndTenancyAuditUnit(t *testing.T) {
	st := &Store{}
	if err := st.SetAuditSink(nil); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("SetAuditSink(nil) = %v, want ErrInvalidInput", err)
	}
	if err := st.SetAuditSink(&unitPlainSink{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("SetAuditSink(non-TxAuditSink) = %v, want ErrInvalidInput", err)
	}
	sink := &unitTxSink{}
	if err := st.SetAuditSink(sink); err != nil {
		t.Fatalf("SetAuditSink(txSink): %v", err)
	}
	if st.getAuditSink() != sink {
		t.Fatalf("getAuditSink did not return configured sink")
	}

	ctx := context.Background()
	orgID := "org_unit_1"
	if err := emitTenancyAuditTx(ctx, (*sql.DB)(nil), nil, &orgID, "organization.created", "organization", orgID, nil, 100); err != nil {
		t.Fatalf("nil auditSink = %v", err)
	}
	if err := emitTenancyAuditTx(ctx, (*sql.DB)(nil), &unitPlainSink{}, &orgID, "organization.created", "organization", orgID, nil, 100); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("non-TxAuditSink = %v, want ErrInvalidInput", err)
	}
	if err := emitTenancyAuditTx(ctx, (*sql.DB)(nil), sink, &orgID, "organization.created", "organization", orgID, nil, 100); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("non-*sql.Tx exec = %v, want ErrInvalidInput", err)
	}

	actorCtx := port.WithAuditActor(ctx, port.AuditActor{
		ActorKind:  "PRINCIPAL",
		ActorID:    "usr_unit_1",
		EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID:  "req_unit_1",
	})
	if err := emitTenancyAuditTx(actorCtx, &sql.Tx{}, sink, &orgID, "organization.created", "organization", orgID, map[string]any{"slug": "alpha"}, 200); err != nil {
		t.Fatalf("emitTenancyAuditTx with actor: %v", err)
	}
	if len(sink.events) != 1 || sink.events[0].ActorKind != "PRINCIPAL" || sink.events[0].ActorID != "usr_unit_1" || sink.events[0].RequestID == nil || *sink.events[0].RequestID != "req_unit_1" {
		t.Fatalf("unexpected emitted event: %+v", sink.events)
	}
	defaultActorCtx := port.WithAuditActor(ctx, port.AuditActor{ActorID: "usr_unit_2"})
	if err := emitTenancyAuditTx(defaultActorCtx, &sql.Tx{}, sink, &orgID, "organization.updated", "organization", orgID, nil, 201); err != nil {
		t.Fatalf("emitTenancyAuditTx fallback actor: %v", err)
	}
	if len(sink.events) != 2 || sink.events[1].ActorKind != "SYSTEM" || sink.events[1].EntryPoint != port.AuditEntryPointAdminAPI {
		t.Fatalf("unexpected fallback actor event: %+v", sink.events[1])
	}
	if err := emitTenancyAuditTx(ctx, &sql.Tx{}, sink, &orgID, "organization.updated", "organization", orgID, map[string]any{"bad": make(chan int)}, 202); err == nil {
		t.Fatalf("expected marshal error on unserializable metadata")
	}
}

