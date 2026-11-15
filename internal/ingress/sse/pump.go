// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package sse implements Tanpopo's low-overhead, single-writer Server-Sent Events
// (SSE) streaming pump (TP-IMP-0015, SLO-03 < 1 ms/chunk added overhead, CT-10, CT-11,
// FZ-03, FMEA-01).
//
// Invariants:
//   - Exactly one reader goroutine pulls bounded reads via sync.Pool into a channel;
//     only the owning handler goroutine writes/flushes http.ResponseWriter.
//   - Downstream disconnects (ctx.Done) immediately cancel the upstream context and
//     close the upstream io.ReadCloser so FakeLLM / upstream sockets close in < 500 ms
//     with 0 leaked goroutines (verified by go.uber.org/goleak).
//   - Pre-first-byte upstream failures return (PumpResult{StreamStarted: false}, err)
//     for canonical pre-stream JSON error mapping. Once the first SSE frame is flushed
//     (streamStarted == true), retries/failovers are 100% locked out and any upstream
//     drop emits exactly one canonical event: error (mid_stream_abort) — never a
//     synthetic data: [DONE].
//   - SSE lines are bounded strictly to MaxLineBytes (2 MiB default) prior to full buffering.
package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/port"
)

const (
	// DefaultChunkBytes sizes pooled upstream read slices for low-latency SSE pumping.
	DefaultChunkBytes = 32 * 1024
	// DefaultMaxLineBytes enforces the 2 MiB per-SSE-line ceiling (NEG-2 / FZ-03).
	DefaultMaxLineBytes = 2 * 1024 * 1024
	// DefaultHeartbeatInterval sets the SSE `: ping\n\n` keepalive period once streaming starts.
	DefaultHeartbeatInterval = 15 * time.Second
)

var (
	// ErrLineTooLarge indicates an upstream SSE line exceeded MaxLineBytes without a newline.
	ErrLineTooLarge = errors.New("sse: upstream line exceeds max line byte limit")
	// ErrMidStreamAbort indicates the upstream dropped or errored after streamStarted latched.
	ErrMidStreamAbort = errors.New("sse: upstream stream aborted mid-stream")
	// ErrInvalidPumpConfig indicates missing required PumpConfig dependencies.
	ErrInvalidPumpConfig = errors.New("sse: invalid pump configuration")
	// ErrIdleChunkTimeout indicates no upstream SSE chunk arrived before IdleChunkTimeout elapsed.
	ErrIdleChunkTimeout = errors.New("sse: upstream chunk idle timeout exceeded")
)

var readBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, DefaultChunkBytes)
		return &b
	},
}

// PumpConfig configures a single SSE relay session.
type PumpConfig struct {
	RequestID           string
	Upstream            io.ReadCloser
	CancelUpstream      context.CancelFunc
	Runtime             port.ProviderRuntime
	Writer              http.ResponseWriter
	MaxLineBytes        int
	HeartbeatInterval   time.Duration
	IdleChunkTimeout    time.Duration
	RequireDoneSentinel bool
	ElideUsageChunk     bool
}

// PumpResult summarizes terminal telemetry from a Pump execution.
type PumpResult struct {
	StreamStarted      bool
	FramesEmitted      int
	SawDone            bool
	ClientDisconnected bool
	Usage              *port.NormalizedUsage
}

type readMsg struct {
	data  []byte
	isEOF bool
	err   error
}

