// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
)

type otlpSignalEndpoints struct {
	tracesURL  string
	metricsURL string
	logsURL    string
	hosts      []string
	requireTLS bool
}

func resolveOTLPSignalEndpoints(cfg PipelineConfig) (*otlpSignalEndpoints, error) {
	baseRaw := strings.TrimSpace(cfg.OTLPEndpoint)
	tracesRaw := strings.TrimSpace(cfg.OTLPTracesEndpoint)
	metricsRaw := strings.TrimSpace(cfg.OTLPMetricsEndpoint)
	logsRaw := strings.TrimSpace(cfg.OTLPLogsEndpoint)
	if baseRaw == "" && tracesRaw == "" && metricsRaw == "" && logsRaw == "" {
		return nil, nil
	}

	derive := func(base, signalPath string) (string, error) {
		parsed, err := url.Parse(strings.TrimSpace(base))
		if err != nil {
			return "", fmt.Errorf("telemetry: invalid OTLP endpoint %q: %w", base, err)
		}
		scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
		if (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return "", fmt.Errorf("telemetry: invalid OTLP endpoint %q", base)
		}
		p := strings.TrimRight(parsed.Path, "/")
		switch {
		case strings.HasSuffix(p, "/v1/traces"):
			p = strings.TrimSuffix(p, "/v1/traces") + signalPath
		case strings.HasSuffix(p, "/v1/metrics"):
			p = strings.TrimSuffix(p, "/v1/metrics") + signalPath
		case strings.HasSuffix(p, "/v1/logs"):
			p = strings.TrimSuffix(p, "/v1/logs") + signalPath
		case p == "":
			p = signalPath
		default:
			p = p + signalPath
		}
		cp := *parsed
		cp.Path = p
		return cp.String(), nil
	}

	out := &otlpSignalEndpoints{requireTLS: true}
	if tracesRaw != "" {
		out.tracesURL = tracesRaw
	} else if baseRaw != "" {
		u, err := url.Parse(baseRaw)
		if err != nil {
			return nil, fmt.Errorf("telemetry: invalid OTLP endpoint %q: %w", baseRaw, err)
		}
		scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
		if (scheme != "http" && scheme != "https") || u.Host == "" || u.User != nil {
			return nil, fmt.Errorf("telemetry: invalid OTLP endpoint %q", baseRaw)
		}
		if strings.TrimSpace(u.Path) == "" || u.Path == "/" {
			cp := *u
			cp.Path = "/v1/traces"
			out.tracesURL = cp.String()
		} else {
			out.tracesURL = baseRaw
		}
	}

	if metricsRaw != "" {
		out.metricsURL = metricsRaw
	} else if baseRaw != "" {
		var err error
		if out.metricsURL, err = derive(baseRaw, "/v1/metrics"); err != nil {
			return nil, err
		}
	}
	if logsRaw != "" {
		out.logsURL = logsRaw
	} else if baseRaw != "" {
		var err error
		if out.logsURL, err = derive(baseRaw, "/v1/logs"); err != nil {
			return nil, err
		}
	}

	seenHosts := map[string]bool{}
	for _, raw := range []string{out.tracesURL, out.metricsURL, out.logsURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("telemetry: invalid OTLP signal endpoint %q: %w", raw, err)
		}
		scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
		if (scheme != "http" && scheme != "https") || u.Host == "" || u.User != nil {
			return nil, fmt.Errorf("telemetry: invalid OTLP signal endpoint %q", raw)
		}
		if scheme != "https" {
			out.requireTLS = false
		}
		if !seenHosts[u.Host] {
			seenHosts[u.Host] = true
			out.hosts = append(out.hosts, u.Host)
		}
	}
	return out, nil
}

func postOTLPProtobuf(ctx context.Context, client *http.Client, endpointURL string, msg proto.Message) error {
	if client == nil || strings.TrimSpace(endpointURL) == "" {
		return nil
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry: OTLP collector %s returned HTTP %d", endpointURL, resp.StatusCode)
	}
	return nil
}

type queuedLogItem struct {
	rec  *logspb.LogRecord
	size int64
}

