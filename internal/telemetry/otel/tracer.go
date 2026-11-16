// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package otel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
)

const (
	// DefaultMaxRingBufferBytes enforces the hard 32 MiB telemetry export queue ceiling (SPEC-CORE-02 / TP-AC-F0804).
	DefaultMaxRingBufferBytes int64 = 32 << 20
	// DefaultExportTimeout bounds each OTLP export batch attempt so hung collectors never stall the worker.
	DefaultExportTimeout = 2 * time.Second
	// DefaultFlushInterval bounds how long buffered spans wait before flushing a batch.
	DefaultFlushInterval = 50 * time.Millisecond
	// DefaultExportBatchSize limits per-batch export span counts.
	DefaultExportBatchSize = 64
	// MaxDebugCaptureTTLHours enforces the 72-hour maximum on opt-in debug payload capture (NEG-2).
	MaxDebugCaptureTTLHours int64 = 72
	// MaxDebugBlobCiphertextBytes enforces the 256 KiB cap on encrypted debug_payload_blobs payloads.
	MaxDebugBlobCiphertextBytes = 256 * 1024
	// aesGCMTagBytes accounts for the 16-byte GCM authentication tag when bounding plaintext.
	aesGCMTagBytes = 16
	// MaxActiveDebugSessions bounds the in-memory debug session registry.
	MaxActiveDebugSessions = 4096
)

var (
	// ErrDebugCaptureTTLExceeded indicates a debug payload capture session requested > 72h TTL (NEG-2).
	ErrDebugCaptureTTLExceeded = errors.New("telemetry: debug_capture_ttl_exceeds_72h")
	// ErrInvalidDebugCaptureSession indicates missing/invalid debug capture session parameters.
	ErrInvalidDebugCaptureSession = errors.New("telemetry: invalid debug capture session")
	// ErrDuplicateDebugCaptureSession indicates an active debug capture session with the same ID already exists.
	ErrDuplicateDebugCaptureSession = errors.New("telemetry: duplicate debug capture session")
	// ErrDebugCaptureSessionLimitExceeded indicates active debug capture sessions reached MaxActiveDebugSessions.
	ErrDebugCaptureSessionLimitExceeded = errors.New("telemetry: debug capture session limit exceeded")
)

// DebugCaptureError models a structured 422 API rejection matching NEG-2.
type DebugCaptureError struct {
	HTTPStatus int
	Code       string
	Message    string
	Err        error
}

func (e *DebugCaptureError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s (%d): %s", e.Code, e.HTTPStatus, e.Message)
}

