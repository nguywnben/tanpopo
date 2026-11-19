// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// BrokerResolver resolves MCP server egress policies and opaque credentials for the shared
// BlindHostCredentialBroker. It never reads or returns credential plaintext.
type BrokerResolver struct{ DB *sql.DB }

func (r BrokerResolver) ResolveConnectionPolicy(ctx context.Context, orgID, serverID string) (*egress.Policy, error) {
	if r.DB == nil || orgID == "" || serverID == "" {
		return nil, port.ErrInvalidInput
	}
	server, err := r.load(ctx, orgID, serverID)
	if err != nil {
		return nil, err
	}
	return server.egressPolicy()
}

func (r BrokerResolver) ResolveSlot(ctx context.Context, orgID, serverID, slotName string) (*broker.ResolvedSlot, error) {
	if r.DB == nil || orgID == "" || serverID == "" || slotName != "mcp_server" {
		return nil, port.ErrInvalidInput
	}
	server, err := r.load(ctx, orgID, serverID)
	if err != nil {
		return nil, err
	}
	if server.CredentialID == "" {
		return nil, port.ErrNotFound
	}
	parsed, err := url.Parse(server.Endpoint)
	if err != nil || parsed.Host == "" {
		return nil, port.ErrInvalidInput
	}
	var audience sql.NullString
	var credentialOwner sql.NullString
	var kind string
	err = r.DB.QueryRowContext(ctx, `
		SELECT bound_audience, owner_project_id, secret_kind
		FROM secret_credentials WHERE org_id = ? AND id = ?`, orgID, server.CredentialID).
		Scan(&audience, &credentialOwner, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load MCP credential metadata: %w", err)
	}
	if kind != "API_KEY" && kind != "OAUTH2_TOKEN_SET" && kind != "AWS_SIGV4_KEY" {
		return nil, port.ErrInvalidInput
	}
	if credentialOwner.Valid && credentialOwner.String != server.ProjectID {
		return nil, port.ErrNotFound
	}
	if !audience.Valid || !strings.EqualFold(strings.TrimSpace(audience.String), strings.TrimSpace(parsed.Host)) {
		return nil, errors.New("MCP credential audience must exactly match the server host")
	}
	return &broker.ResolvedSlot{
		OrgID: orgID, ConnectionID: serverID, SlotName: slotName, SecretID: server.CredentialID,
		InjectMode: broker.InjectModeHostBlind, BoundAudience: &audience.String,
		AllowedHosts: []string{parsed.Host}, AllowPrivateIP: server.AllowPrivateIP,
		AllowedCIDRs: append([]string(nil), server.AllowedCIDRs...), RequireTLS: parsed.Scheme == "https",
	}, nil
}

func (r BrokerResolver) load(ctx context.Context, orgID, serverID string) (serverRecord, error) {
	var s serverRecord
	var allowPrivate int
	var cidrs string
	err := r.DB.QueryRowContext(ctx, `
		SELECT id, org_id, project_id, server_slug, transport_kind, endpoint_url,
		       allow_private_ip, allowed_cidrs_json, COALESCE(credential_id, ''),
		       pinning_mode, status
		FROM mcp_servers WHERE org_id = ? AND id = ?`, orgID, serverID).Scan(
		&s.ID, &s.OrgID, &s.ProjectID, &s.Slug, &s.Transport, &s.Endpoint,
		&allowPrivate, &cidrs, &s.CredentialID, &s.PinningMode, &s.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return serverRecord{}, port.ErrNotFound
	}
	if err != nil {
		return serverRecord{}, fmt.Errorf("load MCP server egress metadata: %w", err)
	}
	if s.Status != "ACTIVE" || s.Transport != "STREAMABLE_HTTP" {
		return serverRecord{}, port.ErrNotFound
	}
	s.AllowPrivateIP = allowPrivate != 0
	if err := json.Unmarshal([]byte(cidrs), &s.AllowedCIDRs); err != nil {
		return serverRecord{}, port.ErrInvalidInput
	}
	return s, nil
}

var _ broker.CredentialBindingResolver = BrokerResolver{}
var _ broker.ConnectionPolicyResolver = BrokerResolver{}