// ScanCompleteSSELines splits complete '\n'-terminated lines from (partial + incoming)
// while enforcing maxLineBytes per line prior to allocating large buffers (FZ-03).
// When incoming is nil and atEOF is true, any buffered partial line is validated and
// returned as complete.
func ScanCompleteSSELines(partial, incoming []byte, maxLineBytes int, atEOF bool) (complete []byte, nextPartial []byte, err error) {
	if maxLineBytes <= 0 {
		maxLineBytes = DefaultMaxLineBytes
	}
	if len(incoming) > 0 {
		// Inspect every '\n'-delimited line across partial + incoming without combining first
		// so unterminated >2 MiB lines fail immediately without extra copying.
		curLen := len(partial)
		lastNL := -1
		pos := 0
		for pos < len(incoming) {
			idx := bytes.IndexByte(incoming[pos:], '\n')
			if idx < 0 {
				curLen += len(incoming) - pos
				if curLen > maxLineBytes {
					clear(partial)
					return nil, nil, ErrLineTooLarge
				}
				break
			}
			lineLen := curLen + idx
			if idx > 0 && incoming[pos+idx-1] == '\r' {
				lineLen--
			} else if idx == 0 && curLen > 0 && len(partial) > 0 && partial[len(partial)-1] == '\r' {
				lineLen--
			}
			if lineLen > maxLineBytes {
				clear(partial)
				return nil, nil, ErrLineTooLarge
			}
			curLen = 0
			lastNL = pos + idx
			pos = lastNL + 1
		}

		if lastNL >= 0 {
			emitLen := len(partial) + lastNL + 1
			complete = make([]byte, emitLen)
			n := copy(complete, partial)
			copy(complete[n:], incoming[:lastNL+1])
			clear(partial)
			if lastNL+1 < len(incoming) {
				rem := incoming[lastNL+1:]
				nextPartial = make([]byte, len(rem))
				copy(nextPartial, rem)
			}
		} else {
			if cap(partial) >= len(partial)+len(incoming) {
				nextPartial = append(partial, incoming...)
			} else {
				nextPartial = make([]byte, len(partial)+len(incoming))
				n := copy(nextPartial, partial)
				copy(nextPartial[n:], incoming)
				clear(partial)
			}
		}
	} else {
		nextPartial = partial
	}

	if atEOF && len(nextPartial) > 0 {
		if len(nextPartial) > maxLineBytes {
			clear(nextPartial)
			return complete, nil, ErrLineTooLarge
		}
		if len(complete) == 0 {
			complete = nextPartial
		} else {
			merged := make([]byte, len(complete)+len(nextPartial))
			n := copy(merged, complete)
			copy(merged[n:], nextPartial)
			clear(nextPartial)
			complete = merged
		}
		nextPartial = nil
	}
	return complete, nextPartial, nil
}

const maxLinesPerTransformCall = 64

func emitLineBoundedMsgs(complete []byte, isEOF bool, msgCh chan<- readMsg, stopReader <-chan struct{}) bool {
	if len(complete) == 0 {
		select {
		case msgCh <- readMsg{isEOF: isEOF}:
			return true
		case <-stopReader:
			return false
		}
	}
	pos := 0
	for pos < len(complete) {
		lines := 0
		splitAt := pos
		for splitAt < len(complete) {
			nl := bytes.IndexByte(complete[splitAt:], '\n')
			if nl < 0 {
				splitAt = len(complete)
				break
			}
			splitAt += nl + 1
			lines++
			if lines >= maxLinesPerTransformCall {
				break
			}
		}
		lastBatch := splitAt >= len(complete)
		select {
		case msgCh <- readMsg{data: complete[pos:splitAt], isEOF: isEOF && lastBatch}:
		case <-stopReader:
			return false
		}
		pos = splitAt
	}
	return true
}