func (e *DebugCaptureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ValidateDebugCaptureTTL enforces 1 <= ttlHours <= 72, returning a structured 422
// debug_capture_ttl_exceeds_72h error when ttlHours > 72 (NEG-2).
func ValidateDebugCaptureTTL(ttlHours int64) error {
	if ttlHours <= 0 {
		return &DebugCaptureError{
			HTTPStatus: http.StatusUnprocessableEntity,
			Code:       "invalid_debug_capture_ttl",
			Message:    "Debug capture TTL must be at least 1 hour.",
			Err:        ErrInvalidDebugCaptureSession,
		}
	}
	if ttlHours > MaxDebugCaptureTTLHours {
		return &DebugCaptureError{
			HTTPStatus: http.StatusUnprocessableEntity,
			Code:       "debug_capture_ttl_exceeds_72h",
			Message:    fmt.Sprintf("Debug capture TTL %d hours exceeds maximum 72 hours.", ttlHours),
			Err:        ErrDebugCaptureTTLExceeded,
		}
	}
	return nil
}

// DebugCaptureSession defines an explicit opt-in, time-bounded, project-scoped debug capture window.
type DebugCaptureSession struct {
	ID           string
	OrgID        string
	ProjectID    string
	VirtualKeyID string // optional narrowing filter; empty matches all keys in project
	CreatedAtMs  int64
	ExpiresAtMs  int64
}

// WaterfallStage captures a single execution stage inside request_traces_meta.waterfall_stages_json.
type WaterfallStage struct {
	Stage      string `json:"stage"`
	OffsetUs   int64  `json:"offset_us"`
	DurationUs int64  `json:"duration_us"`
	Status     string `json:"status,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// RequestTraceRecord holds strictly metadata-only attributes persisted into request_traces_meta.
type RequestTraceRecord struct {
	RequestID               string
	CreatedAtMs             int64
	StartedAtMs             int64
	OrgID                   string
	ProjectID               string
	EnvID                   string
	VirtualKeyID            string
	EntryPoint              string
	OtelTraceID             string
	RouteSlug               string
	SelectedDeploymentID    string
	ConnectionID            string
	ExtensionInstallationID string
	ExtensionVersionID      string
	FailoverAttempts        int
	HTTPStatus              int
	CanonicalErrorCode      string
	ProxyOverheadUs         int64
	TTFTMs                  *int64
	TotalDurationMs         int64
	WaterfallStages         []WaterfallStage
}

type bufferedSpanItem struct {
	span sdktrace.ReadOnlySpan
	size int64
}

// RingBufferConfig configures RingBufferSpanProcessor.
type RingBufferConfig struct {
	MaxBufferBytes int64
	ExportTimeout  time.Duration
	FlushInterval  time.Duration
	BatchSize      int
	Exporter       sdktrace.SpanExporter
	Metrics        *MetricsRegistry
	Redactor       *privacy.Redactor
}

// RingBufferSpanProcessor is an OTel sdktrace.SpanProcessor backed by a non-blocking
// <= 32 MiB Drop-Oldest ring buffer so unresponsive OTLP collectors never stall hot-path requests.
type RingBufferSpanProcessor struct {
	maxBytes      int64
	exportTimeout time.Duration
	flushInterval time.Duration
	batchSize     int
	exporter      sdktrace.SpanExporter
	metrics       *MetricsRegistry
	redactor      *privacy.Redactor

	mu           sync.Mutex
	ring         []bufferedSpanItem
	usedBytes    int64
	inflightByte int64
	peakBytes    int64
	droppedCount int64
	closed       bool

	notifyCh  chan struct{}
	flushCh   chan chan error
	loopCtx   context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewRingBufferSpanProcessor creates and starts a bounded non-blocking Drop-Oldest SpanProcessor.
func NewRingBufferSpanProcessor(cfg RingBufferConfig) *RingBufferSpanProcessor {
	maxBytes := cfg.MaxBufferBytes
	if maxBytes <= 0 || maxBytes > DefaultMaxRingBufferBytes {
		maxBytes = DefaultMaxRingBufferBytes
	}
	exportTimeout := cfg.ExportTimeout
	if exportTimeout <= 0 {
		exportTimeout = DefaultExportTimeout
	}
	flushInterval := cfg.FlushInterval
	if flushInterval <= 0 {
		flushInterval = DefaultFlushInterval
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultExportBatchSize
	}
	red := cfg.Redactor
	if red == nil {
		red = privacy.NewDefaultRedactor()
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &RingBufferSpanProcessor{
		maxBytes:      maxBytes,
		exportTimeout: exportTimeout,
		flushInterval: flushInterval,
		batchSize:     batchSize,
		exporter:      cfg.Exporter,
		metrics:       cfg.Metrics,
		redactor:      red,
		notifyCh:      make(chan struct{}, 1),
		flushCh:       make(chan chan error, 16),
		loopCtx:       ctx,
		cancel:        cancel,
	}
	p.wg.Add(1)
	go p.exportLoop()
	return p
}

// OnStart satisfies sdktrace.SpanProcessor.
func (p *RingBufferSpanProcessor) OnStart(_ context.Context, _ sdktrace.ReadWriteSpan) {}

// OnEnd enqueues a privacy-scrubbed span snapshot into the bounded ring buffer without blocking.
func (p *RingBufferSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if p == nil || s == nil {
		return
	}
	scrubbed := p.scrubSpan(s)
	itemBytes := estimateSpanBytes(scrubbed)

	var droppedNow int64
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if itemBytes > p.maxBytes {
		p.droppedCount++
		p.mu.Unlock()
		if p.metrics != nil {
			p.metrics.RecordDropped("traces", 1)
		}
		return
	}
	for (p.usedBytes+p.inflightByte+itemBytes) > p.maxBytes && len(p.ring) > 0 {
		oldest := p.ring[0]
		p.ring[0] = bufferedSpanItem{}
		p.ring = p.ring[1:]
		p.usedBytes -= oldest.size
		p.droppedCount++
		droppedNow++
	}
	// Compact slice backing array cleanly when head advances past half capacity.
	if len(p.ring) == 0 {
		p.ring = nil
	} else if cap(p.ring) > 64 && len(p.ring)*2 < cap(p.ring) {
		shrunk := make([]bufferedSpanItem, len(p.ring))
		copy(shrunk, p.ring)
		p.ring = shrunk
	}
	if (p.usedBytes + p.inflightByte + itemBytes) > p.maxBytes {
		// In-flight export already consumes the budget; drop incoming cleanly without overflowing cap.
		p.droppedCount++
		droppedNow++
		p.mu.Unlock()
		if p.metrics != nil {
			p.metrics.RecordDropped("traces", droppedNow)
		}
		return
	}
	p.ring = append(p.ring, bufferedSpanItem{span: scrubbed, size: itemBytes})
	p.usedBytes += itemBytes
	if total := p.usedBytes + p.inflightByte; total > p.peakBytes {
		p.peakBytes = total
	}
	p.mu.Unlock()

	if droppedNow > 0 && p.metrics != nil {
		p.metrics.RecordDropped("traces", droppedNow)
	}
	select {
	case p.notifyCh <- struct{}{}:
	default:
	}
}

// ForceFlush requests the background worker to drain queued spans immediately.
func (p *RingBufferSpanProcessor) ForceFlush(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return nil
	}
	done := make(chan error, 1)
	select {
	case p.flushCh <- done:
	case <-p.loopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-p.loopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown cancels pending OTLP export I/O immediately, stops the worker goroutine,
// and shuts down any wrapped exporter cleanly with zero leaked goroutines.
func (p *RingBufferSpanProcessor) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var err error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.ring = nil
		p.usedBytes = 0
		p.mu.Unlock()

		p.cancel()
		p.wg.Wait()

		if p.exporter != nil {
			if ctx == nil {
				ctx = context.Background()
			}
			err = p.exporter.Shutdown(ctx)
		}
	})
	return err
}

// BufferStats returns (currentUsedBytes, peakUsedBytes, droppedSpans) for diagnostics and tests.
func (p *RingBufferSpanProcessor) BufferStats() (usedBytes, peakBytes, droppedSpans int64) {
	if p == nil {
		return 0, 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usedBytes + p.inflightByte, p.peakBytes, p.droppedCount
}

func (p *RingBufferSpanProcessor) exportLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.loopCtx.Done():
			return
		case done := <-p.flushCh:
			done <- p.drainAll()
		case <-p.notifyCh:
			p.drainAvailable()
		case <-ticker.C:
			p.drainAvailable()
		}
	}
}

func (p *RingBufferSpanProcessor) drainAll() error {
	var firstErr error
	for {
		if p.loopCtx.Err() != nil {
			return nil
		}
		if !p.exportNextBatch(&firstErr) {
			return firstErr
		}
	}
}

func (p *RingBufferSpanProcessor) drainAvailable() {
	for {
		if p.loopCtx.Err() != nil {
			return
		}
		if !p.exportNextBatch(nil) {
			return
		}
	}
}

func (p *RingBufferSpanProcessor) exportNextBatch(firstErr *error) bool {
	p.mu.Lock()
	if len(p.ring) == 0 || p.closed {
		p.mu.Unlock()
		return false
	}
	n := len(p.ring)
	if n > p.batchSize {
		n = p.batchSize
	}
	batch := make([]sdktrace.ReadOnlySpan, n)
	var batchBytes int64
	for i := 0; i < n; i++ {
		batch[i] = p.ring[i].span
		batchBytes += p.ring[i].size
		p.ring[i] = bufferedSpanItem{}
	}
	p.ring = p.ring[n:]
	if len(p.ring) == 0 {
		p.ring = nil
	}
	p.usedBytes -= batchBytes
	p.inflightByte += batchBytes
	p.mu.Unlock()

	if p.exporter != nil {
		expCtx, cancel := context.WithTimeout(p.loopCtx, p.exportTimeout)
		err := p.exporter.ExportSpans(expCtx, batch)
		cancel()
		if err != nil {
			p.mu.Lock()
			p.droppedCount += int64(len(batch))
			p.mu.Unlock()
			if p.metrics != nil {
				p.metrics.RecordDropped("traces", int64(len(batch)))
			}
			if firstErr != nil && *firstErr == nil {
				*firstErr = err
			}
		}
	}

	p.mu.Lock()
	p.inflightByte -= batchBytes
	if p.inflightByte < 0 {
		p.inflightByte = 0
	}
	hasMore := len(p.ring) > 0 && !p.closed
	p.mu.Unlock()
	return hasMore
}

type scrubbedSpanSnapshot struct {
	sdktrace.ReadOnlySpan
	name       string
	attributes []attribute.KeyValue
	events     []sdktrace.Event
	statusMsg  string
}

func (s *scrubbedSpanSnapshot) Name() string                      { return s.name }
func (s *scrubbedSpanSnapshot) Attributes() []attribute.KeyValue  { return s.attributes }
func (s *scrubbedSpanSnapshot) Events() []sdktrace.Event          { return s.events }
func (s *scrubbedSpanSnapshot) Status() sdktrace.Status {
	st := s.ReadOnlySpan.Status()
	st.Description = s.statusMsg
	return st
}

func (p *RingBufferSpanProcessor) scrubSpan(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	rawAttrs := s.Attributes()
	safeAttrs := make([]attribute.KeyValue, 0, len(rawAttrs))
	for _, kv := range rawAttrs {
		if sanitized, ok := p.scrubAttr(kv); ok {
			safeAttrs = append(safeAttrs, sanitized)
		}
	}
	rawEvents := s.Events()
	var safeEvents []sdktrace.Event
	if len(rawEvents) > 0 {
		safeEvents = make([]sdktrace.Event, 0, len(rawEvents))
		for _, ev := range rawEvents {
			evAttrs := make([]attribute.KeyValue, 0, len(ev.Attributes))
			for _, kv := range ev.Attributes {
				if sanitized, ok := p.scrubAttr(kv); ok {
					evAttrs = append(evAttrs, sanitized)
				}
			}
			safeEvents = append(safeEvents, sdktrace.Event{
				Name:                  p.redactor.SanitizeString(ev.Name),
				Attributes:            evAttrs,
				DroppedAttributeCount: ev.DroppedAttributeCount,
				Time:                  ev.Time,
			})
		}
	}
	return &scrubbedSpanSnapshot{
		ReadOnlySpan: s,
		name:         p.redactor.SanitizeString(s.Name()),
		attributes:   safeAttrs,
		events:       safeEvents,
		statusMsg:    p.redactor.SanitizeString(s.Status().Description),
	}
}

func (p *RingBufferSpanProcessor) scrubAttr(kv attribute.KeyValue) (attribute.KeyValue, bool) {
	k := string(kv.Key)
	if privacy.IsSensitiveKey(k) {
		return attribute.String(k, privacy.RedactedMarker), true
	}
	if !p.redactor.CapturePayloads && privacy.IsPayloadKey(k) {
		return attribute.KeyValue{}, false
	}
	switch kv.Value.Type() {
	case attribute.STRING:
		return attribute.String(k, p.redactor.SanitizeString(kv.Value.AsString())), true
	case attribute.STRINGSLICE:
		in := kv.Value.AsStringSlice()
		out := make([]string, len(in))
		for i, v := range in {
			out[i] = p.redactor.SanitizeString(v)
		}
		return attribute.StringSlice(k, out), true
	default:
		return kv, true
	}
}

func estimateSpanBytes(s sdktrace.ReadOnlySpan) int64 {
	size := int64(256 + len(s.Name()) + len(s.Status().Description))
	for _, a := range s.Attributes() {
		size += int64(len(a.Key) + 32)
		switch a.Value.Type() {
		case attribute.STRING:
			size += int64(len(a.Value.AsString()))
		case attribute.STRINGSLICE:
			for _, item := range a.Value.AsStringSlice() {
				size += int64(len(item) + 8)
			}
		}
	}
	for _, ev := range s.Events() {
		size += int64(len(ev.Name) + 48)
		for _, a := range ev.Attributes {
			size += int64(len(a.Key) + 16 + len(a.Value.Emit()))
		}
	}
	return size
}

// PipelineConfig wires Tanpopo's unified observability plane (metrics, traces, logs, TTL store).
type PipelineConfig struct {
	ServiceName         string
	ServiceVersion      string
	IncludeTenantLabels *bool
	MetricsScrapeToken  string
	OTLPEndpoint        string
	OTLPHTTPClient      *http.Client
	OTLPExporter        sdktrace.SpanExporter
	OTLPAllowPrivateIP  bool
	OTLPAllowedCIDRs    []string
	OTLPControlHook     func(network, address string, c syscall.RawConn) error
	MaxRingBufferBytes  int64
	ExportTimeout       time.Duration
	FlushInterval       time.Duration
	CapturePayloads     bool // Always false by default per OPS-AC-10
	DB                  *sql.DB
	Vault               *vault.Store
	LogOut              io.Writer
	NowMs               func() int64
}

type guardedOTLPClient struct {
	endpointURL string
	httpClient  *http.Client
	stopOnce    sync.Once
	stopCh      chan struct{}
}

func (c *guardedOTLPClient) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (c *guardedOTLPClient) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (c *guardedOTLPClient) UploadTraces(ctx context.Context, protoSpans []*tracepb.ResourceSpans) error {
	select {
	case <-c.stopCh:
		return errors.New("telemetry: OTLP client stopped")
	default:
	}
	pbReq := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: protoSpans,
	}
	raw, err := proto.Marshal(pbReq)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry: OTLP collector returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// Pipeline bundles Metrics, RingBufferSpanProcessor, Tracer, JSONLogger, trace DB persistence,
// and opt-in Project-DEK debug payload capture.
type Pipeline struct {
	Metrics        *MetricsRegistry
	Redactor       *privacy.Redactor
	Logger         *privacy.JSONLogger
	Ring           *RingBufferSpanProcessor
	TracerProvider *sdktrace.TracerProvider
	Tracer         trace.Tracer

	db    *sql.DB
	vault *vault.Store
	nowMs func() int64

	sessMu   sync.RWMutex
	sessions map[string]DebugCaptureSession // key: session ID
	closed   atomic.Bool
}

// NewPipeline initializes the complete telemetry pipeline with strict metadata-only defaults.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	redactor := &privacy.Redactor{CapturePayloads: cfg.CapturePayloads}
	nowMs := cfg.NowMs
	if nowMs == nil {
		nowMs = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	nowFn := func() time.Time { return time.UnixMilli(nowMs()).UTC() }

	metrics, err := NewMetricsRegistry(MetricsConfig{
		IncludeTenantLabels: cfg.IncludeTenantLabels,
		ScrapeBearerToken:   cfg.MetricsScrapeToken,
		Redactor:            redactor,
	})
	if err != nil {
		return nil, err
	}

	exporter := cfg.OTLPExporter
	if exporter == nil && strings.TrimSpace(cfg.OTLPEndpoint) != "" {
		expTimeout := cfg.ExportTimeout
		if expTimeout <= 0 {
			expTimeout = DefaultExportTimeout
		}
		client := cfg.OTLPHTTPClient
		parsed, uerr := url.Parse(strings.TrimSpace(cfg.OTLPEndpoint))
		scheme := ""
		if parsed != nil {
			scheme = strings.ToLower(strings.TrimSpace(parsed.Scheme))
		}
		if uerr != nil || (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return nil, fmt.Errorf("telemetry: invalid OTLP endpoint %q", cfg.OTLPEndpoint)
		}
		if client == nil {
			guarded, gerr := egress.NewGuardedHTTPClient(egress.ClientConfig{
				Policy: egress.Policy{
					AllowedHosts:   []string{parsed.Host},
					RequireTLS:     strings.EqualFold(parsed.Scheme, "https"),
					AllowPrivateIP: cfg.OTLPAllowPrivateIP,
					AllowedCIDRs:   cfg.OTLPAllowedCIDRs,
				},
				ControlHook: cfg.OTLPControlHook,
				Timeout:     expTimeout,
			})
			if gerr != nil {
				return nil, fmt.Errorf("telemetry: construct guarded OTLP HTTP client: %w", gerr)
			}
			client = guarded
		}
		targetURL := strings.TrimSpace(cfg.OTLPEndpoint)
		if strings.TrimSpace(parsed.Path) == "" || parsed.Path == "/" {
			parsedCopy := *parsed
			parsedCopy.Path = "/v1/traces"
			targetURL = parsedCopy.String()
		}
		exporter, err = otlptrace.New(context.Background(), &guardedOTLPClient{
			endpointURL: targetURL,
			httpClient:  client,
			stopCh:      make(chan struct{}),
		})
		if err != nil {
			return nil, fmt.Errorf("telemetry: initialize guarded OTLP HTTP exporter: %w", err)
		}
	}

	ring := NewRingBufferSpanProcessor(RingBufferConfig{
		MaxBufferBytes: cfg.MaxRingBufferBytes,
		ExportTimeout:  cfg.ExportTimeout,
		FlushInterval:  cfg.FlushInterval,
		Exporter:       exporter,
		Metrics:        metrics,
		Redactor:       redactor,
	})

	svcName := strings.TrimSpace(cfg.ServiceName)
	if svcName == "" {
		svcName = "tanpopo"
	}
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(svcName),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(ring),
	)

	return &Pipeline{
		Metrics:        metrics,
		Redactor:       redactor,
		Logger:         privacy.NewJSONLogger(cfg.LogOut, redactor, nowFn),
		Ring:           ring,
		TracerProvider: tp,
		Tracer:         tp.Tracer("github.com/nguywnben/tanpopo/dataplane"),
		db:             cfg.DB,
		vault:          cfg.Vault,
		nowMs:          nowMs,
		sessions:       make(map[string]DebugCaptureSession),
	}, nil
}

// Shutdown flushes/terminates the ring buffer and OTel tracer provider cleanly.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.closed.Store(true)
	if ctx == nil {
		ctx = context.Background()
	}
	return p.TracerProvider.Shutdown(ctx)
}

// CreateDebugCaptureSession validates and activates an opt-in, project-scoped debug capture
// session with strict TTL <= 72 hours (rejecting > 72h with 422 debug_capture_ttl_exceeds_72h).
func (p *Pipeline) CreateDebugCaptureSession(sessionID, orgID, projectID, virtualKeyID string, ttlHours int64) (*DebugCaptureSession, error) {
	if err := ValidateDebugCaptureTTL(ttlHours); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(orgID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, &DebugCaptureError{
			HTTPStatus: http.StatusUnprocessableEntity,
			Code:       "invalid_debug_capture_session",
			Message:    "session_id, org_id, and project_id are required.",
			Err:        ErrInvalidDebugCaptureSession,
		}
	}
	now := p.nowMs()
	sess := DebugCaptureSession{
		ID:           strings.TrimSpace(sessionID),
		OrgID:        strings.TrimSpace(orgID),
		ProjectID:    strings.TrimSpace(projectID),
		VirtualKeyID: strings.TrimSpace(virtualKeyID),
		CreatedAtMs:  now,
		ExpiresAtMs:  now + ttlHours*int64(time.Hour/time.Millisecond),
	}
	p.sessMu.Lock()
	for id, existing := range p.sessions {
		if now >= existing.ExpiresAtMs {
			delete(p.sessions, id)
		}
	}
	if _, dup := p.sessions[sess.ID]; dup {
		p.sessMu.Unlock()
		return nil, &DebugCaptureError{
			HTTPStatus: http.StatusConflict,
			Code:       "duplicate_debug_capture_session",
			Message:    fmt.Sprintf("Debug capture session %q already exists and is active.", sess.ID),
			Err:        ErrDuplicateDebugCaptureSession,
		}
	}
	if len(p.sessions) >= MaxActiveDebugSessions {
		p.sessMu.Unlock()
		return nil, &DebugCaptureError{
			HTTPStatus: http.StatusTooManyRequests,
			Code:       "debug_capture_session_limit_exceeded",
			Message:    fmt.Sprintf("Maximum active debug capture sessions (%d) reached.", MaxActiveDebugSessions),
			Err:        ErrDebugCaptureSessionLimitExceeded,
		}
	}
	p.sessions[sess.ID] = sess
	p.sessMu.Unlock()
	cp := sess
	return &cp, nil
}

// HasActiveDebugSession reports whether an active non-expired debug capture session
// authorizes capturing debug payload blobs for (orgID, projectID, virtualKeyID).
func (p *Pipeline) HasActiveDebugSession(orgID, projectID, virtualKeyID string) bool {
	if p == nil {
		return false
	}
	now := p.nowMs()
	p.sessMu.RLock()
	defer p.sessMu.RUnlock()
	for _, sess := range p.sessions {
		if now >= sess.ExpiresAtMs {
			continue
		}
		if sess.OrgID == orgID && sess.ProjectID == projectID {
			if sess.VirtualKeyID == "" || sess.VirtualKeyID == virtualKeyID {
				return true
			}
		}
	}
	return false
}

// activeSessionExpiry returns the shortest active session expiry capped at 72h from createdAtMs.
func (p *Pipeline) activeSessionExpiry(orgID, projectID, virtualKeyID string, createdAtMs int64) (int64, bool) {
	now := p.nowMs()
	p.sessMu.RLock()
	defer p.sessMu.RUnlock()
	var best int64
	found := false
	maxExpiry := createdAtMs + MaxDebugCaptureTTLHours*int64(time.Hour/time.Millisecond)
	for _, sess := range p.sessions {
		if now >= sess.ExpiresAtMs || sess.ExpiresAtMs <= createdAtMs {
			continue
		}
		if sess.OrgID == orgID && sess.ProjectID == projectID && (sess.VirtualKeyID == "" || sess.VirtualKeyID == virtualKeyID) {
			exp := sess.ExpiresAtMs
			if exp > maxExpiry {
				exp = maxExpiry
			}
			if !found || exp < best {
				best = exp
				found = true
			}
		}
	}
	return best, found
}

// StartRequestSpan creates the root Ingress waterfall span linking an inbound W3C traceparent
// and attaches metadata-only request_id and entry_point attributes.
func (p *Pipeline) StartRequestSpan(ctx context.Context, traceparent, requestID, entryPoint string) (context.Context, trace.Span, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.Tracer == nil {
		return ctx, trace.SpanFromContext(ctx), extractTraceIDFromHeader(traceparent)
	}
	if sc, ok := parseW3CTraceparent(traceparent); ok {
		ctx = trace.ContextWithRemoteSpanContext(ctx, sc)
	}
	ctx, span := p.Tracer.Start(ctx, "Ingress",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("request_id", p.Redactor.SanitizeString(requestID)),
			attribute.String("entry_point", normalizePlane(entryPoint)),
		),
	)
	traceID := ""
	if sc := span.SpanContext(); sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	return ctx, span, traceID
}

// StartStageSpan starts a named child waterfall span ("Policy", "RouteAttempt", "ExtensionHook", "Egress")
// with attributes scrubbed through privacy.Redactor at creation time.
func (p *Pipeline) StartStageSpan(ctx context.Context, stageName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.Tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	safe := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		k := string(kv.Key)
		if privacy.IsSensitiveKey(k) {
			safe = append(safe, attribute.String(k, privacy.RedactedMarker))
			continue
		}
		if !p.Redactor.CapturePayloads && privacy.IsPayloadKey(k) {
			continue
		}
		if kv.Value.Type() == attribute.STRING {
			safe = append(safe, attribute.String(k, p.Redactor.SanitizeString(kv.Value.AsString())))
		} else {
			safe = append(safe, kv)
		}
	}
	return p.Tracer.Start(ctx, p.Redactor.SanitizeString(stageName), trace.WithAttributes(safe...))
}

// EndSpan finishes a span cleanly, recording error status if err != nil (with redacted error message).
func (p *Pipeline) EndSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		msg := err.Error()
		if p != nil && p.Redactor != nil {
			msg = p.Redactor.SanitizeString(msg)
		}
		span.SetStatus(codes.Error, msg)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// RecordRequestTrace persists strictly metadata-only waterfall trace fields to request_traces_meta.
func (p *Pipeline) RecordRequestTrace(ctx context.Context, rec RequestTraceRecord) error {
	if p == nil || p.db == nil || p.closed.Load() {
		return nil
	}
	if strings.TrimSpace(rec.OrgID) == "" || strings.TrimSpace(rec.RequestID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := rec.CreatedAtMs
	if now <= 0 {
		now = p.nowMs()
	}
	started := rec.StartedAtMs
	if started <= 0 || started > now {
		started = now
	}
	totalMs := rec.TotalDurationMs
	if totalMs < 0 {
		totalMs = 0
	}
	overheadUs := rec.ProxyOverheadUs
	if overheadUs < 0 {
		overheadUs = 0
	}
	attempts := rec.FailoverAttempts
	if attempts < 0 {
		attempts = 0
	}

	safeStages := make([]WaterfallStage, 0, len(rec.WaterfallStages))
	for _, st := range rec.WaterfallStages {
		off := st.OffsetUs
		if off < 0 {
			off = 0
		}
		dur := st.DurationUs
		if dur < 0 {
			dur = 0
		}
		safeStages = append(safeStages, WaterfallStage{
			Stage:      p.Redactor.SanitizeString(st.Stage),
			OffsetUs:   off,
			DurationUs: dur,
			Status:     p.Redactor.SanitizeString(st.Status),
			Detail:     p.Redactor.SanitizeString(st.Detail),
		})
	}
	stagesJSONBytes, err := json.Marshal(safeStages)
	if err != nil {
		stagesJSONBytes = []byte("[]")
	}

	entryPoint := normalizeTraceEntryPoint(rec.EntryPoint)
	otelTraceID := strings.TrimSpace(rec.OtelTraceID)
	if len(otelTraceID) != 32 {
		otelTraceID = ""
	}
	errCode := strings.TrimSpace(p.Redactor.SanitizeString(rec.CanonicalErrorCode))
	if len(errCode) > 64 {
		errCode = errCode[:64]
	}

	_, err = p.db.ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id,
			entry_point, otel_trace_id, route_slug, selected_deployment_id, connection_id,
			extension_installation_id, extension_version_id, failover_attempts, http_status,
			canonical_error_code, proxy_overhead_us, ttft_ms, total_duration_ms, waterfall_stages_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(org_id, request_id, created_at_ms) DO NOTHING`,
		truncateStr(rec.RequestID, 40),
		now,
		started,
		truncateStr(rec.OrgID, 40),
		truncateStr(rec.ProjectID, 40),
		truncateStr(rec.EnvID, 40),
		nullIfEmpty(truncateStr(rec.VirtualKeyID, 40)),
		entryPoint,
		nullIfEmpty(otelTraceID),
		nullIfEmpty(truncateStr(p.Redactor.SanitizeString(rec.RouteSlug), 128)),
		nullIfEmpty(truncateStr(rec.SelectedDeploymentID, 40)),
		nullIfEmpty(truncateStr(rec.ConnectionID, 40)),
		nullIfEmpty(truncateStr(rec.ExtensionInstallationID, 40)),
		nullIfEmpty(truncateStr(rec.ExtensionVersionID, 40)),
		attempts,
		rec.HTTPStatus,
		nullIfEmpty(errCode),
		overheadUs,
		nullInt64Ptr(rec.TTFTMs),
		totalMs,
		string(stagesJSONBytes),
	)
	return err
}

