// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
)

type nativeTelemetryCtxKey struct{}

type nativeStatusCaptureWriter struct {
	http.ResponseWriter
	status  int
	wrote   bool
	errCode string
}

func (w *nativeStatusCaptureWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *nativeStatusCaptureWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *nativeStatusCaptureWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *nativeStatusCaptureWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(p)
}

func (w *nativeStatusCaptureWriter) SetCanonicalErrorCode(code string) {
	if strings.TrimSpace(code) != "" {
		w.errCode = strings.TrimSpace(code)
	}
}

type nativeRequestTelemetry struct {
	pipeline      *otel.Pipeline
	reqID         string
	traceRecordID string
	traceID       string
	entryPoint    string
	startedAt     time.Time
	startedAtMs   int64
	rootSpan      trace.Span

	mu               sync.Mutex
	orgID            string
	projectID        string
	envID            string
	vkeyID           string
	routePattern     string
	validatedModel   string
	adapterID        string
	extTier          string
	deploymentID     string
	connectionID     string
	installID        string
	versionID        string
	dataRegion       string
	failoverAttempts int
	errCode          string
	ttftMs           *int64
	upstreamNs       int64
	stages           []otel.WaterfallStage
	debugBody        []byte
}

func withNativeTelemetry(ctx context.Context, rt *nativeRequestTelemetry) context.Context {
	if rt == nil {
		return ctx
	}
	return context.WithValue(ctx, nativeTelemetryCtxKey{}, rt)
}

func nativeTelemetryFromContext(ctx context.Context) *nativeRequestTelemetry {
	if ctx == nil {
		return nil
	}
	rt, _ := ctx.Value(nativeTelemetryCtxKey{}).(*nativeRequestTelemetry)
	return rt
}

func (rt *nativeRequestTelemetry) recordAuth(match snapshot.KeyMatchResult) {
	if rt == nil || match.Key == nil {
		return
	}
	rt.mu.Lock()
	rt.orgID = match.Key.OrgID
	rt.projectID = match.Key.ProjectID
	rt.envID = match.Key.EnvID
	rt.vkeyID = match.Key.ID
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) setRoutePattern(pattern string) {
	if rt == nil || strings.TrimSpace(pattern) == "" {
		return
	}
	rt.mu.Lock()
	rt.routePattern = strings.TrimSpace(pattern)
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) recordValidatedModel(modelSlug string) {
	if rt == nil || strings.TrimSpace(modelSlug) == "" {
		return
	}
	rt.mu.Lock()
	rt.validatedModel = strings.TrimSpace(modelSlug)
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) recordRouteAttempt(target *snapshot.ResolvedTarget, validatedModel string) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.failoverAttempts++
	if strings.TrimSpace(validatedModel) != "" {
		rt.validatedModel = strings.TrimSpace(validatedModel)
	}
	if target != nil {
		if target.Deployment != nil {
			rt.deploymentID = target.Deployment.ID
			rt.dataRegion = target.Deployment.DataRegion
		}
		if target.Connection != nil {
			rt.connectionID = target.Connection.ID
		}
		if target.Adapter != nil {
			rt.adapterID = target.Adapter.ID
			if rt.adapterID == "" {
				rt.adapterID = target.Adapter.AdapterSlug
			}
			rt.extTier = target.Adapter.ExecutionTier
			rt.installID = target.Adapter.InstallationID
			rt.versionID = target.Adapter.ActiveVersionID
		}
	}
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) addUpstreamDuration(d time.Duration) {
	if rt == nil || d <= 0 {
		return
	}
	rt.mu.Lock()
	rt.upstreamNs += d.Nanoseconds()
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) setTTFTMs(ms int64) {
	if rt == nil || ms < 0 {
		return
	}
	rt.mu.Lock()
	if rt.ttftMs == nil {
		v := ms
		rt.ttftMs = &v
	}
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) captureDebugPayloadIfAuthorized(raw []byte) {
	if rt == nil || rt.pipeline == nil || len(raw) == 0 {
		return
	}
	rt.mu.Lock()
	orgID := rt.orgID
	projectID := rt.projectID
	vkeyID := rt.vkeyID
	already := len(rt.debugBody) > 0
	rt.mu.Unlock()
	if already || orgID == "" || projectID == "" {
		return
	}
	if !rt.pipeline.HasActiveDebugSession(orgID, projectID, vkeyID) {
		return
	}
	// Scrub and bound immediately at capture time (<= 256 KiB) so long-lived streams never pin 32 MiB buffers.
	maxPT := otel.MaxDebugBlobCiphertextBytes - 16
	scrubbed := rt.pipeline.Redactor.SanitizeDebugPayload(raw, maxPT)
	if len(scrubbed) == 0 {
		return
	}
	rt.mu.Lock()
	if len(rt.debugBody) == 0 {
		rt.debugBody = scrubbed
	} else {
		clear(scrubbed)
	}
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) startStage(ctx context.Context, stageName string, attrs ...attribute.KeyValue) (context.Context, func(status, detail string, err error)) {
	if rt == nil || rt.pipeline == nil {
		return ctx, func(string, string, error) {}
	}
	stageStart := time.Now()
	sCtx, span := rt.pipeline.StartStageSpan(ctx, stageName, attrs...)
	return sCtx, func(status, detail string, err error) {
		dur := time.Since(stageStart)
		rt.pipeline.EndSpan(span, err)
		offsetUs := stageStart.Sub(rt.startedAt).Microseconds()
		if offsetUs < 0 {
			offsetUs = 0
		}
		durUs := dur.Microseconds()
		if durUs < 0 {
			durUs = 0
		}
		rt.mu.Lock()
		rt.stages = append(rt.stages, otel.WaterfallStage{
			Stage:      stageName,
			OffsetUs:   offsetUs,
			DurationUs: durUs,
			Status:     status,
			Detail:     detail,
		})
		rt.mu.Unlock()
	}
}

