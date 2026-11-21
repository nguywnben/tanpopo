// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/firewall"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const approvalStatusTool = "tanpopo__check_approval_and_resume"
const approvalTTL = 30 * time.Minute

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Meta      struct {
		ProgressToken json.RawMessage `json:"progressToken"`
		Token         json.RawMessage `json:"tanpopoApprovalToken"`
		Mode          string          `json:"tanpopoApprovalMode"`
	} `json:"_meta"`
}

func (p toolCallParams) replayToken() (string, error) {
	if p.Meta.Token == nil {
		return "", nil
	}
	var token string
	if len(p.Meta.Token) > 256 || json.Unmarshal(p.Meta.Token, &token) != nil || token == "" {
		return "", errors.New("invalid approval token")
	}
	return token, nil
}

func (s *Server) prepareTool(ctx context.Context, sc scope, rpcID json.RawMessage, params toolCallParams) (toolRecord, *response) {
	slug, name, ok := splitToolName(params.Name)
	if !ok {
		r := rpcFailure(rpcID, -32602, "Invalid namespaced tool name", nil)
		return toolRecord{}, &r
	}
	record, err := s.catalog.getGrantedTool(ctx, sc, slug, name)
	var failure response
	switch {
	case err != nil:
		failure = rpcFailure(rpcID, -32003, "tool_access_denied", nil)
	case record.DriftLocked || record.ApprovedHash != record.ObservedHash:
		failure = rpcFailure(rpcID, -32003, "schema_hash_drift_blocked", nil)
	case record.Server.Transport != "STREAMABLE_HTTP":
		failure = rpcFailure(rpcID, -32003, "unsupported_transport", nil)
	case validateArgumentsContext(ctx, record.Schema, params.Arguments) != nil:
		failure = rpcFailure(rpcID, -32602, "Invalid tool arguments", nil)
	default:
		return record, nil
	}
	return toolRecord{}, &failure
}

// Include the system tool only in catalogs with an authorized destructive tool.
// A registered upstream cannot shadow the system tool.
func withApprovalStatusTool(tools []toolRecord) []toolRecord {
	visible := tools[:0]
	hasDestructive := false
	for _, t := range tools {
		if t.Server.Slug+"__"+t.Name == approvalStatusTool {
			continue
		}
		visible = append(visible, t)
		hasDestructive = hasDestructive || firewall.NormalizeClass(t.SideEffectClass) == firewall.Destructive
	}
	if hasDestructive {
		visible = append(visible, toolRecord{Server: serverRecord{Slug: "tanpopo"}, Name: "check_approval_and_resume",
			Schema: json.RawMessage(`{"description":"Check an approval request. To resume, include the original arguments and _meta.tanpopoApprovalToken.","inputSchema":{"type":"object","properties":{"approval_request_id":{"type":"string"},"arguments":{"type":"object"}},"required":["approval_request_id"],"additionalProperties":false}}`)})
	}
	return visible
}

