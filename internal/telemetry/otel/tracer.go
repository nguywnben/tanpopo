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
	"github.com/nguywnben/tanpopo/internal/storage/port"
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
	// DefaultMaxPersistQueueBytes caps the async trace/blob DB persistence queue at 8 MiB.
	DefaultMaxPersistQueueBytes int64 = 8 << 20
	// DefaultMaxPersistQueueItems caps queued DB trace/blob records to bound memory overhead.
	DefaultMaxPersistQueueItems = 4096
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
	links      []sdktrace.Link
	statusMsg  string
}

func (s *scrubbedSpanSnapshot) Name() string                      { return s.name }
func (s *scrubbedSpanSnapshot) Attributes() []attribute.KeyValue  { return s.attributes }
func (s *scrubbedSpanSnapshot) Events() []sdktrace.Event          { return s.events }
func (s *scrubbedSpanSnapshot) Links() []sdktrace.Link            { return s.links }
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
	rawLinks := s.Links()
	var safeLinks []sdktrace.Link
	if len(rawLinks) > 0 {
		safeLinks = make([]sdktrace.Link, 0, len(rawLinks))
		for _, lk := range rawLinks {
			lkAttrs := make([]attribute.KeyValue, 0, len(lk.Attributes))
			for _, kv := range lk.Attributes {
				if sanitized, ok := p.scrubAttr(kv); ok {
					lkAttrs = append(lkAttrs, sanitized)
				}
			}
			safeLinks = append(safeLinks, sdktrace.Link{
				SpanContext:           lk.SpanContext,
				Attributes:            lkAttrs,
				DroppedAttributeCount: lk.DroppedAttributeCount,
			})
		}
	}
	return &scrubbedSpanSnapshot{
		ReadOnlySpan: s,
		name:         p.redactor.SanitizeString(s.Name()),
		attributes:   safeAttrs,
		events:       safeEvents,
		links:        safeLinks,
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
	ServiceName               string
	ServiceVersion            string
	IncludeTenantLabels       *bool
	MetricsScrapeToken        string
	AllowUnauthenticatedScrape bool
	OTLPEndpoint              string
	OTLPTracesEndpoint        string
	OTLPMetricsEndpoint       string
	OTLPLogsEndpoint          string
	OTLPHTTPClient            *http.Client
	OTLPExporter              sdktrace.SpanExporter
	OTLPAllowPrivateIP        bool
	OTLPAllowedCIDRs          []string
	OTLPControlHook           func(network, address string, c syscall.RawConn) error
	MaxRingBufferBytes        int64
	MaxPersistQueueBytes      int64
	ExportTimeout             time.Duration
	FlushInterval             time.Duration
	MetricsExportInterval     time.Duration
	CapturePayloads           bool // Always false by default per OPS-AC-10
	DB                        *sql.DB
	Vault                     *vault.Store
	LogOut                    io.Writer
	NowMs                     func() int64
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

type debugBlobTask struct {
	orgID        string
	projectID    string
	virtualKeyID string
	requestID    string
	payload      []byte
	preScrubbed  bool
}

type persistTask struct {
	trace *RequestTraceRecord
	blob  *debugBlobTask
	bytes int64
}

// Pipeline bundles Metrics, RingBufferSpanProcessor, Tracer, JSONLogger, non-blocking
// trace/blob DB persistence worker, and opt-in Project-DEK debug payload capture.
type Pipeline struct {
	Metrics        *MetricsRegistry
	Redactor       *privacy.Redactor
	Logger         *privacy.JSONLogger
	Ring           *RingBufferSpanProcessor
	TracerProvider *sdktrace.TracerProvider
	Tracer         trace.Tracer

	logExporter     *ringLogExporter
	metricsExporter *otlpMetricsExporter

	db      *sql.DB
	dialect port.Dialect
	vault   *vault.Store
	nowMs   func() int64

	sessMu   sync.RWMutex
	sessions map[string]DebugCaptureSession // key: session ID
	closed   atomic.Bool

	persistMaxBytes int64
	persistMaxItems int
	persistMu       sync.Mutex
	persistQueue    []persistTask
	persistBytes    int64
	persistNotify   chan struct{}
	persistFlushCh  chan chan struct{}
	persistStopCh   chan struct{}
	persistWG       sync.WaitGroup
	shutdownOnce    sync.Once
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
		IncludeTenantLabels:        cfg.IncludeTenantLabels,
		ScrapeBearerToken:          cfg.MetricsScrapeToken,
		AllowUnauthenticatedScrape: cfg.AllowUnauthenticatedScrape,
		Redactor:                   redactor,
	})
	if err != nil {
		return nil, err
	}

	svcName := strings.TrimSpace(cfg.ServiceName)
	if svcName == "" {
		svcName = "tanpopo"
	}

	var (
		exporter    = cfg.OTLPExporter
		otlpClient  = cfg.OTLPHTTPClient
		logExporter *ringLogExporter
		metExporter *otlpMetricsExporter
	)
	expTimeout := cfg.ExportTimeout
	if expTimeout <= 0 {
		expTimeout = DefaultExportTimeout
	}
	endpoints, epErr := resolveOTLPSignalEndpoints(cfg)
	if epErr != nil {
		return nil, epErr
	}
	if endpoints != nil && otlpClient == nil {
		guarded, gerr := egress.NewGuardedHTTPClient(egress.ClientConfig{
			Policy: egress.Policy{
				AllowedHosts:   endpoints.hosts,
				RequireTLS:     endpoints.requireTLS,
				AllowPrivateIP: cfg.OTLPAllowPrivateIP,
				AllowedCIDRs:   cfg.OTLPAllowedCIDRs,
			},
			ControlHook: cfg.OTLPControlHook,
			Timeout:     expTimeout,
		})
		if gerr != nil {
			return nil, fmt.Errorf("telemetry: construct guarded OTLP HTTP client: %w", gerr)
		}
		otlpClient = guarded
	}
	if exporter == nil && endpoints != nil && endpoints.tracesURL != "" {
		exporter, err = otlptrace.New(context.Background(), &guardedOTLPClient{
			endpointURL: endpoints.tracesURL,
			httpClient:  otlpClient,
			stopCh:      make(chan struct{}),
		})
		if err != nil {
			return nil, fmt.Errorf("telemetry: initialize guarded OTLP HTTP exporter: %w", err)
		}
	}
	if endpoints != nil && endpoints.logsURL != "" && otlpClient != nil {
		logExporter = newRingLogExporter(
			endpoints.logsURL,
			otlpClient,
			metrics,
			svcName,
			cfg.ServiceVersion,
			cfg.MaxRingBufferBytes,
			expTimeout,
			cfg.FlushInterval,
		)
	}
	if endpoints != nil && endpoints.metricsURL != "" && otlpClient != nil {
		metExporter = newOTLPMetricsExporter(
			endpoints.metricsURL,
			otlpClient,
			metrics,
			svcName,
			cfg.ServiceVersion,
			expTimeout,
			cfg.MetricsExportInterval,
		)
	}

	ring := NewRingBufferSpanProcessor(RingBufferConfig{
		MaxBufferBytes: cfg.MaxRingBufferBytes,
		ExportTimeout:  cfg.ExportTimeout,
		FlushInterval:  cfg.FlushInterval,
		Exporter:       exporter,
		Metrics:        metrics,
		Redactor:       redactor,
	})

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(svcName),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(ring),
	)

	maxPersistBytes := cfg.MaxPersistQueueBytes
	if maxPersistBytes <= 0 || maxPersistBytes > DefaultMaxPersistQueueBytes {
		maxPersistBytes = DefaultMaxPersistQueueBytes
	}

	var dialect port.Dialect
	if cfg.DB != nil {
		dialect = port.DetectDialect(context.Background(), cfg.DB)
	}
	logger := privacy.NewJSONLogger(cfg.LogOut, redactor, nowFn)
	if logExporter != nil {
		logger.SetHook(logExporter.Enqueue)
	}
	p := &Pipeline{
		Metrics:         metrics,
		Redactor:        redactor,
		Logger:          logger,
		Ring:            ring,
		TracerProvider:  tp,
		Tracer:          tp.Tracer("github.com/nguywnben/tanpopo/dataplane"),
		logExporter:     logExporter,
		metricsExporter: metExporter,
		db:              cfg.DB,
		dialect:         dialect,
		vault:           cfg.Vault,
		nowMs:           nowMs,
		sessions:        make(map[string]DebugCaptureSession),
		persistMaxBytes: maxPersistBytes,
		persistMaxItems: DefaultMaxPersistQueueItems,
		persistNotify:   make(chan struct{}, 1),
		persistFlushCh:  make(chan chan struct{}, 16),
		persistStopCh:   make(chan struct{}),
	}
	if p.db != nil {
		p.persistWG.Add(1)
		go p.persistLoop()
	}
	return p, nil
}

