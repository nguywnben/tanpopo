// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package upgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHelmChartSecurityAndHARequirements(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "deploy", "helm", "tanpopo")

	// 1. Validate Chart.yaml metadata.
	chartBytes, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	var chartMeta struct {
		APIVersion string `yaml:"apiVersion"`
		Name       string `yaml:"name"`
		Version    string `yaml:"version"`
	}
	if err := yaml.Unmarshal(chartBytes, &chartMeta); err != nil {
		t.Fatalf("parse Chart.yaml: %v", err)
	}
	if chartMeta.APIVersion != "v2" || chartMeta.Name != "tanpopo" || chartMeta.Version == "" {
		t.Fatalf("unexpected Chart.yaml metadata: %+v", chartMeta)
	}

	// 2. Validate hardened default values.yaml (NEG-1 & OPS-AC-06).
	valuesBytes, err := os.ReadFile(filepath.Join(chartDir, "values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	var values struct {
		PodSecurityContext struct {
			RunAsNonRoot bool  `yaml:"runAsNonRoot"`
			RunAsUser    int64 `yaml:"runAsUser"`
			RunAsGroup   int64 `yaml:"runAsGroup"`
		} `yaml:"podSecurityContext"`
		SecurityContext struct {
			AllowPrivilegeEscalation bool `yaml:"allowPrivilegeEscalation"`
			ReadOnlyRootFilesystem   bool `yaml:"readOnlyRootFilesystem"`
			Capabilities             struct {
				Drop []string `yaml:"drop"`
			} `yaml:"capabilities"`
		} `yaml:"securityContext"`
		ControlPlane struct {
			ReplicaCount int `yaml:"replicaCount"`
			PDB          struct {
				Enabled      bool `yaml:"enabled"`
				MinAvailable int  `yaml:"minAvailable"`
			} `yaml:"pdb"`
		} `yaml:"controlPlane"`
		DataPlane struct {
			ReplicaCount                  int    `yaml:"replicaCount"`
			TerminationGracePeriodSeconds int    `yaml:"terminationGracePeriodSeconds"`
			DrainTimeout                  string `yaml:"drainTimeout"`
			PreStopSleepSeconds           int    `yaml:"preStopSleepSeconds"`
			PDB                           struct {
				Enabled      bool `yaml:"enabled"`
				MinAvailable int  `yaml:"minAvailable"`
			} `yaml:"pdb"`
			Autoscaling struct {
				Enabled     bool `yaml:"enabled"`
				MinReplicas int  `yaml:"minReplicas"`
				MaxReplicas int  `yaml:"maxReplicas"`
			} `yaml:"autoscaling"`
		} `yaml:"dataPlane"`
		NetworkPolicy struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"networkPolicy"`
		ServiceMonitor struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"serviceMonitor"`
	}
	if err := yaml.Unmarshal(valuesBytes, &values); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}
	if !values.PodSecurityContext.RunAsNonRoot || values.PodSecurityContext.RunAsUser <= 0 || values.PodSecurityContext.RunAsGroup <= 0 {
		t.Fatalf("NEG-1 violation: podSecurityContext must enforce runAsNonRoot + non-zero UID/GID, got %+v", values.PodSecurityContext)
	}
	if values.SecurityContext.AllowPrivilegeEscalation || !values.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatalf("NEG-1 violation: securityContext must enforce readOnlyRootFilesystem=true and allowPrivilegeEscalation=false, got %+v", values.SecurityContext)
	}
	hasDropAll := false
	for _, capName := range values.SecurityContext.Capabilities.Drop {
		if capName == "ALL" {
			hasDropAll = true
		}
	}
	if !hasDropAll {
		t.Fatalf("NEG-1 violation: capabilities.drop must include ALL, got %+v", values.SecurityContext.Capabilities.Drop)
	}
	if values.ControlPlane.ReplicaCount < 2 || !values.ControlPlane.PDB.Enabled || values.ControlPlane.PDB.MinAvailable < 1 {
		t.Fatalf("HA control-plane violation: %+v", values.ControlPlane)
	}
	if values.DataPlane.ReplicaCount < 3 || values.DataPlane.TerminationGracePeriodSeconds < 300 || values.DataPlane.PreStopSleepSeconds < 1 || !values.DataPlane.PDB.Enabled || values.DataPlane.PDB.MinAvailable < 2 || !values.DataPlane.Autoscaling.Enabled || values.DataPlane.Autoscaling.MinReplicas < 3 {
		t.Fatalf("HA/OPS-AC-06 data-plane violation: %+v", values.DataPlane)
	}
	if !values.NetworkPolicy.Enabled || !values.ServiceMonitor.Enabled {
		t.Fatalf("expected networkPolicy and serviceMonitor enabled by default")
	}

	// 3. Verify all required HA & security templates exist and declare expected probes/hooks.
	requiredTemplates := map[string][]string{
		"_helpers.tpl":               {"tanpopo.labels", "tanpopo.serviceAccountName"},
		"serviceaccount.yaml":        {"kind: ServiceAccount", "automountServiceAccountToken: false"},
		"configmap.yaml":             {"kind: ConfigMap", "TANPOPO_STORAGE_ENGINE", "TANPOPO_VALKEY_ADDRS"},
		"deployment-controlplane.yaml": {"kind: Deployment", "maxUnavailable: 0", "path: /readyz", "path: /healthz", "automountServiceAccountToken: false"},
		"deployment-dataplane.yaml":  {"kind: Deployment", "maxUnavailable: 0", "terminationGracePeriodSeconds:", "preStop:", "path: /readyz", "path: /healthz"},
		"service-controlplane.yaml":  {"kind: Service"},
		"service-dataplane.yaml":     {"kind: Service"},
		"hpa-dataplane.yaml":         {"kind: HorizontalPodAutoscaler"},
		"pdb-controlplane.yaml":      {"kind: PodDisruptionBudget"},
		"pdb-dataplane.yaml":         {"kind: PodDisruptionBudget"},
		"networkpolicy.yaml":         {"kind: NetworkPolicy", "- Ingress", "- Egress"},
		"servicemonitor.yaml":        {"kind: ServiceMonitor", "path: /metrics"},
	}
	for name, markers := range requiredTemplates {
		p := filepath.Join(chartDir, "templates", name)
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("missing required template %s: %v", name, rerr)
		}
		text := string(raw)
		for _, m := range markers {
			if !strings.Contains(text, m) {
				t.Errorf("template %s missing required directive %q", name, m)
			}
		}
	}
}