// Pump relays upstream SSE chunks through cfg.Runtime.TransformStreamChunk to cfg.Writer.
func Pump(ctx context.Context, cfg PumpConfig) (PumpResult, error) {
	var res PumpResult
	if cfg.Upstream == nil || cfg.Runtime == nil || cfg.Writer == nil {
		return res, ErrInvalidPumpConfig
	}
	maxLineBytes := cfg.MaxLineBytes
	if maxLineBytes <= 0 {
		maxLineBytes = DefaultMaxLineBytes
	}
	hbInterval := cfg.HeartbeatInterval
	if hbInterval == 0 {
		hbInterval = DefaultHeartbeatInterval
	}

	rc := http.NewResponseController(cfg.Writer)

	msgCh := make(chan readMsg, 4)
	readerDone := make(chan struct{})
	stopReader := make(chan struct{})

	go func() {
		defer close(readerDone)
		defer close(msgCh)
		bufPtr := readBufPool.Get().(*[]byte)
		buf := *bufPtr

		var partial []byte
		defer func() {
			clear(partial)
			clear(buf)
			readBufPool.Put(bufPtr)
		}()

		for {
			n, rerr := cfg.Upstream.Read(buf)
			if n > 0 {
				var complete []byte
				var scanErr error
				isEOF := errors.Is(rerr, io.EOF)
				complete, partial, scanErr = ScanCompleteSSELines(partial, buf[:n], maxLineBytes, isEOF)
				if scanErr != nil {
					select {
					case msgCh <- readMsg{err: scanErr}:
					case <-stopReader:
					}
					return
				}
				if len(complete) > 0 {
					if !emitLineBoundedMsgs(complete, isEOF, msgCh, stopReader) {
						return
					}
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					if len(partial) > 0 {
						finalSlice := make([]byte, len(partial))
						copy(finalSlice, partial)
						clear(partial)
						partial = nil
						if !emitLineBoundedMsgs(finalSlice, true, msgCh, stopReader) {
							return
						}
					} else if n == 0 {
						if !emitLineBoundedMsgs(nil, true, msgCh, stopReader) {
							return
						}
					}
					return
				}
				select {
				case msgCh <- readMsg{err: rerr}:
				case <-stopReader:
				}
				return
			}
		}
	}()

	abortUpstream := func() {
		if cfg.CancelUpstream != nil {
			cfg.CancelUpstream()
		}
		_ = cfg.Upstream.Close()
		close(stopReader)
		<-readerDone
	}

	var hbTicker *time.Ticker
	var hbCh <-chan time.Time
	startHeartbeat := func() {
		if hbInterval > 0 && hbTicker == nil {
			hbTicker = time.NewTicker(hbInterval)
			hbCh = hbTicker.C
		}
	}
	defer func() {
		if hbTicker != nil {
			hbTicker.Stop()
		}
	}()

	var idleTimer *time.Timer
	var idleCh <-chan time.Time
	if cfg.IdleChunkTimeout > 0 {
		idleTimer = time.NewTimer(cfg.IdleChunkTimeout)
		idleCh = idleTimer.C
		defer idleTimer.Stop()
	}
	resetIdleTimer := func() {
		if idleTimer == nil {
			return
		}
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(cfg.IdleChunkTimeout)
	}

	var sessionState []byte
	var upstreamErr error

pumpLoop:
	for {
		select {
		case <-ctx.Done():
			res.ClientDisconnected = true
			abortUpstream()
			return res, ctx.Err()

		case <-idleCh:
			abortUpstream()
			if !res.StreamStarted {
				return res, ErrIdleChunkTimeout
			}
			writeMidStreamAbort(cfg.Writer, rc, cfg.RequestID, "upstream SSE stream idle timeout")
			return res, fmt.Errorf("%w: %w", ErrMidStreamAbort, ErrIdleChunkTimeout)

		case <-hbCh:
			if res.StreamStarted {
				if _, werr := cfg.Writer.Write([]byte(": ping\n\n")); werr != nil {
					res.ClientDisconnected = true
					abortUpstream()
					return res, werr
				}
				if ferr := rc.Flush(); ferr != nil {
					res.ClientDisconnected = true
					abortUpstream()
					return res, ferr
				}
			}

		case msg, ok := <-msgCh:
			if !ok {
				break pumpLoop
			}
			resetIdleTimer()
			if msg.err != nil {
				upstreamErr = msg.err
				break pumpLoop
			}
			if len(msg.data) > 0 || msg.isEOF {
				out, terr := cfg.Runtime.TransformStreamChunk(ctx, port.StreamChunkInput{
					RequestID:        cfg.RequestID,
					SessionState:     sessionState,
					RawUpstreamChunk: msg.data,
					IsEOF:            msg.isEOF,
				})
				if terr != nil {
					if ctx.Err() != nil {
						res.ClientDisconnected = true
						abortUpstream()
						return res, ctx.Err()
					}
					upstreamErr = terr
					break pumpLoop
				}
				sessionState = out.NextSessionState
				if out.IncrementalUsage != nil {
					res.Usage = mergeUsage(res.Usage, out.IncrementalUsage)
				}
				for _, frame := range out.NormalizedSSEFrames {
					if len(frame) == 0 {
						continue
					}
					if cfg.ElideUsageChunk {
						var skip bool
						frame, skip = elideUsageFromSSEFrame(frame)
						if skip || len(frame) == 0 {
							continue
						}
					}
					if isDoneSSEFrame(frame) {
						res.SawDone = true
					}
					if !res.StreamStarted {
						writeSSEHeaders(cfg.Writer)
						res.StreamStarted = true
						startHeartbeat()
					}
					normalized := ensureSSEFrameTerminator(frame)
					if _, werr := cfg.Writer.Write(normalized); werr != nil {
						res.ClientDisconnected = true
						abortUpstream()
						return res, werr
					}
					res.FramesEmitted++
				}
				if out.StreamCompleted {
					res.SawDone = true
				}
				if res.StreamStarted {
					if ferr := rc.Flush(); ferr != nil {
						res.ClientDisconnected = true
						abortUpstream()
						return res, ferr
					}
				}
			}
			if msg.isEOF {
				break pumpLoop
			}
		}
	}

	// Check downstream cancellation before calling CancelUpstream so upstream
	// teardown cannot falsely trigger ClientDisconnected if a caller shares cancel functions.
	downstreamCanceled := ctx.Err() != nil

	// Cleanly shut down reader goroutine on normal or error exit.
	if cfg.CancelUpstream != nil {
		cfg.CancelUpstream()
	}
	_ = cfg.Upstream.Close()
	close(stopReader)
	<-readerDone

	if downstreamCanceled {
		res.ClientDisconnected = true
		return res, ctx.Err()
	}

	if upstreamErr != nil {
		if !res.StreamStarted {
			return res, upstreamErr
		}
		writeMidStreamAbort(cfg.Writer, rc, cfg.RequestID, "upstream stream interrupted mid-stream")
		return res, fmt.Errorf("%w: %w", ErrMidStreamAbort, upstreamErr)
	}

	if ctx.Err() != nil {
		res.ClientDisconnected = true
		return res, ctx.Err()
	}

	// Guard FMEA-01: if EOF occurred without a completion sentinel (or with trailing
	// incomplete buffered state), treat as mid-stream abort rather than faking [DONE].
	if len(sessionState) > 0 || (cfg.RequireDoneSentinel && !res.SawDone) {
		if !res.StreamStarted {
			return res, ErrMidStreamAbort
		}
		writeMidStreamAbort(cfg.Writer, rc, cfg.RequestID, "upstream closed unexpectedly before stream completion")
		return res, ErrMidStreamAbort
	}

	return res, nil
}

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

