// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// validSM02Transitions defines canonical ProviderConnection state transitions (SM-02).
var validSM02Transitions = map[string]map[string]struct{}{
	ConnStateDraft: {
		ConnStateValidating: {},
		ConnStateRevoked:    {},
		ConnStateArchived:   {},
	},
	ConnStateValidating: {
		ConnStateActive:  {},
		ConnStateDraft:   {},
		ConnStateRevoked: {},
	},
	ConnStateActive: {
		ConnStateDegraded:       {},
		ConnStateAuthExpired:    {},
		ConnStateSuspendedAdmin: {},
		ConnStateRevoked:        {},
	},
	ConnStateDegraded: {
		ConnStateActive:         {},
		ConnStateAuthExpired:    {},
		ConnStateSuspendedAdmin: {},
		ConnStateRevoked:        {},
	},
	ConnStateAuthExpired: {
		ConnStateValidating:     {},
		ConnStateSuspendedAdmin: {},
		ConnStateRevoked:        {},
	},
	ConnStateSuspendedAdmin: {
		ConnStateValidating: {},
		ConnStateRevoked:    {},
	},
	ConnStateRevoked: {
		ConnStateArchived: {},
	},
	ConnStateArchived: {},
}

// IsValidSM02Transition reports whether transitioning from -> to obeys SM-02.
func IsValidSM02Transition(from, to string) bool {
	targets, ok := validSM02Transitions[from]
	if !ok {
		return false
	}
	_, allowed := targets[to]
	return allowed
}

// TransitionConnectionState enforces SM-02 + optimistic locking, appends to config_changelog
// (marking emergency=true for REVOKED / SUSPENDED_BY_ADMIN / AUTH_EXPIRED_REAUTH_REQUIRED),
// and synchronously refreshes ConfigSnapshot. If the DB commit succeeds and only snapshot
// synchronization fails, the committed ProviderConnection is returned alongside ErrSnapshotSyncFailed.
func (r *Repository) TransitionConnectionState(ctx context.Context, orgID, connectionID, targetState string, expectedVersion int64, reason string) (*ProviderConnection, error) {
	if _, err := r.transitionConnectionStateDB(ctx, orgID, connectionID, targetState, expectedVersion, reason); err != nil {
		return nil, err
	}
	syncErr := r.syncSnapshot(ctx)
	updated, readErr := r.getConnWithoutCancel(ctx, orgID, connectionID)
	return updated, errors.Join(syncErr, readErr)
}