// With just the ID, the system tool polls. Execution additionally requires
// the original arguments and nonce. It uses the same dispatch path as a direct replay.
func (s *Server) checkApproval(ctx context.Context, sc scope, rpcID json.RawMessage, params toolCallParams, token string) (toolCallParams, *response) {
	var args struct {
		ID        string          `json:"approval_request_id"`
		Arguments json.RawMessage `json:"arguments"`
	}
	decoder := json.NewDecoder(bytes.NewReader(params.Arguments))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil || args.ID == "" || (token == "") != (args.Arguments == nil) {
		reply := rpcFailure(rpcID, -32602, "Expected approval_request_id and, for replay, arguments and approval token", nil)
		return toolCallParams{}, &reply
	}
	request, err := s.approvals.Lookup(ctx, sc.OrgID, sc.KeyID, args.ID)
	if err != nil {
		reply := approvalError(rpcID, err)
		return toolCallParams{}, &reply
	}
	record, err := s.catalog.getGrantedTool(ctx, sc, request.Binding.ServerSlug, request.Binding.ToolName)
	if err != nil || record.Server.Slug+"__"+record.Name == approvalStatusTool {
		reply := rpcFailure(rpcID, -32003, "tool_access_denied", nil)
		return toolCallParams{}, &reply
	}
	if request.Binding.ServerID != "" && (record.Server.ID != request.Binding.ServerID || record.ID != request.Binding.ToolID || record.ApprovedHash != request.Binding.ApprovedSchemaHash) {
		reply := rpcFailure(rpcID, -32003, "approval_target_changed", nil)
		return toolCallParams{}, &reply
	}
	if token == "" {
		reply := s.approvalResult(ctx, rpcID, request, "", "")
		return toolCallParams{}, &reply
	}

	// Consume selects by nonce. Bind this status-tool ID to that same immutable
	// nonce first, even if two requests were created for identical arguments.
	digest := sha256.Sum256([]byte(token))
	d := port.DetectDialect(ctx, s.cfg.DB)
	tx, err := s.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		reply := rpcFailure(rpcID, -32603, "Internal error", nil)
		return toolCallParams{}, &reply
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, d, sc.OrgID); err != nil {
		reply := rpcFailure(rpcID, -32603, "Internal error", nil)
		return toolCallParams{}, &reply
	}
	var storedHash []byte
	err = tx.QueryRowContext(ctx, port.Rebind(d, `SELECT pre_approval_token_hash FROM approval_requests
        WHERE id=? AND org_id=? AND requester_virtual_key_id=?`), args.ID, sc.OrgID, sc.KeyID).Scan(&storedHash)
	_ = tx.Rollback()
	if err != nil {
		reply := rpcFailure(rpcID, -32603, "Internal error", nil)
		return toolCallParams{}, &reply
	}
	if len(storedHash) == 0 || !hmac.Equal(storedHash, digest[:]) {
		reply := approvalError(rpcID, &approval.Error{Code: approval.CodeInvalidToken})
		return toolCallParams{}, &reply
	}
	params.Name = record.Server.Slug + "__" + record.Name
	params.Arguments = args.Arguments
	return params, nil
}

// Only the POST that created a request can return its nonce. Polls contain
// status and replay instructions, and never recover or rotate the nonce.
func (s *Server) approvalResult(ctx context.Context, rpcID json.RawMessage, request approval.Request, newToken, notice string) response {
	state := string(request.State)
	details := map[string]any{"approval_request_id": request.ID, "expires_at": request.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	message := "Approval request is " + state + "."
	if request.State == approval.StatePending {
		state = "APPROVAL_PENDING"
		d := port.DetectDialect(ctx, s.cfg.DB)
		tx, err := s.cfg.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return rpcFailure(rpcID, -32603, "Internal error", nil)
		}
		defer func() { _ = tx.Rollback() }()
		if err := port.SetOrgRLSTx(ctx, tx, d, request.Binding.OrgID); err != nil {
			return rpcFailure(rpcID, -32603, "Internal error", nil)
		}
		var hash string
		if err := tx.QueryRowContext(ctx, port.Rebind(d, `SELECT params_jcs_sha256 FROM approval_requests WHERE id=? AND org_id=? AND requester_virtual_key_id=?`),
			request.ID, request.Binding.OrgID, request.Binding.VirtualKeyID).Scan(&hash); err != nil {
			return rpcFailure(rpcID, -32603, "Internal error", nil)
		}
		details["approval_binding_hash"] = hash
		details["resume_tool"] = approvalStatusTool
		message = "Approval is pending. Check the request with " + approvalStatusTool + "."
		if newToken != "" {
			details["pre_approval_token"] = newToken
		}
	} else if request.State == approval.StateApproved {
		message = "Approved. Replay the original tool and arguments with the token in _meta.tanpopoApprovalToken."
	}
	if notice != "" {
		message += " " + notice
	}
	details["status"] = state
	return rpcResponse(rpcID, map[string]any{"isError": request.State != approval.StateApproved, "structuredContent": details,
		"content": []map[string]string{{"type": "text", "text": message}}})
}