func (p *Pipeline) persistLoop() {
	defer p.persistWG.Done()
	for {
		p.drainPersistQueue()
		select {
		case <-p.persistNotify:
		case ch := <-p.persistFlushCh:
			p.drainPersistQueue()
			close(ch)
		case <-p.persistStopCh:
			p.drainPersistQueue()
			for {
				select {
				case ch := <-p.persistFlushCh:
					close(ch)
				default:
					return
				}
			}
		}
	}
}

const maxPersistBatchSize = 64

func (p *Pipeline) drainPersistQueue() {
	batch := make([]persistTask, 0, maxPersistBatchSize)
	traces := make([]RequestTraceRecord, 0, maxPersistBatchSize)
	for {
		batch = batch[:0]
		traces = traces[:0]

		p.persistMu.Lock()
		if len(p.persistQueue) == 0 {
			p.persistMu.Unlock()
			return
		}
		n := len(p.persistQueue)
		if n > maxPersistBatchSize {
			n = maxPersistBatchSize
		}
		for i := 0; i < n; i++ {
			item := p.persistQueue[i]
			p.persistQueue[i] = persistTask{}
			p.persistBytes -= item.bytes
			batch = append(batch, item)
		}
		if n == len(p.persistQueue) {
			p.persistQueue = nil
		} else {
			p.persistQueue = p.persistQueue[n:]
		}
		if p.persistBytes < 0 {
			p.persistBytes = 0
		}
		p.persistMu.Unlock()

		for _, item := range batch {
			if item.trace != nil {
				traces = append(traces, *item.trace)
			}
		}
		if len(traces) > 0 {
			timeout := 2*time.Second + time.Duration(len(traces))*50*time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			if err := p.writeRequestTraceBatch(ctx, traces); err != nil && p.Metrics != nil {
				p.Metrics.RecordDropped("traces", int64(len(traces)))
			}
			cancel()
		}
		for _, item := range batch {
			if item.blob != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, _ = p.writeDebugBlobIfAuthorized(ctx, item.blob.orgID, item.blob.projectID, item.blob.virtualKeyID, item.blob.requestID, item.blob.payload, item.blob.preScrubbed)
				clear(item.blob.payload)
				cancel()
			}
		}
	}
}

