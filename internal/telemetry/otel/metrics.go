// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package otel implements Tanpopo's bounded-cardinality OpenTelemetry/Prometheus
// RED/USE metrics registry and non-blocking 32 MiB Drop-Oldest OTLP trace pipeline
// (SPEC-CORE-02 Sections 3.1-3.3 / OPS-AC-12 / TP-AC-F0804 / TP-IMP-0022).
package otel

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
)

// ErrDisallowedMetricLabel is returned when a metric registration or attribute set
// includes any label outside Tanpopo's strict bounded-cardinality allowlist (NEG-1).
var ErrDisallowedMetricLabel = errors.New("telemetry: metric label violates bounded cardinality allowlist")

// Canonical metric names from SPEC-CORE-02 Section 3.1 (GAP-13).
const (
	MetricHTTPRequestsTotal         = "tanpopo_http_requests_total"
	MetricProxyOverheadSeconds      = "tanpopo_proxy_overhead_seconds"
	MetricSSEChunkRelaySeconds      = "tanpopo_sse_chunk_relay_seconds"
	MetricUpstreamDurationSeconds   = "tanpopo_upstream_call_duration_seconds"
	MetricDeploymentCircuitState    = "tanpopo_deployment_circuit_state"
	MetricSandboxTrapsTotal         = "tanpopo_sandbox_traps_total"
	MetricActiveStreams             = "tanpopo_active_streams"
	MetricConfigSnapshotAgeSeconds  = "tanpopo_config_snapshot_age_seconds"
	MetricTelemetryDroppedTotal     = "tanpopo_telemetry_dropped_total"
	MetricUsageSettlementsTotal     = "tanpopo_usage_settlements_total"
)

// Strictly banned high-cardinality / sensitive labels (SPEC-CORE-02 Section 3.1 & NEG-1).
var bannedLabels = map[string]bool{
	"request_id":     true,
	"virtual_key_id": true,
	"principal_id":   true,
	"user_id":        true,
	"prompt_hash":    true,
	"email":          true,
	"url":            true,
	"raw_url":        true,
	"path":           true,
	"raw_path":       true,
	"error_message":  true,
	"message":        true,
	"prompt":         true,
	"completion":     true,
}

// AllowedMetricLabels defines the exclusive allowlist of bounded metric label names.
var AllowedMetricLabels = map[string]bool{
	// Tenant & domain allowlist (SPEC-CORE-02 Section 3.1)
	"org_id":        true,
	"project_id":    true,
	"adapter_id":    true,
	"logical_model": true,
	"status_class":  true,
	"error_code":    true,
	"accuracy_tier": true,
	// Structural bounded enums
	"plane":          true,
	"extension_tier": true,
	"data_region":    true,
	"phase":          true,
	"state":          true,
	"trap_reason":    true,
	"sync_channel":   true,
	"signal":         true,
}

// ValidateMetricLabels enforces the strict bounded-cardinality label allowlist (NEG-1).
func ValidateMetricLabels(labels ...string) error {
	for _, raw := range labels {
		k := strings.ToLower(strings.TrimSpace(raw))
		if k == "" || bannedLabels[k] || !AllowedMetricLabels[k] {
			return fmt.Errorf("%w: %q", ErrDisallowedMetricLabel, raw)
		}
	}
	return nil
}

// RegisterCustomCounter validates labels strictly against AllowedMetricLabels before
// creating a counter on MetricsRegistry, failing immediately on high-cardinality keys (NEG-1).
func (m *MetricsRegistry) RegisterCustomCounter(name, description string, labels ...string) (metric.Int64Counter, error) {
	if m == nil || m.meter == nil {
		return nil, fmt.Errorf("telemetry: metrics registry is nil")
	}
	if err := ValidateMetricLabels(labels...); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("telemetry: metric name is required")
	}
	return m.meter.Int64Counter(name, metric.WithDescription(description))
}

// MetricsConfig configures the bounded-cardinality Prometheus & OTel MetricsRegistry.
type MetricsConfig struct {
	// IncludeTenantLabels toggles org_id and project_id labels on Prometheus /metrics
	// (defaults to true; set false on high-tenant clusters per OPS-AC-12).
	IncludeTenantLabels *bool
	// ScrapeBearerToken optionally guards GET /metrics with a constant-time bearer token.
	ScrapeBearerToken string
	// AllowUnauthenticatedScrape explicitly opts in to unauthenticated /metrics scraping
	// when ScrapeBearerToken is empty (disabled by default so ServeHTTP fails closed).
	AllowUnauthenticatedScrape bool
	// Redactor scrubs any stray tokens from metric label values.
	Redactor *privacy.Redactor
}

