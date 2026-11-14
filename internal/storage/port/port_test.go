// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package port

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestPortSecurityInvariantNoByIDOnlyMethods(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	fileAST, err := parser.ParseFile(fset, "port.go", nil, 0)
	if err != nil {
		t.Fatalf("parse port.go: %v", err)
	}

	ast.Inspect(fileAST, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "TenancyRepository" {
			return true
		}
		iface, ok := ts.Type.(*ast.InterfaceType)
		if !ok {
			t.Fatalf("TenancyRepository must be an interface")
		}
		for _, m := range iface.Methods.List {
			if len(m.Names) == 0 {
				continue
			}
			name := m.Names[0].Name
			if name == "CreateOrganization" || name == "GetOrganizationBySlug" || name == "ListOrganizations" {
				continue
			}
			ft, ok := m.Type.(*ast.FuncType)
			if !ok || len(ft.Params.List) < 2 {
				t.Fatalf("method %s must take (ctx, orgID, ...)", name)
			}
			secondParam := ft.Params.List[1]
			if len(secondParam.Names) == 0 || secondParam.Names[0].Name != "orgID" {
				t.Fatalf("SECURITY INVARIANT VIOLATION: TenancyRepository.%s second param must be orgID, got %+v", name, secondParam.Names)
			}
		}
		return false
	})
}

func TestPortErrorSentinelsDistinct(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrNotFound,
		ErrVersionConflict,
		ErrResourceInUse,
		ErrAlreadyExists,
		ErrInvalidInput,
		ErrDirtyMigration,
		ErrNetworkFilesystem,
	}
	for i := range errs {
		if errs[i] == nil {
			t.Fatalf("sentinel %d is nil", i)
		}
		for j := i + 1; j < len(errs); j++ {
			if errors.Is(errs[i], errs[j]) {
				t.Fatalf("duplicate sentinel identity at %d and %d", i, j)
			}
		}
	}
}

func TestDistributedLeaseIsExpired(t *testing.T) {
	t.Parallel()
	var nilLease *DistributedLease
	if !nilLease.IsExpired(1000) {
		t.Fatalf("expected nil lease IsExpired=true")
	}
	lease := &DistributedLease{LeaseName: "l", HolderNodeID: "h", FencingToken: 1, AcquiredAtMs: 1000, ExpiresAtMs: 5000}
	if lease.IsExpired(4999) {
		t.Fatalf("expected not expired at 4999")
	}
	if !lease.IsExpired(5000) || !lease.IsExpired(6000) {
		t.Fatalf("expected expired at >= 5000")
	}
}