// StoreDebugBlobIfAuthorized persists an AEAD Project-DEK-sealed debug payload blob (capped
// strictly <= 256 KiB ciphertext) ONLY when an active, non-expired DebugCaptureSession covers
// (orgID, projectID, virtualKeyID). By default (no active session), 0 bytes are persisted.
func (p *Pipeline) StoreDebugBlobIfAuthorized(ctx context.Context, orgID, projectID, virtualKeyID, requestID string, rawPayload []byte) (bool, error) {
	if p == nil || p.db == nil || p.vault == nil || len(rawPayload) == 0 {
		return false, nil
	}
	now := p.nowMs()
	expMs, ok := p.activeSessionExpiry(orgID, projectID, virtualKeyID, now)
	if !ok {
		return false, nil
	}
	// Always scrub credentials (including non-prefixed JSON secret fields, Bearer, vk_tp_*, sk-*, cookies)
	// before encrypting the opt-in debug payload.
	maxPT := MaxDebugBlobCiphertextBytes - aesGCMTagBytes
	scrubbed := p.Redactor.SanitizeDebugPayload(rawPayload, maxPT)
	defer clear(scrubbed)

	nonce, ct, err := p.vault.SealProjectBlob(ctx, orgID, projectID, vault.RecordKindDebugBlob, requestID, scrubbed)
	if err != nil {
		return false, fmt.Errorf("telemetry: seal debug payload blob: %w", err)
	}
	if len(ct) > MaxDebugBlobCiphertextBytes {
		return false, fmt.Errorf("telemetry: encrypted debug payload blob exceeds 256 KiB limit")
	}

	_, err = p.db.ExecContext(ctx, `
		INSERT INTO debug_payload_blobs (
			request_id, org_id, project_id, aead_alg, nonce, encrypted_payload_aead, created_at_ms, expires_at_ms
		) VALUES (?, ?, ?, 'AES-256-GCM', ?, ?, ?, ?)
		ON CONFLICT(org_id, project_id, request_id) DO NOTHING`,
		truncateStr(requestID, 40),
		truncateStr(orgID, 40),
		truncateStr(projectID, 40),
		nonce,
		ct,
		now,
		expMs,
	)
	if err != nil {
		return false, fmt.Errorf("telemetry: insert debug_payload_blobs: %w", err)
	}
	return true, nil
}