func (p *Pipeline) enqueuePersistTask(task persistTask) {
	if p == nil || p.db == nil || p.closed.Load() {
		if task.blob != nil {
			clear(task.blob.payload)
		}
		return
	}
	var dropped int64
	p.persistMu.Lock()
	if p.closed.Load() {
		p.persistMu.Unlock()
		if task.blob != nil {
			clear(task.blob.payload)
		}
		return
	}
	for len(p.persistQueue) > 0 && (len(p.persistQueue) >= p.persistMaxItems || p.persistBytes+task.bytes > p.persistMaxBytes) {
		evicted := p.persistQueue[0]
		p.persistQueue[0] = persistTask{}
		p.persistQueue = p.persistQueue[1:]
		p.persistBytes -= evicted.bytes
		if evicted.blob != nil {
			clear(evicted.blob.payload)
		}
		dropped++
	}
	if task.bytes > p.persistMaxBytes {
		p.persistMu.Unlock()
		if task.blob != nil {
			clear(task.blob.payload)
		}
		if p.Metrics != nil {
			p.Metrics.RecordDropped("traces", dropped+1)
		}
		return
	}
	p.persistQueue = append(p.persistQueue, task)
	p.persistBytes += task.bytes
	p.persistMu.Unlock()

	if dropped > 0 && p.Metrics != nil {
		p.Metrics.RecordDropped("traces", dropped)
	}
	select {
	case p.persistNotify <- struct{}{}:
	default:
	}
}