// ringLogExporter provides non-blocking bounded Drop-Oldest OTLP Logs export.
type ringLogExporter struct {
	endpointURL   string
	httpClient    *http.Client
	metrics       *MetricsRegistry
	resourceAttrs []*commonpb.KeyValue
	maxBytes      int64
	exportTimeout time.Duration
	flushInterval time.Duration

	mu      sync.Mutex
	queue   []queuedLogItem
	bytes   int64
	closed  atomic.Bool
	notify  chan struct{}
	flushCh chan chan struct{}
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func newRingLogExporter(
	endpointURL string,
	client *http.Client,
	metrics *MetricsRegistry,
	svcName, svcVersion string,
	maxBytes int64,
	exportTimeout, flushInterval time.Duration,
) *ringLogExporter {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRingBufferBytes
	}
	if exportTimeout <= 0 {
		exportTimeout = DefaultExportTimeout
	}
	if flushInterval <= 0 {
		flushInterval = DefaultFlushInterval
	}
	resAttrs := []*commonpb.KeyValue{
		{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: svcName}}},
	}
	if strings.TrimSpace(svcVersion) != "" {
		resAttrs = append(resAttrs, &commonpb.KeyValue{
			Key: "service.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: svcVersion}},
		})
	}
	e := &ringLogExporter{
		endpointURL:   endpointURL,
		httpClient:    client,
		metrics:       metrics,
		resourceAttrs: resAttrs,
		maxBytes:      maxBytes,
		exportTimeout: exportTimeout,
		flushInterval: flushInterval,
		notify:        make(chan struct{}, 1),
		flushCh:       make(chan chan struct{}, 16),
		stopCh:        make(chan struct{}),
	}
	e.wg.Add(1)
	go e.run()
	return e
}

func (e *ringLogExporter) Enqueue(sanitizedLine []byte, payload map[string]any) {
	if e == nil || e.closed.Load() {
		return
	}
	tsNano := uint64(time.Now().UTC().UnixNano())
	if rawTS, ok := payload["ts"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTS); err == nil {
			tsNano = uint64(parsed.UTC().UnixNano())
		}
	}
	level, _ := payload["level"].(string)
	sevNum, sevText := otlpSeverity(level)
	keys := make([]string, 0, len(payload))
	for k := range payload {
		if k == "ts" || k == "level" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := make([]*commonpb.KeyValue, 0, len(keys))
	for _, k := range keys {
		attrs = append(attrs, &commonpb.KeyValue{
			Key:   k,
			Value: anyToOTLPValue(payload[k]),
		})
	}
	rec := &logspb.LogRecord{
		TimeUnixNano:         tsNano,
		ObservedTimeUnixNano: tsNano,
		SeverityNumber:       sevNum,
		SeverityText:         sevText,
		Body:                 &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: string(sanitizedLine)}},
		Attributes:           attrs,
	}
	itemSize := int64(len(sanitizedLine) + len(attrs)*48 + 128)
	if itemSize > e.maxBytes {
		if e.metrics != nil {
			e.metrics.RecordDropped("logs", 1)
		}
		return
	}

	var dropped int64
	e.mu.Lock()
	if e.closed.Load() {
		e.mu.Unlock()
		return
	}
	for len(e.queue) > 0 && e.bytes+itemSize > e.maxBytes {
		e.bytes -= e.queue[0].size
		e.queue[0] = queuedLogItem{}
		e.queue = e.queue[1:]
		dropped++
	}
	e.queue = append(e.queue, queuedLogItem{rec: rec, size: itemSize})
	e.bytes += itemSize
	e.mu.Unlock()

	if dropped > 0 && e.metrics != nil {
		e.metrics.RecordDropped("logs", dropped)
	}
	select {
	case e.notify <- struct{}{}:
	default:
	}
}

func (e *ringLogExporter) ForceFlush(ctx context.Context) error {
	if e == nil || e.closed.Load() {
		return nil
	}
	ch := make(chan struct{})
	select {
	case e.flushCh <- ch:
	case <-e.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ch:
		return nil
	case <-e.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *ringLogExporter) Shutdown(ctx context.Context) error {
	if e == nil || !e.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(e.stopCh)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *ringLogExporter) run() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.notify:
			e.drain(true)
		case <-ticker.C:
			e.drain(false)
		case ch := <-e.flushCh:
			e.drainAll()
			close(ch)
		case <-e.stopCh:
			e.drainAll()
			for {
				select {
				case ch := <-e.flushCh:
					close(ch)
				default:
					return
				}
			}
		}
	}
}

func (e *ringLogExporter) drain(onlyFullBatch bool) {
	for {
		e.mu.Lock()
		if len(e.queue) == 0 || (onlyFullBatch && len(e.queue) < DefaultExportBatchSize) {
			e.mu.Unlock()
			return
		}
		n := min(len(e.queue), DefaultExportBatchSize)
		batch := make([]*logspb.LogRecord, 0, n)
		for i := 0; i < n; i++ {
			batch = append(batch, e.queue[i].rec)
			e.bytes -= e.queue[i].size
			e.queue[i] = queuedLogItem{}
		}
		if n == len(e.queue) {
			e.queue = nil
		} else {
			e.queue = e.queue[n:]
		}
		if e.bytes < 0 {
			e.bytes = 0
		}
		e.mu.Unlock()
		e.exportBatch(batch)
	}
}