func normalizeTraceEntryPoint(ep string) string {
	switch strings.ToLower(strings.TrimSpace(ep)) {
	case "unified_http", "native_http", "mcp_http", "a2a_http", "playground":
		return strings.ToLower(strings.TrimSpace(ep))
	default:
		return "unified_http"
	}
}

func parseW3CTraceparent(raw string) (trace.SpanContext, bool) {
	s := strings.TrimSpace(raw)
	if len(s) != 55 || s[2] != '-' || s[35] != '-' || s[52] != '-' {
		return trace.SpanContext{}, false
	}
	if s[0] != '0' || s[1] != '0' {
		return trace.SpanContext{}, false
	}
	tidBytes, err := hex.DecodeString(s[3:35])
	if err != nil || len(tidBytes) != 16 {
		return trace.SpanContext{}, false
	}
	sidBytes, err := hex.DecodeString(s[36:52])
	if err != nil || len(sidBytes) != 8 {
		return trace.SpanContext{}, false
	}
	flagsBytes, err := hex.DecodeString(s[53:55])
	if err != nil || len(flagsBytes) != 1 {
		return trace.SpanContext{}, false
	}
	var tid trace.TraceID
	var sid trace.SpanID
	copy(tid[:], tidBytes)
	copy(sid[:], sidBytes)
	if !tid.IsValid() || !sid.IsValid() {
		return trace.SpanContext{}, false
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.TraceFlags(flagsBytes[0]),
		Remote:     true,
	}), true
}

func extractTraceIDFromHeader(tp string) string {
	if sc, ok := parseW3CTraceparent(tp); ok {
		return sc.TraceID().String()
	}
	return ""
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullInt64Ptr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