func (r *Repository) transitionConnectionStateDB(ctx context.Context, orgID, connectionID, targetState string, expectedVersion int64, reason string) (int64, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(connectionID) == "" {
		return 0, fmt.Errorf("%w: org_id and connection_id are required", port.ErrInvalidInput)
	}
	if expectedVersion < 1 {
		return 0, fmt.Errorf("%w: expected_version >= 1 required", port.ErrInvalidInput)
	}

	now := r.nowMs()
	var prevState string
	if err := r.withTx(ctx, func(tx *sql.Tx) error {
		var currentVer int64
		err := tx.QueryRowContext(ctx, `
			SELECT state, version FROM provider_connections
			WHERE org_id = ? AND id = ?`, orgID, connectionID,
		).Scan(&prevState, &currentVer)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: provider_connection %q", port.ErrNotFound, connectionID)
		}
		if err != nil {
			return mapDBError(err)
		}
		if currentVer != expectedVersion {
			return fmt.Errorf("%w: connection %q version=%d expected=%d", port.ErrVersionConflict, connectionID, currentVer, expectedVersion)
		}
		if !IsValidSM02Transition(prevState, targetState) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidStateTransition, prevState, targetState)
		}
		if targetState == ConnStateActive {
			if aerr := r.ensureConnectionAuthorized(ctx, tx, orgID, connectionID, ErrConnectionNotReady); aerr != nil {
				return aerr
			}
		}

		res, err := tx.ExecContext(ctx, `
			UPDATE provider_connections
			SET state = ?, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			targetState, now, orgID, connectionID, expectedVersion,
		)
		if err != nil {
			return mapDBError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: concurrent update on connection %q", port.ErrVersionConflict, connectionID)
		}

		mutType := port.MutationUpsert
		emergency := false
		switch targetState {
		case ConnStateRevoked:
			mutType = port.MutationRevoke
			emergency = true
		case ConnStateSuspendedAdmin, ConnStateAuthExpired:
			emergency = true
		}
		if err := appendChangelogTx(ctx, tx, &orgID, "provider_connection", connectionID, mutType, emergency, now); err != nil {
			return err
		}
		auditID, idErr := idgen.New(idgen.PrefixAudit)
		if idErr != nil {
			return idErr
		}
		metaBytes, merr := json.Marshal(map[string]string{
			"from":   prevState,
			"to":     targetState,
			"reason": strings.TrimSpace(reason),
		})
		if merr != nil {
			return merr
		}
		if aerr := r.audit.AppendTx(ctx, tx, port.AuditEvent{
			ID:           auditID,
			CreatedAtMs:  now,
			OrgID:        &orgID,
			ActorKind:    "SYSTEM",
			ActorID:      "catalog",
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "provider_connection.state_transition",
			TargetType:   "provider_connection",
			TargetID:     connectionID,
			MetadataJSON: string(metaBytes),
		}); aerr != nil {
			return aerr
		}
		return nil
	}); err != nil {
		return 0, err
	}

	return now, nil
}

func connectionAfterTransition(conn *ProviderConnection, state string, updatedAt int64) *ProviderConnection {
	updated := *conn
	updated.State = state
	updated.Version++
	updated.UpdatedAtMs = updatedAt
	return &updated
}

// ValidateConnection drives the two-phase blind probe lifecycle:
//
//	(DRAFT | AUTH_EXPIRED_REAUTH_REQUIRED | SUSPENDED_BY_ADMIN) -> VALIDATING
//	-> BuildValidationProbe (Extension Runtime)
//	-> BlindHostCredentialBroker.ExecuteWithHostConstraint (SSRFGuard + Secret Injection)
//	-> VerifyValidationResult (Extension Runtime)
//	-> ACTIVE (valid) or DRAFT (invalid probe/auth)
func (r *Repository) ValidateConnection(
	ctx context.Context,
	orgID, connectionID string,
	runtime extport.ProviderRuntime,
	brk *broker.BlindHostCredentialBroker,
) (*extport.ValidationVerdict, *ProviderConnection, error) {
	if runtime == nil || brk == nil {
		return nil, nil, fmt.Errorf("%w: runtime and BlindHostCredentialBroker are required", port.ErrInvalidInput)
	}

	cur, err := r.GetProviderConnection(ctx, orgID, connectionID)
	if err != nil {
		return nil, nil, err
	}
	if cur.AuthMode != AuthModeNone && cur.AuthMode != AuthModeWorkloadIdentity &&
		cur.CredentialID == nil && len(cur.SecretBindings) == 0 {
		return nil, nil, fmt.Errorf("%w: auth_mode %s requires bound credential slot before validation", port.ErrInvalidInput, cur.AuthMode)
	}
	if aerr := r.ensureConnectionAuthorized(ctx, r.db, orgID, connectionID, ErrConnectionNotReady); aerr != nil {
		verdict := extport.ValidationVerdict{
			Valid:            false,
			FailureCode:      "authorization_revoked",
			SanitizedMessage: aerr.Error(),
		}
		return &verdict, cur, aerr
	}

	validatingAt, err := r.transitionConnectionStateDB(ctx, orgID, connectionID, ConnStateValidating, cur.Version, "probe_started")
	if err != nil {
		if errors.Is(err, port.ErrVersionConflict) {
			curNow, _ := r.getConnWithoutCancel(ctx, orgID, connectionID)
			return nil, curNow, err
		}
		return nil, nil, err
	}
	validatingConn := connectionAfterTransition(cur, ConnStateValidating, validatingAt)
	if syncErr := r.syncSnapshot(ctx); syncErr != nil {
		// VALIDATING already committed in DB; revert VALIDATING -> DRAFT while respecting
		// optimistic locking (e.g., concurrent REVOKED) so the caller can safely retry.
		reverted, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "validating_snapshot_sync_failed")
		return nil, reverted, errors.Join(syncErr, revertErr)
	}

	probeReqID, err := idgen.New(idgen.PrefixTrace)
	if err != nil {
		probeReqID = "trc_probe_" + connectionID
	}
	mergedConfig, err := mergeConnectionConfigJSON(validatingConn.EndpointBaseURL, validatingConn.ConfigJSON)
	if err != nil {
		updated, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "config_merge_error")
		return nil, updated, errors.Join(err, revertErr)
	}

	slotNames := make([]string, 0, len(validatingConn.SecretBindings))
	for _, b := range validatingConn.SecretBindings {
		slotNames = append(slotNames, b.SlotName)
	}

	probeSpec, err := runtime.BuildValidationProbe(ctx, extport.ValidationProbeInput{
		RequestID:       probeReqID,
		ConfigJSON:      mergedConfig,
		SecretSlotNames: slotNames,
	})
	if err != nil {
		updated, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "build_probe_failed")
		return nil, updated, errors.Join(err, revertErr)
	}

	egressReq, hostConstraint, err := buildBrokerProbeRequest(orgID, connectionID, probeReqID, validatingConn, probeSpec)
	if err != nil {
		updated, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "broker_request_invalid")
		return nil, updated, errors.Join(err, revertErr)
	}

	egressResp, err := brk.ExecuteWithHostConstraint(ctx, egressReq, hostConstraint)
	if err != nil {
		updated, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "probe_egress_failed")
		verdict := &extport.ValidationVerdict{
			Valid:            false,
			FailureCode:      "probe_egress_failed",
			SanitizedMessage: err.Error(),
		}
		return verdict, updated, errors.Join(err, revertErr)
	}

	verdict, err := runtime.VerifyValidationResult(ctx, extport.ValidationResultInput{
		RequestID:        probeReqID,
		HTTPStatus:       int32(egressResp.StatusCode),
		SanitizedHeaders: flattenHeaders(egressResp.SanitizedHeaders),
		ResponseBody:     egressResp.Body,
	})
	if err != nil {
		updated, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "verify_probe_error")
		return nil, updated, errors.Join(err, revertErr)
	}

	if !verdict.Valid {
		updated, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, verdict.FailureCode)
		return &verdict, updated, revertErr
	}
	activeAt, terr := r.transitionConnectionStateDB(ctx, orgID, connectionID, ConnStateActive, validatingConn.Version, "probe_passed")
	if terr != nil {
		verdict.Valid = false
		verdict.FailureCode = "authorization_revoked"
		verdict.SanitizedMessage = "connection or extension authorization revoked prior to ACTIVE commit"
		if errors.Is(terr, port.ErrVersionConflict) {
			curNow, _ := r.getConnWithoutCancel(ctx, orgID, connectionID)
			return &verdict, curNow, terr
		}
		reverted, revertErr := r.rollbackValidatingToDraft(ctx, validatingConn, "authorization_revoked")
		return &verdict, reverted, errors.Join(terr, revertErr)
	}
	updated := connectionAfterTransition(validatingConn, ConnStateActive, activeAt)
	if syncErr := r.syncSnapshot(ctx); syncErr != nil {
		// ACTIVE already committed in DB; return the actual committed connection and ErrSnapshotSyncFailed
		// instead of issuing a stale-version rollback against VALIDATING.
		return &verdict, updated, syncErr
	}
	return &verdict, updated, nil
}

func (r *Repository) getConnWithoutCancel(ctx context.Context, orgID, connectionID string) (*ProviderConnection, error) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.GetProviderConnection(readCtx, orgID, connectionID)
}

func (r *Repository) rollbackValidatingToDraft(ctx context.Context, conn *ProviderConnection, reason string) (*ProviderConnection, error) {
	revertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	draftAt, err := r.transitionConnectionStateDB(revertCtx, conn.OrgID, conn.ID, ConnStateDraft, conn.Version, reason)
	if err != nil {
		if errors.Is(err, port.ErrVersionConflict) {
			curNow, readErr := r.GetProviderConnection(revertCtx, conn.OrgID, conn.ID)
			if readErr == nil {
				return curNow, err
			}
		}
		return nil, err
	}
	reverted := connectionAfterTransition(conn, ConnStateDraft, draftAt)
	if syncErr := r.syncSnapshot(revertCtx); syncErr != nil {
		return reverted, syncErr
	}
	return reverted, nil
}

// RecordRuntimeHealthOutcome applies SM-02 health feedback from normalized upstream inference errors.
func (r *Repository) RecordRuntimeHealthOutcome(ctx context.Context, orgID, connectionID string, normErr *extport.CanonicalProviderError) (*ProviderConnection, error) {
	cur, err := r.GetProviderConnection(ctx, orgID, connectionID)
	if err != nil {
		return nil, err
	}
	if normErr == nil {
		if cur.State == ConnStateDegraded {
			return r.TransitionConnectionState(ctx, orgID, connectionID, ConnStateActive, cur.Version, "runtime_recovered")
		}
		return cur, nil
	}
	switch {
	case normErr.TriggerConnectionReauth && (cur.State == ConnStateActive || cur.State == ConnStateDegraded):
		return r.TransitionConnectionState(ctx, orgID, connectionID, ConnStateAuthExpired, cur.Version, normErr.CanonicalCode)
	case normErr.RetryablePreStream && cur.State == ConnStateActive:
		return r.TransitionConnectionState(ctx, orgID, connectionID, ConnStateDegraded, cur.Version, normErr.CanonicalCode)
	default:
		return cur, nil
	}
}

// ConvertCredentialInjections translates runtime extension port injections to BlindBroker instructions.
func ConvertCredentialInjections(in []extport.CredentialInjectInstruction) ([]broker.CredentialInjection, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]broker.CredentialInjection, 0, len(in))
	for _, item := range in {
		var loc broker.TargetLocation
		switch item.TargetLocation {
		case extport.CredentialTargetLocationHeader:
			loc = broker.TargetHeader
		case extport.CredentialTargetLocationQueryParam:
			loc = broker.TargetQueryParam
		case extport.CredentialTargetLocationHostCryptoSign:
			loc = broker.TargetHostCryptoSign
		default:
			return nil, fmt.Errorf("%w: unsupported credential target_location %q", port.ErrInvalidInput, item.TargetLocation)
		}
		ci := broker.CredentialInjection{
			SecretSlotName: item.SecretSlotName,
			Location:       loc,
			FieldName:      item.FieldName,
			ValuePrefix:    item.ValuePrefix,
		}
		if item.CryptoSign != nil {
			sigAlg, ok := mapSigningAlgorithm(item.CryptoSign.Algorithm)
			if !ok {
				return nil, fmt.Errorf("%w: unsupported signing algorithm %q", port.ErrInvalidInput, item.CryptoSign.Algorithm)
			}
			sigEnc, ok := mapSignatureEncoding(item.CryptoSign.OutputEncoding)
			if !ok {
				return nil, fmt.Errorf("%w: unsupported signature encoding %q", port.ErrInvalidInput, item.CryptoSign.OutputEncoding)
			}
			ci.CryptoSign = &broker.CryptoSignSpec{
				Algorithm:                sigAlg,
				CanonicalPayload:         item.CryptoSign.CanonicalPayload,
				OutputEncoding:           sigEnc,
				AWSRegion:                item.CryptoSign.AWSRegion,
				AWSService:               item.CryptoSign.AWSService,
				SigningTimestampEpochSec: item.CryptoSign.SigningTimestampEpochSec,
				SignedHeaders:            item.CryptoSign.SignedHeaders,
			}
		}
		out = append(out, ci)
	}
	return out, nil
}

func mapSigningAlgorithm(a extport.SigningAlgorithm) (broker.SigningAlgorithm, bool) {
	switch a {
	case extport.SigningAlgorithmHMACSHA256:
		return broker.SigningHMACSHA256, true
	case extport.SigningAlgorithmAWSSigV4:
		return broker.SigningAWSSigV4, true
	case extport.SigningAlgorithmRSASHA256:
		return broker.SigningRSASHA256, true
	default:
		return "", false
	}
}

func mapSignatureEncoding(e extport.SignatureEncoding) (broker.SignatureEncoding, bool) {
	switch e {
	case extport.SignatureEncodingHexLower, extport.SignatureEncodingUnspecified:
		return broker.EncodingHexLower, true
	case extport.SignatureEncodingBase64:
		return broker.EncodingBase64, true
	case extport.SignatureEncodingBase64URL:
		return broker.EncodingBase64URL, true
	default:
		return "", false
	}
}

func buildBrokerProbeRequest(
	orgID, connectionID, reqID string,
	conn *ProviderConnection,
	spec extport.UpstreamHttpSpec,
) (broker.EgressRequest, broker.HostEgressConstraint, error) {
	inj, err := ConvertCredentialInjections(spec.CredentialInjections)
	if err != nil {
		return broker.EgressRequest{}, broker.HostEgressConstraint{}, err
	}
	narrowPolicy, err := buildEgressPolicyFromURL(conn.EndpointBaseURL, conn.AllowPrivateIP, conn.AllowedCIDRs)
	if err != nil {
		return broker.EgressRequest{}, broker.HostEgressConstraint{}, err
	}

	req := broker.EgressRequest{
		OrgID:                orgID,
		ConnectionID:         connectionID,
		Method:               spec.Method,
		TargetURL:            spec.TargetURL,
		SafeHeaders:          spec.SafeHeaders,
		BodyBytes:            spec.BodyBytes,
		CredentialInjections: inj,
		ActorKind:            "SYSTEM",
		ActorID:              "catalog_probe",
		EntryPoint:           port.AuditEntryPointAdminAPI,
		RequestID:            reqID,
	}
	return req, broker.HostEgressConstraint{NarrowPolicy: narrowPolicy}, nil
}

func mergeConnectionConfigJSON(endpointBaseURL, rawConfigJSON string) (string, error) {
	m := make(map[string]any)
	trimmed := strings.TrimSpace(rawConfigJSON)
	if trimmed != "" && trimmed != "{}" {
		if err := json.Unmarshal([]byte(trimmed), &m); err != nil || m == nil {
			return "", fmt.Errorf("%w: invalid connection config_json (must be JSON object)", port.ErrInvalidInput)
		}
	}
	// Authoritative endpoint_base_url from connection row always wins over config_json.
	m["endpoint_base_url"] = endpointBaseURL
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func flattenHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = strings.Join(vals, ", ")
		}
	}
	return out
}
