// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/sdk"
)

const (
	manifestSchemaAssetPath = "plugin-schema/v1/extension-manifest.schema.json"
	maxSchemaMemoryMib      = 64
)

var (
	schemaIDRe      = regexp.MustCompile(`^@[a-z0-9][a-z0-9-]{0,62}/[a-z0-9][a-z0-9._-]{0,62}$`)
	schemaSemverRe  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	schemaSHA256Re  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	schemaHostNoWC  = regexp.MustCompile(`^[^*?\s/\\@?#%]+$`)
)

// ValidationReport summarizes structural, schema, security, and runtime verification of an extension.
type ValidationReport struct {
	ExtensionID           string            `json:"extension_id" yaml:"extension_id"`
	Name                  string            `json:"name" yaml:"name"`
	Version               string            `json:"version" yaml:"version"`
	ExecutionTier         string            `json:"execution_tier" yaml:"execution_tier"`
	ExtensionKind         string            `json:"extension_kind" yaml:"extension_kind"`
	RequiresRootSignature bool              `json:"requires_root_signature" yaml:"requires_root_signature"`
	Status                string            `json:"status" yaml:"status"`
	ExitCode              int               `json:"exit_code" yaml:"exit_code"`
	Entrypoint            string            `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	EntrypointSHA256      string            `json:"entrypoint_sha256,omitempty" yaml:"entrypoint_sha256,omitempty"`
	CapabilityBadges      map[string]string `json:"capability_badges" yaml:"capability_badges"`
	SchemaRef             string            `json:"schema_ref" yaml:"schema_ref"`
}

// ValidateExtensionDir validates manifest.yaml against the embedded v1 JSON Schema, Core's
// installer.ParseAndValidateManifest rules, reserved namespace rules, network egress constraints,
// Tier-1 declarative compilation, or Tier-2 Wasm entrypoint verification and sandbox compilation.
func ValidateExtensionDir(ctx context.Context, dir string) (*ValidationReport, *installer.ValidatedManifest, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	absDir, manifestBytes, archiveFiles, err := loadExtensionFilesForValidation(dir)
	if err != nil {
		return nil, nil, nil, err
	}

	rawObj, err := decodeYAMLAsJSONObject(manifestBytes)
	if err != nil {
		return nil, nil, nil, validationErr("malformed manifest.yaml", err)
	}
	if err := validateManifestAgainstEmbeddedSchema(rawObj); err != nil {
		return nil, nil, nil, err
	}

	vm, err := installer.ParseAndValidateManifest(manifestBytes, installer.DefaultValidationPolicy(), archiveFiles)
	if err != nil {
		return nil, nil, nil, classifyInstallerError(err)
	}

	var entrypointSHA string
	var wasmBytes []byte

	switch vm.ExecutionTier {
	case installer.ExecutionTierDeclarativeT1:
		if _, derr := declarative.ParseDeclarativeManifestYAML(manifestBytes); derr != nil {
			if errors.Is(derr, port.ErrProhibitedCookieInjection) {
				return nil, nil, nil, securityErr("declarative manifest violates cookie security invariant", derr)
			}
			return nil, nil, nil, validationErr("declarative manifest compilation failed", derr)
		}
	case installer.ExecutionTierWasmSandboxT2:
		if vm.Manifest.Runtime == nil || strings.TrimSpace(vm.Manifest.Runtime.Entrypoint) == "" {
			return nil, nil, nil, validationErr("sandboxed_wasm requires runtime.entrypoint", nil)
		}
		if vm.Manifest.Runtime.Quotas != nil && vm.Manifest.Runtime.Quotas.MaxMemoryMib > maxSchemaMemoryMib {
			return nil, nil, nil, securityErr(fmt.Sprintf("maxMemoryMib %d exceeds %d MiB hard cap", vm.Manifest.Runtime.Quotas.MaxMemoryMib, maxSchemaMemoryMib), nil)
		}
		relEntry := path.Clean(filepath.ToSlash(strings.TrimSpace(vm.Manifest.Runtime.Entrypoint)))
		entrypointSHA = archiveFiles[relEntry]
		fullEntry := filepath.Join(absDir, filepath.FromSlash(relEntry))
		rawWasm, rerr := os.ReadFile(fullEntry)
		if rerr != nil {
			return nil, nil, nil, validationErr(fmt.Sprintf("read wasm entrypoint %q", relEntry), rerr)
		}
		wasmBytes = rawWasm

		memPages := uint32(wasm.DefaultMemoryLimitPages)
		allowExt := false
		if vm.Manifest.Runtime.Quotas != nil && vm.Manifest.Runtime.Quotas.MaxMemoryMib > 0 {
			reqPages := uint32(vm.Manifest.Runtime.Quotas.MaxMemoryMib) * 16
			if reqPages > wasm.DefaultMemoryLimitPages {
				allowExt = true
			}
			memPages = reqPages
		}
		var slotNames []string
		for _, s := range vm.Manifest.SecretSlots {
			slotNames = append(slotNames, s.Slot)
		}
		rtCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rt, werr := wasm.NewRuntime(rtCtx, wasm.RuntimeConfig{
			WasmBytes:           rawWasm,
			InstallationID:      "validate:" + vm.Manifest.Metadata.ID,
			PoolSize:            1,
			MemoryLimitPages:    memPages,
			AllowExtendedMemory: allowExt,
			AllowedHosts:        vm.Manifest.Permissions.NetworkEgress.AllowedHosts,
			AllowPrivateIP:      vm.Manifest.Permissions.NetworkEgress.AllowPrivateIP,
			AllowInsecureHTTP:   !vm.Manifest.Permissions.NetworkEgress.RequireTLS,
			ApprovedSecretSlots: slotNames,
		})
		cancel()
		if werr != nil {
			if errors.Is(werr, wasm.ErrUnapprovedWASIImport) {
				return nil, nil, nil, securityErr("wasm module attempts forbidden WASI or host import", werr)
			}
			return nil, nil, nil, validationErr("wasm module verification failed", werr)
		}
		_ = rt.Close(context.Background())
	default:
		return nil, nil, nil, validationErr(fmt.Sprintf("unsupported execution tier %q in offline SDK toolchain", vm.ExecutionTier), nil)
	}

	badges := declaredCapabilityBadges(vm.Manifest)
	report := &ValidationReport{
		ExtensionID:           SanitizeTerminalText(vm.Manifest.Metadata.ID),
		Name:                  SanitizeTerminalText(vm.Manifest.Metadata.Name),
		Version:               SanitizeTerminalText(vm.Manifest.Metadata.Version),
		ExecutionTier:         vm.ExecutionTier,
		ExtensionKind:         vm.ExtensionKind,
		RequiresRootSignature: vm.RequiresRootSignature,
		Status:                "VALID",
		ExitCode:              ExitOK,
		Entrypoint:            entrypointName(vm.Manifest),
		EntrypointSHA256:      entrypointSHA,
		CapabilityBadges:      badges,
		SchemaRef:             manifestSchemaAssetPath,
	}
	return report, vm, wasmBytes, nil
}

func loadExtensionFilesForValidation(dir string) (string, []byte, map[string]string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", nil, nil, validationErr(fmt.Sprintf("extension directory %q not found", dir), err)
	}
	if !fi.IsDir() {
		return "", nil, nil, validationErr(fmt.Sprintf("%q is not a directory", dir), nil)
	}
	absDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", nil, nil, validationErr("resolve extension directory", err)
	}

	manifestPath := filepath.Join(absDir, "manifest.yaml")
	mInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return "", nil, nil, validationErr("manifest.yaml missing in extension directory", err)
	}
	if mInfo.Mode()&os.ModeSymlink != 0 || !mInfo.Mode().IsRegular() {
		return "", nil, nil, securityErr("manifest.yaml must be a regular file (symlinks prohibited)", nil)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", nil, nil, validationErr("read manifest.yaml", err)
	}

	files := make(map[string]string)
	walkErr := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, wErr error) error {
		if wErr != nil {
			return wErr
		}
		if path == absDir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return securityErr(fmt.Sprintf("symlink %q prohibited inside extension package", path), nil)
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return securityErr(fmt.Sprintf("non-regular file %q prohibited inside extension package", path), nil)
		}
		rel, rerr := filepath.Rel(absDir, path)
		if rerr != nil {
			return rerr
		}
		relSlash := filepath.ToSlash(rel)
		if strings.HasSuffix(strings.ToLower(relSlash), ".tpkg") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(raw)
		files[relSlash] = hex.EncodeToString(sum[:])
		return nil
	})
	if walkErr != nil {
		var ex *ExitError
		if errors.As(walkErr, &ex) {
			return "", nil, nil, walkErr
		}
		return "", nil, nil, validationErr("walk extension files", walkErr)
	}
	return absDir, manifestBytes, files, nil
}

func classifyInstallerError(err error) error {
	switch {
	case errors.Is(err, installer.ErrEntrypointMismatch):
		return contractErr("runtime entrypoint missing or sha256 digest mismatch", err)
	case errors.Is(err, installer.ErrProhibitedCookiePermission),
		errors.Is(err, installer.ErrProhibitedCapabilityTier),
		errors.Is(err, port.ErrProhibitedCookieInjection),
		errors.Is(err, installer.ErrWildcardEgressForbidden):
		return securityErr("manifest security/permission policy violation", err)
	default:
		return validationErr("manifest validation failed", err)
	}
}

func entrypointName(m installer.ExtensionManifest) string {
	if m.Runtime == nil {
		return ""
	}
	return m.Runtime.Entrypoint
}

func declaredCapabilityBadges(m installer.ExtensionManifest) map[string]string {
	badges := make(map[string]string)
	for _, capItem := range m.Capabilities {
		switch capItem.Type {
		case "provider_adapter":
			badges["chat_completion"] = "DECLARED"
			badges["chat_stream"] = "DECLARED"
			badges["tool_calling"] = "DECLARED"
			badges["error_normalization"] = "DECLARED"
		case "guardrail_policy":
			badges["guardrail"] = "DECLARED"
		case "auth_driver":
			badges["validation_probe"] = "DECLARED"
		}
	}
	return badges
}

func decodeYAMLAsJSONObject(raw []byte) (map[string]any, error) {
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	norm, err := normalizeYAMLValue(parsed)
	if err != nil {
		return nil, err
	}
	obj, ok := norm.(map[string]any)
	if !ok {
		return nil, errors.New("root YAML value must be a mapping/object")
	}
	return obj, nil
}

func normalizeYAMLValue(v any) (any, error) {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			nv, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string YAML map key %v", k)
			}
			nv, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			out[ks] = nv
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			nv, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}

// validateManifestAgainstEmbeddedSchema enforces the embedded extension-manifest.schema.json contract.
func validateManifestAgainstEmbeddedSchema(doc map[string]any) error {
	schemaBytes, err := sdk.EmbeddedAssets.ReadFile(manifestSchemaAssetPath)
	if err != nil {
		return validationErr("load embedded manifest JSON schema", err)
	}
	var schemaObj map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaObj); err != nil {
		return validationErr("decode embedded manifest JSON schema", err)
	}
	props, _ := schemaObj["properties"].(map[string]any)
	if reqRaw, ok := schemaObj["required"].([]any); ok {
		for _, r := range reqRaw {
			key, _ := r.(string)
			if _, present := doc[key]; !present {
				return validationErr(fmt.Sprintf("JSON schema violation: missing required property %q", key), nil)
			}
		}
	}
	for k := range doc {
		if _, allowed := props[k]; !allowed {
			return validationErr(fmt.Sprintf("JSON schema violation: unexpected property %q", k), nil)
		}
	}

	if apiVer, _ := doc["apiVersion"].(string); apiVer != installer.CanonicalAPIVersion {
		return validationErr(fmt.Sprintf("JSON schema violation: apiVersion must equal %q", installer.CanonicalAPIVersion), nil)
	}
	if kind, _ := doc["kind"].(string); kind != installer.KindExtensionManifest && kind != installer.KindDeclarativeAdapter {
		return validationErr("JSON schema violation: unsupported kind", nil)
	}

	meta, ok := doc["metadata"].(map[string]any)
	if !ok {
		return validationErr("JSON schema violation: metadata must be an object", nil)
	}
	id, _ := meta["id"].(string)
	ver, _ := meta["version"].(string)
	lic, _ := meta["license"].(string)
	if !schemaIDRe.MatchString(id) {
		return validationErr(fmt.Sprintf("JSON schema violation: metadata.id %q must be scoped @scope/name", id), nil)
	}
	if !schemaSemverRe.MatchString(ver) {
		return validationErr(fmt.Sprintf("JSON schema violation: metadata.version %q must be valid SemVer", ver), nil)
	}
	if strings.TrimSpace(lic) == "" {
		return validationErr("JSON schema violation: metadata.license is required", nil)
	}

	if ott, hasOTT := meta["originTrustTier"]; hasOTT {
		ottStr, _ := ott.(string)
		switch ottStr {
		case "official", "verified", "unverified_external":
		default:
			return validationErr(fmt.Sprintf("JSON schema violation: invalid metadata.originTrustTier %q", ottStr), nil)
		}
	}

	compat, ok := doc["compatibility"].(map[string]any)
	if !ok {
		return validationErr("JSON schema violation: compatibility must be an object", nil)
	}
	cv, hasCV := compat["contractVersion"].(string)
	if !hasCV || strings.TrimSpace(cv) == "" {
		return validationErr("JSON schema violation: compatibility.contractVersion is required", nil)
	}
	if cv != installer.ActiveContractVersion {
		return validationErr(fmt.Sprintf("JSON schema violation: unsupported compatibility.contractVersion %q", cv), nil)
	}
	if cr, _ := compat["coreVersionRange"].(string); strings.TrimSpace(cr) == "" {
		return validationErr("JSON schema violation: compatibility.coreVersionRange is required", nil)
	}

	if rtRaw, exists := doc["runtime"]; exists {
		rt, ok := rtRaw.(map[string]any)
		if !ok {
			return validationErr("JSON schema violation: runtime must be an object", nil)
		}
		if sha, ok := rt["sha256"].(string); ok && sha != "" && !schemaSHA256Re.MatchString(sha) {
			return validationErr("JSON schema violation: runtime.sha256 must be 64 lowercase hex characters", nil)
		}
		if qRaw, hasQ := rt["quotas"]; hasQ {
			qMap, ok := qRaw.(map[string]any)
			if !ok {
				return validationErr("JSON schema violation: runtime.quotas must be an object", nil)
			}
			for _, qField := range []string{"maxMemoryMib", "maxCpuMsPerCall", "maxStreamChunkCpuMs"} {
				if valRaw, present := qMap[qField]; present {
					v, ok := yamlInt(valRaw)
					if !ok || v < 1 {
						return validationErr(fmt.Sprintf("JSON schema violation: runtime.quotas.%s must be integer >= 1", qField), nil)
					}
					if qField == "maxMemoryMib" && v > maxSchemaMemoryMib {
						return securityErr(fmt.Sprintf("maxMemoryMib %d exceeds %d MiB hard cap", v, maxSchemaMemoryMib), nil)
					}
				}
			}
		}
	}

	if capsRaw, hasCaps := doc["capabilities"]; hasCaps {
		caps, ok := capsRaw.([]any)
		if !ok {
			return validationErr("JSON schema violation: capabilities must be an array", nil)
		}
		for i, cRaw := range caps {
			cMap, ok := cRaw.(map[string]any)
			if !ok {
				return validationErr(fmt.Sprintf("JSON schema violation: capabilities[%d] must be an object", i), nil)
			}
			if amRaw, hasAM := cMap["authMethods"]; hasAM {
				ams, ok := amRaw.([]any)
				if !ok {
					return validationErr(fmt.Sprintf("JSON schema violation: capabilities[%d].authMethods must be an array", i), nil)
				}
				for j, item := range ams {
					m, ok := item.(map[string]any)
					if !ok {
						return validationErr(fmt.Sprintf("JSON schema violation: capabilities[%d].authMethods[%d] must be an object", i, j), nil)
					}
					idStr, _ := m["id"].(string)
					modeStr, _ := m["mode"].(string)
					if strings.TrimSpace(idStr) == "" || strings.TrimSpace(modeStr) == "" {
						return validationErr(fmt.Sprintf("JSON schema violation: capabilities[%d].authMethods[%d] requires non-empty id and mode", i, j), nil)
					}
				}
			}
		}
	}

	perms, ok := doc["permissions"].(map[string]any)
	if !ok {
		return validationErr("JSON schema violation: permissions must be an object", nil)
	}
	ne, ok := perms["networkEgress"].(map[string]any)
	if !ok {
		return validationErr("JSON schema violation: permissions.networkEgress is required", nil)
	}
	if hosts, ok := ne["allowedHosts"].([]any); ok {
		for _, h := range hosts {
			hs, _ := h.(string)
			if !schemaHostNoWC.MatchString(hs) {
				return securityErr(fmt.Sprintf("wildcard or malformed allowedHosts pattern %q violates zero-wildcard policy", hs), nil)
			}
		}
	}
	return nil
}

func yamlInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}
