// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func TestMemoryLimiterPrunesIdleScopesWithoutDroppingActiveStreams(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	lim := NewMemoryLimiter(Config{Now: clk.Now})
	ctx := context.Background()

	activeScope := LimitScope{OrgID: "org_p", VirtualKeyID: "vk_active"}
	lease, _, err := lim.AcquireStreamSlot(ctx, activeScope, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	rpm := int64(10)
	for i := 0; i < defaultPruneThreshold+50; i++ {
		sc := LimitScope{OrgID: "org_p", VirtualKeyID: fmt.Sprintf("vk_ephem_%d", i)}
		if _, err := lim.AllowRequest(ctx, sc, 1, Limits{RPMLimit: &rpm}); err != nil {
			t.Fatalf("scope %d AllowRequest failed: %v", i, err)
		}
	}

	clk.Advance(defaultIdleEvictAfter + time.Minute)
	if _, err := lim.AllowRequest(ctx, LimitScope{OrgID: "org_p", VirtualKeyID: "vk_trigger"}, 1, Limits{RPMLimit: &rpm}); err != nil {
		t.Fatal(err)
	}

	lim.mu.Lock()
	remaining := len(lim.scopes)
	_, activeStillPresent := lim.scopes[activeScope]
	lim.mu.Unlock()

	if !activeStillPresent {
		t.Fatal("expected active stream scope to be preserved during idle pruning")
	}
	if remaining > 10 {
		t.Fatalf("expected stale ephemeral scopes to be pruned, remaining=%d", remaining)
	}
}