func (e *ringLogExporter) drainAll() {
	e.drain(false)
}

func (e *ringLogExporter) exportBatch(records []*logspb.LogRecord) {
	if len(records) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), e.exportTimeout)
	defer cancel()
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{Attributes: e.resourceAttrs},
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope:      &commonpb.InstrumentationScope{Name: "github.com/nguywnben/tanpopo/logs"},
				LogRecords: records,
			}},
		}},
	}
	if err := postOTLPProtobuf(ctx, e.httpClient, e.endpointURL, req); err != nil && e.metrics != nil {
		e.metrics.RecordDropped("logs", int64(len(records)))
	}
}

func otlpSeverity(level string) (logspb.SeverityNumber, string) {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case privacy.LevelDebug:
		return logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG, "DEBUG"
	case privacy.LevelWarn:
		return logspb.SeverityNumber_SEVERITY_NUMBER_WARN, "WARN"
	case privacy.LevelError:
		return logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, "ERROR"
	default:
		return logspb.SeverityNumber_SEVERITY_NUMBER_INFO, "INFO"
	}
}

func anyToOTLPValue(v any) *commonpb.AnyValue {
	switch val := v.(type) {
	case string:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: val}}
	case bool:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: val}}
	case int:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: int64(val)}}
	case int64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: val}}
	case float64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: val}}
	case json.Number:
		if iv, err := val.Int64(); err == nil {
			return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: iv}}
		}
		if fv, err := val.Float64(); err == nil {
			return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: fv}}
		}
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: val.String()}}
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: fmt.Sprintf("%v", val)}}
		}
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: string(b)}}
	}
}

type otlpMetricsExporter struct {
	endpointURL   string
	httpClient    *http.Client
	metrics       *MetricsRegistry
	resourceAttrs []*commonpb.KeyValue
	exportTimeout time.Duration
	interval      time.Duration

	closed  atomic.Bool
	flushCh chan chan error
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func newOTLPMetricsExporter(
	endpointURL string,
	client *http.Client,
	metrics *MetricsRegistry,
	svcName, svcVersion string,
	exportTimeout, interval time.Duration,
) *otlpMetricsExporter {
	if exportTimeout <= 0 {
		exportTimeout = DefaultExportTimeout
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	resAttrs := []*commonpb.KeyValue{
		{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: svcName}}},
	}
	if strings.TrimSpace(svcVersion) != "" {
		resAttrs = append(resAttrs, &commonpb.KeyValue{
			Key: "service.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: svcVersion}},
		})
	}
	e := &otlpMetricsExporter{
		endpointURL:   endpointURL,
		httpClient:    client,
		metrics:       metrics,
		resourceAttrs: resAttrs,
		exportTimeout: exportTimeout,
		interval:      interval,
		flushCh:       make(chan chan error, 16),
		stopCh:        make(chan struct{}),
	}
	e.wg.Add(1)
	go e.run()
	return e
}

func (e *otlpMetricsExporter) run() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), e.exportTimeout)
			_ = e.exportOnce(ctx)
			cancel()
		case ch := <-e.flushCh:
			ctx, cancel := context.WithTimeout(context.Background(), e.exportTimeout)
			ch <- e.exportOnce(ctx)
			cancel()
		case <-e.stopCh:
			ctx, cancel := context.WithTimeout(context.Background(), e.exportTimeout)
			_ = e.exportOnce(ctx)
			cancel()
			for {
				select {
				case ch := <-e.flushCh:
					ch <- errors.New("telemetry: metrics exporter stopped")
				default:
					return
				}
			}
		}
	}
}

func (e *otlpMetricsExporter) ForceFlush(ctx context.Context) error {
	if e == nil || e.closed.Load() {
		return nil
	}
	ch := make(chan error, 1)
	select {
	case e.flushCh <- ch:
	case <-e.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-ch:
		return err
	case <-e.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *otlpMetricsExporter) Shutdown(ctx context.Context) error {
	if e == nil || !e.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(e.stopCh)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *otlpMetricsExporter) exportOnce(ctx context.Context) error {
	if e.metrics == nil {
		return nil
	}
	rm, err := e.metrics.CollectMetrics(ctx)
	if err != nil {
		e.metrics.RecordDropped("metrics", 1)
		return err
	}
	scopeMetrics := toOTLPScopeMetrics(rm)
	if len(scopeMetrics) == 0 {
		return nil
	}
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource:     &resourcepb.Resource{Attributes: e.resourceAttrs},
			ScopeMetrics: scopeMetrics,
		}},
	}
	if err := postOTLPProtobuf(ctx, e.httpClient, e.endpointURL, req); err != nil {
		e.metrics.RecordDropped("metrics", 1)
		return err
	}
	return nil
}