// EnqueueRequestTrace enqueues a metadata-only waterfall trace onto the non-blocking
// bounded Drop-Oldest DB persistence queue so gateway response teardown never blocks on SQLite.
func (p *Pipeline) EnqueueRequestTrace(rec RequestTraceRecord) {
	if p == nil || p.db == nil || p.closed.Load() {
		return
	}
	if strings.TrimSpace(rec.OrgID) == "" || strings.TrimSpace(rec.RequestID) == "" {
		return
	}
	recCopy := rec
	if len(rec.WaterfallStages) > 0 {
		recCopy.WaterfallStages = append([]WaterfallStage(nil), rec.WaterfallStages...)
	}
	estBytes := int64(512 + len(recCopy.WaterfallStages)*128)
	p.enqueuePersistTask(persistTask{trace: &recCopy, bytes: estBytes})
}

// EnqueueDebugBlob checks for an active debug capture session and enqueues a bounded payload
// onto the non-blocking Drop-Oldest DB persistence queue without blocking request teardown.
func (p *Pipeline) EnqueueDebugBlob(orgID, projectID, virtualKeyID, requestID string, rawOrScrubbed []byte) {
	if p == nil || p.db == nil || p.vault == nil || p.closed.Load() || len(rawOrScrubbed) == 0 {
		return
	}
	if !p.HasActiveDebugSession(orgID, projectID, virtualKeyID) {
		return
	}
	maxPT := MaxDebugBlobCiphertextBytes - aesGCMTagBytes
	scrubbed := p.Redactor.SanitizeDebugPayload(rawOrScrubbed, maxPT)
	if len(scrubbed) == 0 {
		return
	}
	p.enqueuePersistTask(persistTask{
		blob: &debugBlobTask{
			orgID:        orgID,
			projectID:    projectID,
			virtualKeyID: virtualKeyID,
			requestID:    truncateStr(strings.TrimSpace(requestID), 40),
			payload:      scrubbed,
			preScrubbed:  true,
		},
		bytes: int64(256 + len(scrubbed)),
	})
}

// EnqueuePreScrubbedDebugBlob enqueues an already-sanitized debug payload bounded <= maxPT
// onto the non-blocking DB persistence queue, avoiding redundant JSON tokenization passes.
func (p *Pipeline) EnqueuePreScrubbedDebugBlob(orgID, projectID, virtualKeyID, requestID string, scrubbed []byte) {
	if p == nil || p.db == nil || p.vault == nil || p.closed.Load() || len(scrubbed) == 0 {
		return
	}
	if !p.HasActiveDebugSession(orgID, projectID, virtualKeyID) {
		return
	}
	maxPT := MaxDebugBlobCiphertextBytes - aesGCMTagBytes
	if len(scrubbed) > maxPT {
		scrubbed = scrubbed[:maxPT]
	}
	owned := make([]byte, len(scrubbed))
	copy(owned, scrubbed)
	p.enqueuePersistTask(persistTask{
		blob: &debugBlobTask{
			orgID:        orgID,
			projectID:    projectID,
			virtualKeyID: virtualKeyID,
			requestID:    truncateStr(strings.TrimSpace(requestID), 40),
			payload:      owned,
			preScrubbed:  true,
		},
		bytes: int64(256 + len(owned)),
	})
}

// ForceFlush flushes the OTLP span/log/metric exporters and queued trace/blob DB writes.
func (p *Pipeline) ForceFlush(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var flushErr error
	if p.Ring != nil {
		flushErr = p.Ring.ForceFlush(ctx)
	}
	if p.logExporter != nil {
		if lerr := p.logExporter.ForceFlush(ctx); lerr != nil && flushErr == nil {
			flushErr = lerr
		}
	}
	if p.metricsExporter != nil {
		if merr := p.metricsExporter.ForceFlush(ctx); merr != nil && flushErr == nil {
			flushErr = merr
		}
	}
	if p.db != nil && !p.closed.Load() {
		ch := make(chan struct{})
		select {
		case p.persistFlushCh <- ch:
			select {
			case <-ch:
			case <-ctx.Done():
				if flushErr == nil {
					flushErr = ctx.Err()
				}
			}
		case <-p.persistStopCh:
		case <-ctx.Done():
			if flushErr == nil {
				flushErr = ctx.Err()
			}
		}
	}
	return flushErr
}