func inspectNativeWithTelemetry(
	ctx context.Context,
	reqTel *nativeRequestTelemetry,
	target *snapshot.ResolvedTarget,
	rt port.ProviderRuntime,
	reqID, dialect, upstreamPath string,
	headers map[string]string,
	rawBody []byte,
	hints RawBodyHints,
) (InspectResult, error) {
	adpID, _, extTier := nativeTargetTelemetryLabels(target)
	inspCtx, endInspHook := reqTel.startStage(ctx, "ExtensionHook",
		attribute.String("hook", "inspect_native_request"),
		attribute.String("adapter_id", adpID),
		attribute.String("extension_tier", extTier),
	)
	res, err := InspectNativeRequest(inspCtx, rt, reqID, dialect, upstreamPath, headers, rawBody, hints)
	st := "ok"
	if err != nil {
		st = "error"
	}
	endInspHook(st, "inspect_native_request", err)
	return res, err
}

func (rt *nativeRequestTelemetry) finish(ctx context.Context, scw *nativeStatusCaptureWriter, r *http.Request) {
	if rt == nil || rt.pipeline == nil {
		return
	}
	totalDur := time.Since(rt.startedAt)
	status := http.StatusOK
	errCode := ""
	wrote := false
	if scw != nil {
		wrote = scw.wrote
		if scw.status != 0 {
			status = scw.status
		}
		errCode = scw.errCode
	}

	rt.mu.Lock()
	if errCode == "" {
		errCode = rt.errCode
	}
	if !wrote && r != nil && r.Context().Err() != nil {
		status = 499
		if errCode == "" {
			errCode = "client_canceled"
		}
	}
	orgID := rt.orgID
	projectID := rt.projectID
	envID := rt.envID
	vkeyID := rt.vkeyID
	routePattern := rt.routePattern
	validatedModel := rt.validatedModel
	extTier := rt.extTier
	deploymentID := rt.deploymentID
	connectionID := rt.connectionID
	installID := rt.installID
	versionID := rt.versionID
	failoverAttempts := rt.failoverAttempts
	ttftMs := rt.ttftMs
	upstreamNs := rt.upstreamNs
	stages := append([]otel.WaterfallStage(nil), rt.stages...)
	debugBody := rt.debugBody
	rt.debugBody = nil
	rt.mu.Unlock()
	defer clear(debugBody)

	metricModel := validatedModel
	if metricModel == "" {
		metricModel = "unknown"
	}
	routeSlug := validatedModel
	if routeSlug == "" {
		if routePattern != "" {
			routeSlug = routePattern
		} else {
			routeSlug = "/native/{dialect}"
		}
	}
	traceRecordID := rt.traceRecordID
	if traceRecordID == "" {
		traceRecordID = idgen.MustNew(idgen.PrefixTrace)
	}

	overheadNs := totalDur.Nanoseconds() - upstreamNs
	if overheadNs < 0 {
		overheadNs = 0
	}
	proxyOverhead := time.Duration(overheadNs)

	if rt.pipeline.Metrics != nil {
		rt.pipeline.Metrics.RecordHTTPRequest(rt.entryPoint, orgID, projectID, metricModel, status, errCode)
		rt.pipeline.Metrics.RecordProxyOverhead(rt.entryPoint, extTier, proxyOverhead)
	}

	var rootErr error
	if status >= 400 {
		rootErr = &nativeOtelErrorMsg{code: errCode, status: status}
	}
	rt.pipeline.EndSpan(rt.rootSpan, rootErr)

	level := "INFO"
	if status >= 500 {
		level = "ERROR"
	} else if status >= 400 {
		level = "WARN"
	}
	_ = rt.pipeline.Logger.Log(privacy.LogRecord{
		Level:      level,
		Event:      "gateway_request",
		EntryPoint: rt.entryPoint,
		RequestID:  rt.reqID,
		TraceID:    rt.traceID,
		Fields: map[string]any{
			"org_id":            orgID,
			"project_id":        projectID,
			"env_id":            envID,
			"virtual_key_id":    vkeyID,
			"route_slug":        routeSlug,
			"deployment_id":     deploymentID,
			"failover_attempts": failoverAttempts,
			"http_status":       status,
			"error_code":        errCode,
			"proxy_overhead_us": proxyOverhead.Microseconds(),
			"total_duration_ms": totalDur.Milliseconds(),
		},
	})

	if orgID != "" {
		rt.pipeline.EnqueueRequestTrace(otel.RequestTraceRecord{
			RequestID:               traceRecordID,
			StartedAtMs:             rt.startedAtMs,
			OrgID:                   orgID,
			ProjectID:               projectID,
			EnvID:                   envID,
			VirtualKeyID:            vkeyID,
			EntryPoint:              rt.entryPoint,
			OtelTraceID:             rt.traceID,
			RouteSlug:               routeSlug,
			SelectedDeploymentID:    deploymentID,
			ConnectionID:            connectionID,
			ExtensionInstallationID: installID,
			ExtensionVersionID:      versionID,
			FailoverAttempts:        failoverAttempts,
			HTTPStatus:              status,
			CanonicalErrorCode:      errCode,
			ProxyOverheadUs:         proxyOverhead.Microseconds(),
			TTFTMs:                  ttftMs,
			TotalDurationMs:         totalDur.Milliseconds(),
			WaterfallStages:         stages,
		})
		if len(debugBody) > 0 {
			rt.pipeline.EnqueuePreScrubbedDebugBlob(orgID, projectID, vkeyID, traceRecordID, debugBody)
		}
	}
}