func toOTLPScopeMetrics(rm metricdata.ResourceMetrics) []*metricspb.ScopeMetrics {
	var out []*metricspb.ScopeMetrics
	for _, sm := range rm.ScopeMetrics {
		var pbMetrics []*metricspb.Metric
		for _, m := range sm.Metrics {
			if pbm := toOTLPMetric(m); pbm != nil {
				pbMetrics = append(pbMetrics, pbm)
			}
		}
		if len(pbMetrics) == 0 {
			continue
		}
		out = append(out, &metricspb.ScopeMetrics{
			Scope:   &commonpb.InstrumentationScope{Name: sm.Scope.Name, Version: sm.Scope.Version},
			Metrics: pbMetrics,
		})
	}
	return out
}

func toOTLPMetric(m metricdata.Metrics) *metricspb.Metric {
	out := &metricspb.Metric{
		Name:        m.Name,
		Description: m.Description,
		Unit:        m.Unit,
	}
	switch data := m.Data.(type) {
	case metricdata.Sum[int64]:
		pts := make([]*metricspb.NumberDataPoint, 0, len(data.DataPoints))
		for _, dp := range data.DataPoints {
			pts = append(pts, &metricspb.NumberDataPoint{
				Attributes:        toOTLPAttrs(dp.Attributes),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsInt{AsInt: dp.Value},
			})
		}
		if len(pts) == 0 {
			return nil
		}
		out.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			DataPoints:             pts,
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
			IsMonotonic:            data.IsMonotonic,
		}}
	case metricdata.Sum[float64]:
		pts := make([]*metricspb.NumberDataPoint, 0, len(data.DataPoints))
		for _, dp := range data.DataPoints {
			pts = append(pts, &metricspb.NumberDataPoint{
				Attributes:        toOTLPAttrs(dp.Attributes),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: dp.Value},
			})
		}
		if len(pts) == 0 {
			return nil
		}
		out.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			DataPoints:             pts,
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
			IsMonotonic:            data.IsMonotonic,
		}}
	case metricdata.Gauge[int64]:
		pts := make([]*metricspb.NumberDataPoint, 0, len(data.DataPoints))
		for _, dp := range data.DataPoints {
			pts = append(pts, &metricspb.NumberDataPoint{
				Attributes:        toOTLPAttrs(dp.Attributes),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsInt{AsInt: dp.Value},
			})
		}
		if len(pts) == 0 {
			return nil
		}
		out.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: pts}}
	case metricdata.Gauge[float64]:
		pts := make([]*metricspb.NumberDataPoint, 0, len(data.DataPoints))
		for _, dp := range data.DataPoints {
			pts = append(pts, &metricspb.NumberDataPoint{
				Attributes:        toOTLPAttrs(dp.Attributes),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: dp.Value},
			})
		}
		if len(pts) == 0 {
			return nil
		}
		out.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: pts}}
	case metricdata.Histogram[float64]:
		pts := make([]*metricspb.HistogramDataPoint, 0, len(data.DataPoints))
		for _, dp := range data.DataPoints {
			pts = append(pts, &metricspb.HistogramDataPoint{
				Attributes:        toOTLPAttrs(dp.Attributes),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Count:             dp.Count,
				Sum:               protoFloat64(dp.Sum),
				BucketCounts:      append([]uint64(nil), dp.BucketCounts...),
				ExplicitBounds:    append([]float64(nil), dp.Bounds...),
			})
		}
		if len(pts) == 0 {
			return nil
		}
		out.Data = &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
			DataPoints:             pts,
			AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		}}
	default:
		return nil
	}
	return out
}

func protoFloat64(v float64) *float64 { return &v }

func toOTLPAttrs(set attribute.Set) []*commonpb.KeyValue {
	it := set.Iter()
	var out []*commonpb.KeyValue
	for it.Next() {
		kv := it.Attribute()
		val := &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: kv.Value.Emit()}}
		switch kv.Value.Type() {
		case attribute.INT64:
			val = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: kv.Value.AsInt64()}}
		case attribute.FLOAT64:
			val = &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: kv.Value.AsFloat64()}}
		case attribute.BOOL:
			val = &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: kv.Value.AsBool()}}
		}
		out = append(out, &commonpb.KeyValue{Key: string(kv.Key), Value: val})
	}
	return out
}