// Shutdown drains/stops the async DB persistence worker, OTLP signal exporters, and metrics registry cleanly.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var retErr error
	p.shutdownOnce.Do(func() {
		p.closed.Store(true)
		close(p.persistStopCh)
		p.persistWG.Wait()
		if p.Logger != nil {
			p.Logger.SetHook(nil)
		}
		if p.TracerProvider != nil {
			retErr = p.TracerProvider.Shutdown(ctx)
		}
		if p.logExporter != nil {
			if lerr := p.logExporter.Shutdown(ctx); lerr != nil && retErr == nil {
				retErr = lerr
			}
		}
		if p.metricsExporter != nil {
			if merr := p.metricsExporter.Shutdown(ctx); merr != nil && retErr == nil {
				retErr = merr
			}
		}
		if p.Metrics != nil {
			if merr := p.Metrics.Shutdown(ctx); merr != nil && retErr == nil {
				retErr = merr
			}
		}
	})
	return retErr
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

// RevokeDebugCaptureSession terminates an active debug capture session immediately before its TTL expires.
func (p *Pipeline) RevokeDebugCaptureSession(sessionID string) bool {
	if p == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	id := strings.TrimSpace(sessionID)
	p.sessMu.Lock()
	defer p.sessMu.Unlock()
	if _, ok := p.sessions[id]; !ok {
		return false
	}
	delete(p.sessions, id)
	return true
}

// RevokeProjectDebugCaptureSessions terminates all active debug capture sessions for an org/project scope.
func (p *Pipeline) RevokeProjectDebugCaptureSessions(orgID, projectID string) int {
	if p == nil || strings.TrimSpace(orgID) == "" {
		return 0
	}
	o := strings.TrimSpace(orgID)
	proj := strings.TrimSpace(projectID)
	p.sessMu.Lock()
	defer p.sessMu.Unlock()
	removed := 0
	for id, sess := range p.sessions {
		if sess.OrgID == o && (proj == "" || sess.ProjectID == proj) {
			delete(p.sessions, id)
			removed++
		}
	}
	return removed
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
	return p.writeRequestTrace(ctx, rec)
}

const insertRequestTraceMetaSQL = `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id,
			entry_point, otel_trace_id, route_slug, selected_deployment_id, connection_id,
			extension_installation_id, extension_version_id, failover_attempts, http_status,
			canonical_error_code, proxy_overhead_us, ttft_ms, total_duration_ms, waterfall_stages_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(org_id, request_id, created_at_ms) DO NOTHING`

type preparedTraceRow struct {
	normOrg string
	args    [21]any
}