// MetricsRegistry exposes Tanpopo's canonical RED & USE metrics plus an isolated
// Prometheus /metrics HTTP exposition handler.
type MetricsRegistry struct {
	includeTenantLabels bool
	scrapeToken         string
	allowUnauth         bool
	redactor            *privacy.Redactor

	promReg       *prometheus.Registry
	promExporter  *otelprom.Exporter
	meterProvider *sdkmetric.MeterProvider
	meter         metric.Meter

	httpRequestsTotal       metric.Int64Counter
	proxyOverheadSeconds    metric.Float64Histogram
	sseChunkRelaySeconds    metric.Float64Histogram
	upstreamDurationSeconds metric.Float64Histogram
	deploymentCircuitState  metric.Int64Gauge
	sandboxTrapsTotal       metric.Int64Counter
	activeStreams           metric.Int64UpDownCounter
	configSnapshotAgeSec    metric.Float64Gauge
	telemetryDroppedTotal   metric.Int64Counter
	usageSettlementsTotal   metric.Int64Counter

	mu             sync.Mutex
	droppedBySig   map[string]int64
	activeStreamCt map[string]int64
	snapshotAgeFn  func() (syncChannel string, age time.Duration, ok bool)
}

// NewMetricsRegistry creates an isolated OpenTelemetry MeterProvider + Prometheus exporter
// containing all canonical Tanpopo RED/USE metrics with strict bounded cardinality.
func NewMetricsRegistry(cfg MetricsConfig) (*MetricsRegistry, error) {
	includeTenants := true
	if cfg.IncludeTenantLabels != nil {
		includeTenants = *cfg.IncludeTenantLabels
	}
	red := cfg.Redactor
	if red == nil {
		red = privacy.NewDefaultRedactor()
	}

	promReg := prometheus.NewRegistry()
	promExp, err := otelprom.New(
		otelprom.WithRegisterer(promReg),
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(promExp))
	meter := mp.Meter("github.com/nguywnben/tanpopo/telemetry")

	reg := &MetricsRegistry{
		includeTenantLabels: includeTenants,
		scrapeToken:         strings.TrimSpace(cfg.ScrapeBearerToken),
		allowUnauth:         cfg.AllowUnauthenticatedScrape,
		redactor:            red,
		promReg:             promReg,
		promExporter:        promExp,
		meterProvider:       mp,
		meter:               meter,
		droppedBySig:        make(map[string]int64),
		activeStreamCt:      make(map[string]int64),
	}

	var initErr error
	initInstrument := func(err error) {
		if err != nil && initErr == nil {
			initErr = err
		}
	}

	// Verify all canonical label schemas against AllowedMetricLabels at startup.
	for _, schema := range [][]string{
		{"plane", "org_id", "project_id", "logical_model", "status_class", "error_code"},
		{"plane", "extension_tier"},
		{"extension_tier"},
		{"adapter_id", "logical_model", "data_region", "status_class", "phase"},
		{"adapter_id", "data_region", "state"},
		{"adapter_id", "trap_reason"},
		{"plane"},
		{"sync_channel"},
		{"signal"},
		{"org_id", "project_id", "logical_model", "accuracy_tier"},
	} {
		initInstrument(ValidateMetricLabels(schema...))
	}

	var errC error
	reg.httpRequestsTotal, errC = meter.Int64Counter(MetricHTTPRequestsTotal, metric.WithDescription("Gateway request rate and error breakdown"))
	initInstrument(errC)

	reg.proxyOverheadSeconds, errC = meter.Float64Histogram(
		MetricProxyOverheadSeconds,
		metric.WithDescription("Internal Tanpopo proxy processing overhead in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.00025, 0.0005, 0.001, 0.002, 0.005, 0.010, 0.025, 0.050, 0.100),
	)
	initInstrument(errC)

	reg.sseChunkRelaySeconds, errC = meter.Float64Histogram(
		MetricSSEChunkRelaySeconds,
		metric.WithDescription("Per-chunk SSE forwarding delta in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.002, 0.005, 0.010),
	)
	initInstrument(errC)

	reg.upstreamDurationSeconds, errC = meter.Float64Histogram(
		MetricUpstreamDurationSeconds,
		metric.WithDescription("Upstream AI vendor response latency (ttft vs total) in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 15.0, 30.0, 60.0),
	)
	initInstrument(errC)

	reg.deploymentCircuitState, errC = meter.Int64Gauge(MetricDeploymentCircuitState, metric.WithDescription("Live SM-04 circuit-breaker posture across deployments (1=active state, 0=inactive)"))
	initInstrument(errC)

	reg.sandboxTrapsTotal, errC = meter.Int64Counter(MetricSandboxTrapsTotal, metric.WithDescription("Wasm sandbox faults/traps by installation and trap reason"))
	initInstrument(errC)

	reg.activeStreams, errC = meter.Int64UpDownCounter(MetricActiveStreams, metric.WithDescription("Concurrent active SSE streams by plane"))
	initInstrument(errC)

	reg.configSnapshotAgeSec, errC = meter.Float64Gauge(MetricConfigSnapshotAgeSeconds, metric.WithDescription("Age of active ConfigSnapshot synchronization in seconds"), metric.WithUnit("s"))
	initInstrument(errC)

	reg.telemetryDroppedTotal, errC = meter.Int64Counter(MetricTelemetryDroppedTotal, metric.WithDescription("Telemetry items dropped by bounded 32 MiB Drop-Oldest ring buffer"))
	initInstrument(errC)

	reg.usageSettlementsTotal, errC = meter.Int64Counter(MetricUsageSettlementsTotal, metric.WithDescription("Usage ledger settlements partitioned by accuracy tier"))
	initInstrument(errC)

	if initErr != nil {
		return nil, initErr
	}
	return reg, nil
}

// HasScrapeToken reports whether a non-empty bearer token protects HTTP /metrics scraping.
func (m *MetricsRegistry) HasScrapeToken() bool {
	return m != nil && m.scrapeToken != ""
}

func writeMetricsUnauth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":       "unauthorized",
			"type":       "authentication_error",
			"message":    "Missing or invalid internal metrics scrape token.",
			"request_id": "",
			"retryable":  false,
		},
	})
}