func (s *Server) requestApproval(ctx context.Context, id identity, sessionID string, rpcID json.RawMessage, params toolCallParams, binding approval.Binding) (string, *response) {
	var principal string
	if id.match.Key.OwnerPrincipalID != nil {
		principal = *id.match.Key.OwnerPrincipalID
	}
	credentialPrincipal := id.match.Key.ActiveKeyRecipientPrincipalID
	if id.match.MatchedPrevious {
		credentialPrincipal = id.match.Key.PreviousKeyRecipientPrincipalID
	}
	if credentialPrincipal != nil {
		binding.CredentialPrincipalID = *credentialPrincipal
	}
	created, err := s.approvals.Create(ctx, binding, principal, s.cfg.Now().Add(approvalTTL))
	if err != nil {
		r := approvalError(rpcID, err)
		return "", &r
	}
	interactive := params.Meta.Mode == "sync" || (params.Meta.Mode == "" && len(params.Meta.ProgressToken) > 0)
	if interactive {
		waitCtx, cancel := context.WithTimeout(ctx, s.cfg.ApprovalWait)
		defer cancel()
		for step := 1; waitCtx.Err() == nil; step++ {
			s.publishProgress(sessionID, params.Meta.ProgressToken, step, approvalProgressTotal)
			record, err := s.approvals.Wait(waitCtx, binding.OrgID, binding.VirtualKeyID, created.ID, 5*time.Second)
			if err == nil {
				if record.State == approval.StateApproved {
					return created.PreApprovalToken, nil
				}
				r := s.approvalResult(ctx, rpcID, record, "", "")
				return "", &r
			}
			if waitCtx.Err() != nil {
				break
			}
			if code, _ := approval.ErrorCodeOf(err); code != approval.CodeWaitTimeout {
				r := approvalError(rpcID, err)
				return "", &r
			}
		}
		if ctx.Err() != nil {
			r := rpcFailure(rpcID, -32000, "approval_wait_cancelled", nil)
			return "", &r
		}
	}
	r := s.approvalResult(ctx, rpcID, created.Request, created.PreApprovalToken, "")
	return "", &r
}

const approvalProgressTotal = int(approval.MaxSynchronousWait/(5*time.Second)) + 2

func approvalError(rpcID json.RawMessage, err error) response {
	code, ok := approval.ErrorCodeOf(err)
	if !ok || code == approval.CodeStorage {
		return rpcFailure(rpcID, -32603, "Internal error", nil)
	}
	return rpcFailure(rpcID, -32003, string(code), nil)
}

func (s *Server) approvalFailure(ctx context.Context, rpcID json.RawMessage, binding approval.Binding, record toolRecord, err error) response {
	if code, _ := approval.ErrorCodeOf(err); code == approval.CodeParameterHashMismatch {
		eventID, idErr := idgen.New("aud")
		if idErr != nil {
			return rpcFailure(rpcID, -32603, "Internal error", nil)
		}
		if auditErr := s.cfg.Audit.Append(ctx, port.AuditEvent{ID: eventID, CreatedAtMs: s.cfg.Now().UnixMilli(), OrgID: &binding.OrgID,
			ActorKind: "VIRTUAL_KEY", ActorID: binding.VirtualKeyID, EntryPoint: "data_plane_guard", EventType: "mcp.approval_parameter_hash_mismatch",
			TargetType: "mcp_tool", TargetID: record.ID, MetadataJSON: `{"reason":"parameter_hash_mismatch"}`}); auditErr != nil {
			return rpcFailure(rpcID, -32603, "Internal error", nil)
		}
	}
	return approvalError(rpcID, err)
}

// These two tool execution failures have explicit HTTP contracts in dataplane-v1.
func writeInvocationError(w http.ResponseWriter, code string) bool {
	var status int
	var kind, message string
	switch code {
	case firewall.AmbiguousSideEffectTimeoutCode:
		status, kind, message = http.StatusGatewayTimeout, "side_effect_timeout_error", "The upstream call may have taken effect."
	case string(approval.CodeParameterHashMismatch):
		status, kind, message = http.StatusForbidden, "permission_error", "The approval does not match this tool call."
	default:
		return false
	}
	id, err := idgen.New(idgen.PrefixTrace)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error")
		return true
	}
	id = "req_" + strings.TrimPrefix(id, idgen.PrefixTrace+"_")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Tanpopo-Request-Id", id)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "type": kind, "message": message, "request_id": id, "retryable": false}})
	return true
}
