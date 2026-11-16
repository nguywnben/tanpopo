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
	pipeline    *otel.Pipeline
	reqID       string
	traceID     string
	entryPoint  string
	startedAt   time.Time
	startedAtMs int64
	rootSpan    trace.Span

	mu               sync.Mutex
	orgID            string
	projectID        string
	envID            string
	vkeyID           string
	routeSlug        string
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

func (rt *nativeRequestTelemetry) recordModel(modelSlug string) {
	if rt == nil || strings.TrimSpace(modelSlug) == "" {
		return
	}
	rt.mu.Lock()
	rt.routeSlug = strings.TrimSpace(modelSlug)
	rt.mu.Unlock()
}

func (rt *nativeRequestTelemetry) recordRouteAttempt(target *snapshot.ResolvedTarget, routeSlug string) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.failoverAttempts++
	if strings.TrimSpace(routeSlug) != "" {
		rt.routeSlug = strings.TrimSpace(routeSlug)
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
	cp := make([]byte, len(raw))
	copy(cp, raw)
	rt.mu.Lock()
	if len(rt.debugBody) == 0 {
		rt.debugBody = cp
	} else {
		clear(cp)
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

func (rt *nativeRequestTelemetry) finish(ctx context.Context, scw *nativeStatusCaptureWriter, r *http.Request) {
	if rt == nil || rt.pipeline == nil {
		return
	}
	totalDur := time.Since(rt.startedAt)
	status := http.StatusOK
	errCode := ""
	if scw != nil {
		if scw.status != 0 {
			status = scw.status
		}
		errCode = scw.errCode
	}

	rt.mu.Lock()
	if errCode == "" {
		errCode = rt.errCode
	}
	orgID := rt.orgID
	projectID := rt.projectID
	envID := rt.envID
	vkeyID := rt.vkeyID
	routeSlug := rt.routeSlug
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

	if routeSlug == "" && r != nil && r.URL != nil {
		routeSlug = r.URL.Path
	}

	overheadNs := totalDur.Nanoseconds() - upstreamNs
	if overheadNs < 0 {
		overheadNs = 0
	}
	proxyOverhead := time.Duration(overheadNs)

	if rt.pipeline.Metrics != nil {
		rt.pipeline.Metrics.RecordHTTPRequest(rt.entryPoint, orgID, projectID, routeSlug, status, errCode)
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

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if orgID != "" {
		_ = rt.pipeline.RecordRequestTrace(persistCtx, otel.RequestTraceRecord{
			RequestID:               rt.reqID,
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
			_, _ = rt.pipeline.StoreDebugBlobIfAuthorized(persistCtx, orgID, projectID, vkeyID, rt.reqID, debugBody)
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
