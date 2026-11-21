// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package foureyes

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func setupFourEyesTest(t *testing.T) (*sqlite.Store, *Service, string, string, string, *atomic.Int64) {
	t.Helper()
	ctx := context.Background()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "foureyes.db")})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := &atomic.Int64{}
	now.Store(1760000000000)

	aud, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB()})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	svc, err := NewService(Config{
		DB:        st.RawDB(),
		AuditSink: aud,
		NowMs:     func() int64 { return now.Load() },
	})
	if err != nil {
		t.Fatalf("foureyes.NewService: %v", err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "fe-org", DisplayName: "FE Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = 1 WHERE id = ?`, org.ID); err != nil {
		t.Fatalf("enable four-eyes: %v", err)
	}

	admin1 := idgen.MustNew(idgen.PrefixPrincipal)
	admin2 := idgen.MustNew(idgen.PrefixPrincipal)
	for idx, pid := range []string{admin1, admin2} {
		email := "admin1@example.test"
		if idx == 1 {
			email = "admin2@example.test"
		}
		if _, err := st.RawDB().ExecContext(ctx, `
			INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'HUMAN_LOCAL', ?, 'ACTIVE', 1000, 1000)`,
			pid, org.ID, email,
		); err != nil {
			t.Fatalf("insert principal: %v", err)
		}
	}

	return st, svc, org.ID, admin1, admin2, now
}

func TestFourEyesDualControlEnforcesSecondApproverAC2(t *testing.T) {
	ctx := context.Background()
	st, svc, orgID, admin1, admin2, now := setupFourEyesTest(t)

	required, err := svc.RequiresFourEyes(ctx, &orgID)
	if err != nil || !required {
		t.Fatalf("expected RequiresFourEyes=true, got required=%v err=%v", required, err)
	}

	req, err := svc.Initiate(ctx, InitiateParams{
		OrgID:                &orgID,
		ActionType:           ActionExtensionActivateExternal,
		TargetType:           "extension_installation",
		TargetID:             "ins_test_ext_001",
		PayloadJSON:          json.RawMessage(`{"enable_immediately":true,"acknowledge_unverified_external":true}`),
		InitiatorPrincipalID: admin1,
	})
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if req.State != StatePendingSecondApprover || req.Version != 1 {
		t.Fatalf("unexpected initial request state=%s ver=%d", req.State, req.Version)
	}

	// Self-approval MUST fail with ErrSelfApprovalForbidden (403 self_approval_forbidden).
	appliedCount := 0
	if _, err := svc.Decide(ctx, DecideParams{
		OrgID:               &orgID,
		RequestID:           req.ID,
		ApproverPrincipalID: admin1,
		Decision:            DecisionApprove,
		ExpectedVersion:     req.Version,
	}, func(context.Context, *ChangeRequest) error {
		appliedCount++
		return nil
	}); !errors.Is(err, ErrSelfApprovalForbidden) {
		t.Fatalf("expected ErrSelfApprovalForbidden on self-approval, got %v", err)
	}
	if appliedCount != 0 {
		t.Fatalf("apply callback must NOT run on self-approval")
	}

	// Admin 2 approves -> transitions atomically to APPROVED_APPLIED and invokes applyFn.
	approved, err := svc.Decide(ctx, DecideParams{
		OrgID:               &orgID,
		RequestID:           req.ID,
		ApproverPrincipalID: admin2,
		Decision:            DecisionApprove,
		Reason:              "verified checksum and external provenance",
		ExpectedVersion:     req.Version,
	}, func(_ context.Context, cr *ChangeRequest) error {
		appliedCount++
		if cr.State != StatePendingSecondApprover || cr.ApproverPrincipalID == nil || *cr.ApproverPrincipalID != admin2 {
			t.Fatalf("unexpected callback request snapshot: %+v", cr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Decide APPROVE by admin2: %v", err)
	}
	if approved.State != StateApprovedApplied || approved.Version != 2 || appliedCount != 1 {
		t.Fatalf("unexpected approved state: %+v appliedCount=%d", approved, appliedCount)
	}

	// Failed applyFn MUST roll back approver claim and keep state PENDING_SECOND_APPROVER.
	failReq, err := svc.Initiate(ctx, InitiateParams{
		OrgID:                &orgID,
		ActionType:           ActionKillSwitchClear,
		TargetType:           "kill_switch",
		TargetID:             "ksw_retry",
		PayloadJSON:          json.RawMessage(`{"clear_reason":"retry check"}`),
		InitiatorPrincipalID: admin1,
	})
	if err != nil {
		t.Fatalf("Initiate failReq: %v", err)
	}
	if _, err := svc.Decide(ctx, DecideParams{
		OrgID:               &orgID,
		RequestID:           failReq.ID,
		ApproverPrincipalID: admin2,
		Decision:            DecisionApprove,
		ExpectedVersion:     failReq.Version,
	}, func(context.Context, *ChangeRequest) error {
		return errors.New("simulated downstream failure")
	}); err == nil {
		t.Fatalf("expected Decide error when applyFn fails")
	}
	afterFail, err := svc.Get(ctx, &orgID, failReq.ID)
	if err != nil || afterFail.State != StatePendingSecondApprover || afterFail.ApproverPrincipalID != nil {
		t.Fatalf("expected PENDING_SECOND_APPROVER with cleared approver after apply failure, got %+v err=%v", afterFail, err)
	}

	// Replay after APPROVED_APPLIED must fail closed with ErrInvalidStateTransition.
	if _, err := svc.Decide(ctx, DecideParams{
		OrgID:               &orgID,
		RequestID:           req.ID,
		ApproverPrincipalID: admin2,
		Decision:            DecisionApprove,
		ExpectedVersion:     approved.Version,
	}, nil); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on replay, got %v", err)
	}

	// Expiry enforcement.
	expReq, err := svc.Initiate(ctx, InitiateParams{
		OrgID:                &orgID,
		ActionType:           ActionKillSwitchClear,
		TargetType:           "kill_switch",
		TargetID:             "ksw_1",
		PayloadJSON:          json.RawMessage(`{"clear_reason":"incident resolved"}`),
		InitiatorPrincipalID: admin1,
		TTL:                  time.Minute,
	})
	if err != nil {
		t.Fatalf("Initiate expiring request: %v", err)
	}
	now.Add(2 * time.Minute.Milliseconds())
	if _, err := svc.Decide(ctx, DecideParams{
		OrgID:               &orgID,
		RequestID:           expReq.ID,
		ApproverPrincipalID: admin2,
		Decision:            DecisionApprove,
		ExpectedVersion:     1,
	}, nil); !errors.Is(err, ErrRequestExpired) {
		t.Fatalf("expected ErrRequestExpired, got %v", err)
	}
	reloaded, err := svc.Get(ctx, &orgID, expReq.ID)
	if err != nil || reloaded.State != StateExpired {
		t.Fatalf("expected stored state EXPIRED, got %+v err=%v", reloaded, err)
	}

	// Verify audit chain remained valid across all Four-Eyes events.
	vRes, err := audit.VerifyAuditChain(ctx, st.RawDB(), audit.VerifyOptions{ChainKeys: []string{orgID}})
	if err != nil || !vRes.Valid {
		t.Fatalf("VerifyAuditChain failed: valid=%v err=%v", vRes != nil && vRes.Valid, err)
	}
}