// Shutdown shuts down the underlying OpenTelemetry MeterProvider cleanly.
func (m *MetricsRegistry) Shutdown(ctx context.Context) error {
	if m == nil || m.meterProvider == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return m.meterProvider.Shutdown(ctx)
}

// ServeHTTP exposes Prometheus text exposition format at GET /metrics with
// fail-closed constant-time bearer auth matching openapi/dataplane-v1.yaml.
func (m *MetricsRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":       "method_not_allowed",
				"type":       "invalid_request_error",
				"message":    "GET is required on /metrics.",
				"request_id": "",
				"retryable":  false,
			},
		})
		return
	}
	if m.scrapeToken != "" {
		authHdr := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(authHdr, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authHdr, prefix)), []byte(m.scrapeToken)) != 1 {
			writeMetricsUnauth(w)
			return
		}
	} else if !m.allowUnauth {
		writeMetricsUnauth(w)
		return
	}
	m.mu.Lock()
	ageProvider := m.snapshotAgeFn
	m.mu.Unlock()
	if ageProvider != nil {
		if ch, age, ok := ageProvider(); ok {
			m.SetConfigSnapshotAge(ch, age)
		}
	}
	fams, err := m.promReg.Gather()
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":       "internal_error",
				"type":       "api_error",
				"message":    "Failed to gather telemetry metrics.",
				"request_id": "",
				"retryable":  true,
			},
		})
		return
	}
	format := expfmt.Negotiate(r.Header)
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, format)
	for _, mf := range fams {
		if err := enc.Encode(mf); err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if closer, ok := enc.(expfmt.Closer); ok {
		_ = closer.Close()
	}
	w.Header().Set("Content-Type", string(format))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// TimeSeriesCount returns the current number of distinct Prometheus time series
// currently registered in the isolated Prometheus registry.
func (m *MetricsRegistry) TimeSeriesCount() (int, error) {
	if m == nil || m.promReg == nil {
		return 0, nil
	}
	fams, err := m.promReg.Gather()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, fam := range fams {
		total += len(fam.GetMetric())
	}
	return total, nil
}

// RecordHTTPRequest records a bounded-cardinality HTTP gateway request counter.
// High-cardinality fields (request_id, virtual_key_id) are intentionally excluded by contract.
func (m *MetricsRegistry) RecordHTTPRequest(plane, orgID, projectID, logicalModel string, httpStatus int, errorCode string) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("plane", normalizePlane(plane)),
		attribute.String("logical_model", m.boundLabelValue(logicalModel, 64, "unknown")),
		attribute.String("status_class", statusClass(httpStatus)),
		attribute.String("error_code", m.boundErrorCode(errorCode)),
	}
	if m.includeTenantLabels {
		attrs = append(attrs,
			attribute.String("org_id", m.boundLabelValue(orgID, 40, "unknown")),
			attribute.String("project_id", m.boundLabelValue(projectID, 40, "unknown")),
		)
	}
	m.httpRequestsTotal.Add(context.Background(), 1, metric.WithAttributes(attrs...))
}

