// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// RouteStrategy constants matching route_policies.strategy CHECK constraint.
const (
	RouteStrategyPriorityFailover = "PRIORITY_FAILOVER"
	RouteStrategyWeightedRR       = "WEIGHTED_ROUND_ROBIN"
	RouteStrategyWeighted         = "WEIGHTED"
	RouteStrategyLowestCost       = "LOWEST_COST"
	RouteStrategyLowestLatency    = "LOWEST_LATENCY_EWMA"
)

// DefaultMaxFailoverHops defines SPEC-CORE-01 default bounded retries (1 + 3 = 4 max attempts).
const DefaultMaxFailoverHops = 3

// ErrRouteConfigCorrupted indicates a route snapshot load failure requiring Fail-Closed rejection.
var ErrRouteConfigCorrupted = errors.New("snapshot: route configuration corrupted or unavailable")

// RouteLookupKey indexes route_policies by (org_id, env_id, route_slug).
type RouteLookupKey struct {
	OrgID     string
	EnvID     string
	RouteSlug string
}

// RouteTargetView is an immutable snapshot record of a route_targets row.
type RouteTargetView struct {
	OrgID         string
	RouteID       string
	DeploymentID  string
	PriorityOrder int
	Weight        int
}

// RoutePolicyView is an immutable snapshot record of a route_policies row with ordered targets.
type RoutePolicyView struct {
	ID              string
	OrgID           string
	ProjectID       string
	EnvID           string
	RouteSlug       string
	Strategy        string
	MaxFailoverHops int
	RequiredRegion  *string
	Version         int64
	CreatedAtMs     int64
	UpdatedAtMs     int64
	Targets         []RouteTargetView
}

// Clone returns a deep copy of RoutePolicyView.
func (r *RoutePolicyView) Clone() *RoutePolicyView {
	if r == nil {
		return nil
	}
	cp := *r
	cp.RequiredRegion = cloneStrPtr(r.RequiredRegion)
	if len(r.Targets) > 0 {
		cp.Targets = append([]RouteTargetView(nil), r.Targets...)
	}
	return &cp
}

// RouteCatalogView bundles O(1) indexes over route_policies and route_targets.
type RouteCatalogView struct {
	byID       map[string]*RoutePolicyView
	byKey      map[RouteLookupKey]*RoutePolicyView
	corruptErr error
}

// RoutesHealthy returns nil if route metadata loaded cleanly without SQL or schema errors.
func (s *ConfigSnapshot) RoutesHealthy() error {
	if s == nil || s.routes == nil {
		return fmt.Errorf("%w: snapshot route catalog missing", ErrRouteConfigCorrupted)
	}
	if s.routes.corruptErr != nil {
		return fmt.Errorf("%w: %v", ErrRouteConfigCorrupted, s.routes.corruptErr)
	}
	return nil
}

// LookupRoutePolicy performs an O(1), tenant-isolated snapshot lookup of a RoutePolicy
// matching (orgID, envID, routeSlug).
func (s *ConfigSnapshot) LookupRoutePolicy(orgID, envID, routeSlug string) (*RoutePolicyView, bool, error) {
	if err := s.RoutesHealthy(); err != nil {
		return nil, false, err
	}
	if orgID == "" || envID == "" || routeSlug == "" {
		return nil, false, nil
	}
	r, ok := s.routes.byKey[RouteLookupKey{
		OrgID:     orgID,
		EnvID:     envID,
		RouteSlug: routeSlug,
	}]
	if !ok {
		return nil, false, nil
	}
	return r.Clone(), true, nil
}

// LookupRoutePolicyByID performs an O(1) tenant-isolated lookup of a RoutePolicy by route ID.
func (s *ConfigSnapshot) LookupRoutePolicyByID(orgID, routeID string) (*RoutePolicyView, bool, error) {
	if err := s.RoutesHealthy(); err != nil {
		return nil, false, err
	}
	if orgID == "" || routeID == "" {
		return nil, false, nil
	}
	r, ok := s.routes.byID[routeID]
	if !ok || r.OrgID != orgID {
		return nil, false, nil
	}
	return r.Clone(), true, nil
}

func loadRoutesSQL(ctx context.Context, tx *sql.Tx) *RouteCatalogView {
	cat := &RouteCatalogView{
		byID:  make(map[string]*RoutePolicyView),
		byKey: make(map[RouteLookupKey]*RoutePolicyView),
	}
	loadRoutesIntoSQL(ctx, tx, cat)
	return cat
}

