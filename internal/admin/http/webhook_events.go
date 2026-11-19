// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"errors"
)

func (s *Server) enqueueWebhookEvent(ctx context.Context, orgID, projectID, eventType string, payload any) error {
	if s.webhookEnqueue == nil {
		return nil
	}
	if orgID == "" {
		return errors.New("webhook event requires an organization scope")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.webhookEnqueue(ctx, orgID, projectID, eventType, raw)
}