// RecordProxyOverhead records internal Tanpopo proxy processing latency.
func (m *MetricsRegistry) RecordProxyOverhead(plane, extensionTier string, d time.Duration) {
	if m == nil || d < 0 {
		return
	}
	m.proxyOverheadSeconds.Record(context.Background(), d.Seconds(), metric.WithAttributes(
		attribute.String("plane", normalizePlane(plane)),
		attribute.String("extension_tier", normalizeExtTier(extensionTier)),
	))
}

// RecordSSEChunkRelay records per-chunk SSE forwarding overhead.
func (m *MetricsRegistry) RecordSSEChunkRelay(extensionTier string, d time.Duration) {
	if m == nil || d < 0 {
		return
	}
	m.sseChunkRelaySeconds.Record(context.Background(), d.Seconds(), metric.WithAttributes(
		attribute.String("extension_tier", normalizeExtTier(extensionTier)),
	))
}

// RecordUpstreamCall records upstream provider duration for ttft or total phase.
func (m *MetricsRegistry) RecordUpstreamCall(adapterID, logicalModel, dataRegion string, httpStatus int, phase string, d time.Duration) {
	if m == nil || d < 0 {
		return
	}
	p := strings.ToLower(strings.TrimSpace(phase))
	if p != "ttft" {
		p = "total"
	}
	m.upstreamDurationSeconds.Record(context.Background(), d.Seconds(), metric.WithAttributes(
		attribute.String("adapter_id", m.boundLabelValue(adapterID, 64, "unknown")),
		attribute.String("logical_model", m.boundLabelValue(logicalModel, 64, "unknown")),
		attribute.String("data_region", m.boundRegion(dataRegion)),
		attribute.String("status_class", statusClass(httpStatus)),
		attribute.String("phase", p),
	))
}

// SetDeploymentCircuitState updates the SM-04 gauge across {healthy, cooldown, open, disabled}.
func (m *MetricsRegistry) SetDeploymentCircuitState(adapterID, dataRegion, activeState string) {
	if m == nil {
		return
	}
	normActive := normalizeCircuitLabel(activeState)
	adp := m.boundLabelValue(adapterID, 64, "unknown")
	reg := m.boundRegion(dataRegion)
	bgCtx := context.Background()
	for _, st := range []string{"healthy", "cooldown", "open", "disabled"} {
		val := int64(0)
		if st == normActive {
			val = 1
		}
		m.deploymentCircuitState.Record(bgCtx, val, metric.WithAttributes(
			attribute.String("adapter_id", adp),
			attribute.String("data_region", reg),
			attribute.String("state", st),
		))
	}
}

// RecordSandboxTrap increments tanpopo_sandbox_traps_total using bounded trap_reason enum.
func (m *MetricsRegistry) RecordSandboxTrap(adapterID, trapReason string) {
	if m == nil {
		return
	}
	m.sandboxTrapsTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("adapter_id", m.boundLabelValue(adapterID, 64, "unknown")),
		attribute.String("trap_reason", normalizeTrapReason(trapReason)),
	))
}

// AddActiveStream increments or decrements tanpopo_active_streams cleanly.
func (m *MetricsRegistry) AddActiveStream(plane string, delta int64) {
	if m == nil || delta == 0 {
		return
	}
	p := normalizePlane(plane)
	m.activeStreams.Add(context.Background(), delta, metric.WithAttributes(attribute.String("plane", p)))
	m.mu.Lock()
	m.activeStreamCt[p] += delta
	m.mu.Unlock()
}

// SetConfigSnapshotAge updates tanpopo_config_snapshot_age_seconds by sync_channel.
func (m *MetricsRegistry) SetConfigSnapshotAge(syncChannel string, age time.Duration) {
	if m == nil || age < 0 {
		return
	}
	ch := strings.ToLower(strings.TrimSpace(syncChannel))
	switch ch {
	case "valkey_pubsub", "pg_notify", "db_poll":
	default:
		ch = "db_poll"
	}
	m.configSnapshotAgeSec.Record(context.Background(), age.Seconds(), metric.WithAttributes(attribute.String("sync_channel", ch)))
}

// SetConfigSnapshotAgeProvider registers a scrape-time callback to update tanpopo_config_snapshot_age_seconds.
func (m *MetricsRegistry) SetConfigSnapshotAgeProvider(fn func() (syncChannel string, age time.Duration, ok bool)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.snapshotAgeFn = fn
	m.mu.Unlock()
}