func loadRoutesIntoSQL(ctx context.Context, tx *sql.Tx, cat *RouteCatalogView) {
	if cat.corruptErr != nil {
		return
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, org_id, project_id, env_id, route_slug, strategy,
		       max_failover_hops, required_region, version, created_at_ms, updated_at_ms
		FROM route_policies
		ORDER BY created_at_ms ASC, id ASC
	`)
	if err != nil {
		cat.corruptErr = fmt.Errorf("query route_policies: %w", err)
		return
	}

	var added []*RoutePolicyView
	for rows.Next() {
		var (
			r      RoutePolicyView
			reqReg sql.NullString
		)
		if scanErr := rows.Scan(
			&r.ID, &r.OrgID, &r.ProjectID, &r.EnvID, &r.RouteSlug, &r.Strategy,
			&r.MaxFailoverHops, &reqReg, &r.Version, &r.CreatedAtMs, &r.UpdatedAtMs,
		); scanErr != nil {
			rows.Close()
			cat.corruptErr = fmt.Errorf("scan route_policy row: %w", scanErr)
			return
		}
		if reqReg.Valid {
			norm := strings.ToUpper(strings.TrimSpace(reqReg.String))
			if norm != "" {
				r.RequiredRegion = &norm
			}
		}
		if r.MaxFailoverHops < 0 || r.MaxFailoverHops > 5 {
			rows.Close()
			cat.corruptErr = fmt.Errorf("route_policy %q invalid max_failover_hops %d", r.ID, r.MaxFailoverHops)
			return
		}
		r.Strategy = strings.ToUpper(strings.TrimSpace(r.Strategy))
		switch r.Strategy {
		case RouteStrategyPriorityFailover,
			RouteStrategyWeightedRR,
			RouteStrategyWeighted,
			RouteStrategyLowestCost,
			RouteStrategyLowestLatency:
		default:
			rows.Close()
			cat.corruptErr = fmt.Errorf("route_policy %q unsupported strategy %q", r.ID, r.Strategy)
			return
		}

		cat.byID[r.ID] = &r
		cat.byKey[RouteLookupKey{
			OrgID:     r.OrgID,
			EnvID:     r.EnvID,
			RouteSlug: r.RouteSlug,
		}] = &r
		added = append(added, &r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		cat.corruptErr = fmt.Errorf("iterate route_policies: %w", err)
		return
	}
	rows.Close()

	tRows, err := tx.QueryContext(ctx, `
		SELECT org_id, route_id, deployment_id, priority_order, weight
		FROM route_targets
		ORDER BY route_id ASC, priority_order ASC, deployment_id ASC
	`)
	if err != nil {
		cat.corruptErr = fmt.Errorf("query route_targets: %w", err)
		return
	}
	defer tRows.Close()

	for tRows.Next() {
		var tgt RouteTargetView
		if scanErr := tRows.Scan(
			&tgt.OrgID, &tgt.RouteID, &tgt.DeploymentID, &tgt.PriorityOrder, &tgt.Weight,
		); scanErr != nil {
			cat.corruptErr = fmt.Errorf("scan route_target row: %w", scanErr)
			return
		}
		if tgt.Weight < 0 {
			cat.corruptErr = fmt.Errorf("route_target (%s,%s) negative weight %d", tgt.RouteID, tgt.DeploymentID, tgt.Weight)
			return
		}
		parent, ok := cat.byID[tgt.RouteID]
		if !ok || parent.OrgID != tgt.OrgID {
			cat.corruptErr = fmt.Errorf("route_target (%s,%s) references unknown or cross-org route %q", tgt.OrgID, tgt.DeploymentID, tgt.RouteID)
			return
		}
		parent.Targets = append(parent.Targets, tgt)
	}
	if err := tRows.Err(); err != nil {
		cat.corruptErr = fmt.Errorf("iterate route_targets: %w", err)
		return
	}

	for _, r := range added {
		if len(r.Targets) > 1 {
			sort.Slice(r.Targets, func(i, j int) bool {
				if r.Targets[i].PriorityOrder != r.Targets[j].PriorityOrder {
					return r.Targets[i].PriorityOrder < r.Targets[j].PriorityOrder
				}
				return r.Targets[i].DeploymentID < r.Targets[j].DeploymentID
			})
		}
	}
}