func writeMidStreamAbort(w http.ResponseWriter, rc *http.ResponseController, requestID, message string) {
	envelope := map[string]any{
		"error": map[string]any{
			"code":       "mid_stream_abort",
			"type":       "upstream_error",
			"message":    message,
			"request_id": requestID,
			"retryable":  false,
		},
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		payload = []byte(`{"error":{"code":"mid_stream_abort","type":"upstream_error","message":"upstream stream interrupted","retryable":false}}`)
	}
	_, _ = w.Write([]byte("event: error\ndata: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	_ = rc.Flush()
}

func isDoneSSEFrame(frame []byte) bool {
	for len(frame) > 0 {
		idx := bytes.IndexByte(frame, '\n')
		var line []byte
		if idx < 0 {
			line = frame
			frame = nil
		} else {
			line = frame[:idx]
			frame = frame[idx+1:]
		}
		trimmed := bytes.TrimSpace(line)
		if bytes.Equal(trimmed, []byte("data: [DONE]")) || bytes.Equal(trimmed, []byte("data:[DONE]")) {
			return true
		}
	}
	return false
}

func ensureSSEFrameTerminator(frame []byte) []byte {
	if bytes.HasSuffix(frame, []byte("\n\n")) {
		return frame
	}
	if bytes.HasSuffix(frame, []byte("\n")) {
		out := make([]byte, len(frame)+1)
		copy(out, frame)
		out[len(frame)] = '\n'
		return out
	}
	out := make([]byte, len(frame)+2)
	copy(out, frame)
	out[len(frame)] = '\n'
	out[len(frame)+1] = '\n'
	return out
}

func mergeUsage(prev, next *port.NormalizedUsage) *port.NormalizedUsage {
	if next == nil {
		return prev
	}
	if prev == nil {
		cp := *next
		if next.MediaUsage != nil {
			m := *next.MediaUsage
			cp.MediaUsage = &m
		}
		return &cp
	}
	if prev.Accuracy == port.UsageAccuracyExact && next.Accuracy != port.UsageAccuracyExact {
		return prev
	}
	if prev.Accuracy == port.UsageAccuracyExact && next.Accuracy == port.UsageAccuracyExact {
		cp := *prev
		if next.InputTokens > 0 {
			cp.InputTokens = next.InputTokens
		}
		if next.OutputTokens > 0 {
			cp.OutputTokens = next.OutputTokens
		}
		if next.CacheReadTokens > 0 {
			cp.CacheReadTokens = next.CacheReadTokens
		}
		if next.CacheWriteTokens > 0 {
			cp.CacheWriteTokens = next.CacheWriteTokens
		}
		if next.ReasoningTokens > 0 {
			cp.ReasoningTokens = next.ReasoningTokens
		}
		if next.MediaUsage != nil {
			m := *next.MediaUsage
			cp.MediaUsage = &m
		}
		return &cp
	}
	cp := *next
	if next.MediaUsage != nil {
		m := *next.MediaUsage
		cp.MediaUsage = &m
	}
	return &cp
}

func elideUsageFromSSEFrame(frame []byte) ([]byte, bool) {
	if isDoneSSEFrame(frame) {
		return frame, false
	}
	var prefixLines [][]byte
	var dataLine []byte
	rem := frame
	for len(rem) > 0 {
		idx := bytes.IndexByte(rem, '\n')
		var line []byte
		if idx < 0 {
			line = rem
			rem = nil
		} else {
			line = rem[:idx]
			rem = rem[idx+1:]
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			dataLine = bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
		} else {
			prefixLines = append(prefixLines, trimmed)
		}
	}
	if len(dataLine) == 0 || dataLine[0] != '{' {
		return frame, false
	}
	dec := json.NewDecoder(bytes.NewReader(dataLine))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return frame, false
	}
	if _, hasUsage := obj["usage"]; !hasUsage {
		return frame, false
	}
	if isUsageOnlyFrame(obj) {
		return nil, true
	}
	delete(obj, "usage")
	encoded, err := json.Marshal(obj)
	if err != nil {
		return frame, false
	}
	var out bytes.Buffer
	for _, pl := range prefixLines {
		out.Write(pl)
		out.WriteByte('\n')
	}
	out.WriteString("data: ")
	out.Write(encoded)
	out.WriteString("\n\n")
	return out.Bytes(), false
}

func isUsageOnlyFrame(obj map[string]any) bool {
	choices, hasChoices := obj["choices"].([]any)
	if !hasChoices {
		_, hasType := obj["type"]
		return !hasType
	}
	if len(choices) == 0 {
		return true
	}
	if len(choices) == 1 {
		c0, ok := choices[0].(map[string]any)
		if !ok {
			return false
		}
		if c0["finish_reason"] != nil {
			return false
		}
		delta, ok := c0["delta"].(map[string]any)
		if ok && len(delta) == 0 {
			return true
		}
	}
	return false
}