// RecordDropped increments tanpopo_telemetry_dropped_total for traces, metrics, or logs.
func (m *MetricsRegistry) RecordDropped(signal string, count int64) {
	if m == nil || count <= 0 {
		return
	}
	sig := normalizeSignal(signal)
	m.telemetryDroppedTotal.Add(context.Background(), count, metric.WithAttributes(attribute.String("signal", sig)))
	m.mu.Lock()
	m.droppedBySig[sig] += count
	m.mu.Unlock()
}

// DroppedCount returns the cumulative dropped telemetry items for signal.
func (m *MetricsRegistry) DroppedCount(signal string) int64 {
	if m == nil {
		return 0
	}
	sig := normalizeSignal(signal)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.droppedBySig[sig]
}

// RecordUsageSettlement increments tanpopo_usage_settlements_total by accuracy tier.
func (m *MetricsRegistry) RecordUsageSettlement(orgID, projectID, logicalModel, accuracyTier string) {
	if m == nil {
		return
	}
	tier := strings.ToUpper(strings.TrimSpace(accuracyTier))
	switch tier {
	case "EXACT", "ESTIMATED", "UNMETERED_FLAGGED":
	default:
		tier = "ESTIMATED"
	}
	attrs := []attribute.KeyValue{
		attribute.String("logical_model", m.boundLabelValue(logicalModel, 64, "unknown")),
		attribute.String("accuracy_tier", tier),
	}
	if m.includeTenantLabels {
		attrs = append(attrs,
			attribute.String("org_id", m.boundLabelValue(orgID, 40, "unknown")),
			attribute.String("project_id", m.boundLabelValue(projectID, 40, "unknown")),
		)
	}
	m.usageSettlementsTotal.Add(context.Background(), 1, metric.WithAttributes(attrs...))
}

func (m *MetricsRegistry) boundLabelValue(raw string, maxLen int, fallback string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback
	}
	s = m.redactor.SanitizeString(s)
	if s == "" || strings.Contains(s, privacy.RedactedMarker) {
		return fallback
	}
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func (m *MetricsRegistry) boundErrorCode(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "none"
	}
	var b strings.Builder
	b.Grow(min(len(s), 64))
	for i := 0; i < len(s) && i < 64; i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			b.WriteByte(c)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func (m *MetricsRegistry) boundRegion(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "EU", "US", "APAC", "ON_PREM":
		return strings.ToUpper(strings.TrimSpace(raw))
	default:
		return "UNSPECIFIED"
	}
}

func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

func normalizePlane(plane string) string {
	switch strings.ToLower(strings.TrimSpace(plane)) {
	case "unified_http", "native_http", "mcp_http", "a2a_http", "playground", "admin_api":
		return strings.ToLower(strings.TrimSpace(plane))
	default:
		return "unified_http"
	}
}

func normalizeExtTier(tier string) string {
	lower := strings.ToLower(strings.TrimSpace(tier))
	switch {
	case strings.Contains(lower, "wasm"):
		return "wasm"
	case strings.Contains(lower, "sidecar"):
		return "sidecar"
	default:
		return "declarative"
	}
}

func normalizeTrapReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "cpu_deadline", "oom_16mib", "egress_denied", "abi_violation", "trap":
		return strings.ToLower(strings.TrimSpace(reason))
	default:
		return "abi_violation"
	}
}

func normalizeCircuitLabel(state string) string {
	upper := strings.ToUpper(strings.TrimSpace(state))
	switch upper {
	case "ACTIVE_HEALTHY", "HALF_OPEN_PROBING", "PROVISIONED", "HEALTHY":
		return "healthy"
	case "THROTTLED_COOLDOWN", "COOLDOWN":
		return "cooldown"
	case "CIRCUIT_OPEN_UNHEALTHY", "OPEN":
		return "open"
	case "DISABLED", "DRAINING_MAINTENANCE":
		return "disabled"
	default:
		return "healthy"
	}
}

func normalizeSignal(signal string) string {
	switch strings.ToLower(strings.TrimSpace(signal)) {
	case "traces", "metrics", "logs":
		return strings.ToLower(strings.TrimSpace(signal))
	default:
		return "traces"
	}
}

// SortedAllowedMetricLabels returns a deterministic sorted copy of AllowedMetricLabels.
func SortedAllowedMetricLabels() []string {
	out := make([]string, 0, len(AllowedMetricLabels))
	for k := range AllowedMetricLabels {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
