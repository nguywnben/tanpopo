// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"strings"

	"github.com/nguywnben/tanpopo/internal/ingress/mcp/hub"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

type planeBrokerResolver struct {
	provider *catalog.Repository
	mcp      hub.BrokerResolver
}

func (r planeBrokerResolver) ResolveSlot(ctx context.Context, orgID, connectionID, slot string) (*broker.ResolvedSlot, error) {
	if strings.HasPrefix(connectionID, "mcp_") {
		return r.mcp.ResolveSlot(ctx, orgID, connectionID, slot)
	}
	return r.provider.ResolveSlot(ctx, orgID, connectionID, slot)
}

func (r planeBrokerResolver) ResolveConnectionPolicy(ctx context.Context, orgID, connectionID string) (*egress.Policy, error) {
	if strings.HasPrefix(connectionID, "mcp_") {
		return r.mcp.ResolveConnectionPolicy(ctx, orgID, connectionID)
	}
	return r.provider.ResolveConnectionPolicy(ctx, orgID, connectionID)
}

var _ broker.CredentialBindingResolver = planeBrokerResolver{}
var _ broker.ConnectionPolicyResolver = planeBrokerResolver{}