type nativeOtelErrorMsg struct {
	code   string
	status int
}

func (e *nativeOtelErrorMsg) Error() string {
	if e.code != "" {
		return e.code
	}
	return http.StatusText(e.status)
}

func nativeTargetTelemetryLabels(target *snapshot.ResolvedTarget) (adapterID, dataRegion, extTier string) {
	if target == nil {
		return "unknown", "UNSPECIFIED", "declarative"
	}
	if target.Adapter != nil {
		adapterID = target.Adapter.ID
		if adapterID == "" {
			adapterID = target.Adapter.AdapterSlug
		}
		extTier = target.Adapter.ExecutionTier
	}
	if target.Deployment != nil && strings.TrimSpace(target.Deployment.DataRegion) != "" {
		dataRegion = target.Deployment.DataRegion
	} else {
		dataRegion = "UNSPECIFIED"
	}
	if adapterID == "" {
		adapterID = "unknown"
	}
	return adapterID, dataRegion, extTier
}

func (h *Handler) recordNativeUsageTelemetry(ctx context.Context, logicalModel string, usage port.NormalizedUsage) {
	if h == nil || h.telemetry == nil || h.telemetry.Metrics == nil {
		return
	}
	var orgID, projectID string
	if rt := nativeTelemetryFromContext(ctx); rt != nil {
		rt.mu.Lock()
		orgID = rt.orgID
		projectID = rt.projectID
		rt.mu.Unlock()
	}
	h.telemetry.Metrics.RecordUsageSettlement(orgID, projectID, logicalModel, formatUsageAccuracyHeader(usage.Accuracy))
}