func (p *Pipeline) prepareRequestTraceRow(rec RequestTraceRecord) (preparedTraceRow, bool) {
	if strings.TrimSpace(rec.OrgID) == "" || strings.TrimSpace(rec.RequestID) == "" {
		return preparedTraceRow{}, false
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

	normOrg := truncateStr(rec.OrgID, 40)
	return preparedTraceRow{
		normOrg: normOrg,
		args: [21]any{
			truncateStr(rec.RequestID, 40),
			now,
			started,
			normOrg,
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
		},
	}, true
}

func (p *Pipeline) writeRequestTrace(ctx context.Context, rec RequestTraceRecord) error {
	if p == nil || p.db == nil {
		return nil
	}
	row, ok := p.prepareRequestTraceRow(rec)
	if !ok {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.execOrgScoped(ctx, row.normOrg, insertRequestTraceMetaSQL, row.args[:]...)
}

func (p *Pipeline) writeRequestTraceBatch(ctx context.Context, recs []RequestTraceRecord) error {
	if p == nil || p.db == nil || len(recs) == 0 {
		return nil
	}
	if len(recs) == 1 {
		return p.writeRequestTrace(ctx, recs[0])
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows := make([]preparedTraceRow, 0, len(recs))
	for _, rec := range recs {
		if row, ok := p.prepareRequestTraceRow(rec); ok {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) == 1 {
		return p.execOrgScoped(ctx, rows[0].normOrg, insertRequestTraceMetaSQL, rows[0].args[:]...)
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := port.Rebind(p.dialect, insertRequestTraceMetaSQL)
	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	lastOrg := ""
	for i := range rows {
		if p.dialect == port.DialectPostgres && rows[i].normOrg != lastOrg {
			if rlsErr := port.SetOrgRLSTx(ctx, tx, p.dialect, rows[i].normOrg); rlsErr != nil {
				return rlsErr
			}
			lastOrg = rows[i].normOrg
		}
		if _, execErr := stmt.ExecContext(ctx, rows[i].args[:]...); execErr != nil {
			return execErr
		}
	}
	if err := stmt.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

// StoreDebugBlobIfAuthorized persists an AEAD Project-DEK-sealed debug payload blob (capped
// strictly <= 256 KiB ciphertext) ONLY when an active, non-expired DebugCaptureSession covers
// (orgID, projectID, virtualKeyID). By default (no active session), 0 bytes are persisted.
func (p *Pipeline) StoreDebugBlobIfAuthorized(ctx context.Context, orgID, projectID, virtualKeyID, requestID string, rawPayload []byte) (bool, error) {
	if p == nil || p.db == nil || p.vault == nil || p.closed.Load() || len(rawPayload) == 0 {
		return false, nil
	}
	return p.writeDebugBlobIfAuthorized(ctx, orgID, projectID, virtualKeyID, requestID, rawPayload, false)
}

func (p *Pipeline) writeDebugBlobIfAuthorized(ctx context.Context, orgID, projectID, virtualKeyID, requestID string, rawPayload []byte, preScrubbed bool) (bool, error) {
	if p == nil || p.db == nil || p.vault == nil || len(rawPayload) == 0 {
		return false, nil
	}
	normOrgID := truncateStr(strings.TrimSpace(orgID), 40)
	normProjID := truncateStr(strings.TrimSpace(projectID), 40)
	normReqID := truncateStr(strings.TrimSpace(requestID), 40)
	if normOrgID == "" || normProjID == "" || normReqID == "" {
		return false, nil
	}
	now := p.nowMs()
	expMs, ok := p.activeSessionExpiry(normOrgID, normProjID, virtualKeyID, now)
	if !ok {
		return false, nil
	}
	maxPT := MaxDebugBlobCiphertextBytes - aesGCMTagBytes
	var scrubbed []byte
	if preScrubbed {
		if len(rawPayload) > maxPT {
			scrubbed = rawPayload[:maxPT]
		} else {
			scrubbed = rawPayload
		}
	} else {
		// Always scrub credentials (including non-prefixed JSON secret fields, Bearer, vk_tp_*, sk-*, cookies)
		// before encrypting the opt-in debug payload.
		scrubbed = p.Redactor.SanitizeDebugPayload(rawPayload, maxPT)
		defer clear(scrubbed)
	}

	nonce, ct, err := p.vault.SealProjectBlob(ctx, normOrgID, normProjID, vault.RecordKindDebugBlob, normReqID, scrubbed)
	if err != nil {
		return false, fmt.Errorf("telemetry: seal debug payload blob: %w", err)
	}
	if len(ct) > MaxDebugBlobCiphertextBytes {
		return false, fmt.Errorf("telemetry: encrypted debug payload blob exceeds 256 KiB limit")
	}

	err = p.execOrgScoped(ctx, normOrgID, `
		INSERT INTO debug_payload_blobs (
			request_id, org_id, project_id, aead_alg, nonce, encrypted_payload_aead, created_at_ms, expires_at_ms
		) VALUES (?, ?, ?, 'AES-256-GCM', ?, ?, ?, ?)
		ON CONFLICT(org_id, project_id, request_id) DO NOTHING`,
		normReqID,
		normOrgID,
		normProjID,
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

func (p *Pipeline) execOrgScoped(ctx context.Context, orgID, query string, args ...any) error {
	q := port.Rebind(p.dialect, query)
	if p.dialect != port.DialectPostgres {
		_, err := p.db.ExecContext(ctx, q, args...)
		return err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, p.dialect, orgID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return err
	}
	return tx.Commit()
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
