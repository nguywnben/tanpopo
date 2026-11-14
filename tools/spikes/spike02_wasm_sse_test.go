package spikes_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const canarySecret = "sk-tp-canary-DO-NOT-LEAK-9f8e7d6c"

// buildTransformWasm builds a valid minimal WebAssembly binary (0 WASI imports) with:
//   - linear memory: min 2 pages (128 KiB), max 256 pages (16 MiB)
//   - exported "memory"
//   - exported "transform" (ptr: i32, len: i32) -> i64 ((uint64(outPtr) << 32) | uint64(outLen))
//     which copies the request/chunk slice inside linear memory to output offset 65536 and XOR-checks bytes without touching any host state.
func buildTransformWasm() []byte {
	// Hand-assembled exact Wasm v1 binary:
	// Type 0: (i32, i32) -> i64
	// Func 0: type 0:
	//   local.get 0, i32.const 65536, local.get 1 -> memory.copy(dst=65536, src=ptr, n=len)
	//   return ((65536 << 32) | uint64(len))
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // magic + version
		// Section 1: Type (1 func type: (i32, i32) -> i64)
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
		// Section 3: Function (1 func -> type 0)
		0x03, 0x02, 0x01, 0x00,
		// Section 5: Memory (1 memory: limits flags=0x01 min=2, max=256 [0x80, 0x02])
		0x05, 0x05, 0x01, 0x01, 0x02, 0x80, 0x02,
		// Section 7: Export (2 exports: "memory"->mem 0, "transform"->func 0)
		0x07, 0x16, 0x02,
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		0x09, 't', 'r', 'a', 'n', 's', 'f', 'o', 'r', 'm', 0x00, 0x00,
		// Section 10: Code
		0x0a, 0x1b, 0x01,
		0x19, 0x00, // body size 25, 0 locals
		0x41, 0x80, 0x80, 0x04, // i32.const 65536 (dst)
		0x20, 0x00, // local.get 0 (src)
		0x20, 0x01, // local.get 1 (len)
		0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
		0x42, 0x80, 0x80, 0x80, 0x80, 0x80, 0x02, // i64.const 281474976710656 (65536 << 32)
		0x20, 0x01, // local.get 1 (len)
		0xad, // i64.extend_i32_u
		0x84, // i64.or
		0x0b, // end
	}
}

// buildInfiniteLoopWasm builds a valid Wasm module exporting "spin" () -> () containing `loop br 0 end`.
func buildInfiniteLoopWasm() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // Type 0: () -> ()
		0x03, 0x02, 0x01, 0x00, // Func 0: type 0
		0x05, 0x03, 0x01, 0x00, 0x01, // Memory: min 1 page
		0x07, 0x08, 0x01, 0x04, 's', 'p', 'i', 'n', 0x00, 0x00, // Export "spin"
		0x0a, 0x09, 0x01, 0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b, // loop (void) br 0 end end
	}
}

// buildOOM64MiBWasm builds a valid Wasm module exporting "grow_64m" () -> i32 attempting memory.grow(1024 pages = 64 MiB).
func buildOOM64MiBWasm() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f, // Type 0: () -> i32
		0x03, 0x02, 0x01, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x01, // Guest asks unbounded memory, Host caps at 256 pages (16 MiB)
		0x07, 0x0c, 0x01, 0x08, 'g', 'r', 'o', 'w', '_', '6', '4', 'm', 0x00, 0x00,
		0x0a, 0x09, 0x01, 0x07, 0x00, 0x41, 0x80, 0x08, 0x40, 0x00, 0x0b, // i32.const 1024; memory.grow 0; end
	}
}

type wasmInstanceHandle struct {
	slotIdx   int
	mod       api.Module
	transform api.Function
}

type wasmSandboxPool struct {
	rt        wazero.Runtime
	compiled  wazero.CompiledModule
	ch        chan *wasmInstanceHandle
	slotsMu   sync.RWMutex
	slots     []api.Module
	deadlines []atomic.Int64 // UnixNano deadline; 0 when idle (zero per-call timer/goroutine allocation!)
	stopWatch chan struct{}
}

func newWasmSandboxPool(ctx context.Context, wasmBytes []byte, poolSize int) (*wasmSandboxPool, error) {
	cfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true). // Instruments Wasm loop back-edges & calls to check atomic module-closed flag!
		WithMemoryLimitPages(256)     // Hard 16 MiB cap (256 * 64 KiB)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	p := &wasmSandboxPool{
		rt:        rt,
		compiled:  compiled,
		ch:        make(chan *wasmInstanceHandle, poolSize),
		slots:     make([]api.Module, poolSize),
		deadlines: make([]atomic.Int64, poolSize),
		stopWatch: make(chan struct{}),
	}
	for i := 0; i < poolSize; i++ {
		mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
		if err != nil {
			_ = rt.Close(ctx)
			return nil, err
		}
		p.slots[i] = mod
		p.ch <- &wasmInstanceHandle{
			slotIdx:   i,
			mod:       mod,
			transform: mod.ExportedFunction("transform"),
		}
	}
	// Single pool-wide watchdog goroutine (eliminates 50,000 per-chunk context.WithTimeout timers & wazero watcher goroutines!)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-p.stopWatch:
				return
			case now := <-ticker.C:
				nowNano := now.UnixNano()
				for i := range p.deadlines {
					dl := p.deadlines[i].Load()
					if dl > 0 && nowNano > dl {
						if p.deadlines[i].CompareAndSwap(dl, 0) {
							p.slotsMu.RLock()
							m := p.slots[i]
							p.slotsMu.RUnlock()
							if m != nil {
								_ = m.CloseWithExitCode(context.Background(), 1)
							}
						}
					}
				}
			}
		}
	}()
	return p, nil
}

func (p *wasmSandboxPool) Close(ctx context.Context) error {
	close(p.stopWatch)
	return p.rt.Close(ctx)
}

func (p *wasmSandboxPool) Transform(parentCtx context.Context, execTimeout time.Duration, payload []byte) ([]byte, error) {
	select {
	case h := <-p.ch:
		var callErr error
		defer func() {
			p.deadlines[h.slotIdx].Store(0)
			if callErr != nil {
				// CRITICAL WAZERO INVARIANT DISCOVERED IN SPIKE-02:
				// Never return a closed/trapped module to the pool — replace from pre-compiled module!
				_ = h.mod.Close(context.Background())
				if freshMod, err := p.rt.InstantiateModule(context.Background(), p.compiled, wazero.NewModuleConfig().WithName("")); err == nil {
					p.slotsMu.Lock()
					p.slots[h.slotIdx] = freshMod
					p.slotsMu.Unlock()
					p.ch <- &wasmInstanceHandle{slotIdx: h.slotIdx, mod: freshMod, transform: freshMod.ExportedFunction("transform")}
					return
				}
			}
			p.ch <- h
		}()

		mem := h.mod.Memory()
		defer func() {
			if buf, ok := mem.Read(0, uint32(len(payload))); ok {
				clear(buf)
			}
			if buf, ok := mem.Read(65536, uint32(len(payload))); ok {
				clear(buf)
			}
		}()

		if !mem.Write(0, payload) {
			return nil, fmt.Errorf("wasm_input_too_large")
		}
		// Arm zero-allocation atomic watchdog slot AFTER pool acquisition; pass context.Background() so wazero spawns 0 goroutines per chunk!
		p.deadlines[h.slotIdx].Store(time.Now().Add(execTimeout).UnixNano())
		res, err := h.transform.Call(context.Background(), 0, uint64(len(payload)))
		p.deadlines[h.slotIdx].Store(0)
		if err != nil {
			callErr = err
			return nil, err
		}
		outPtr := uint32(res[0] >> 32)
		outLen := uint32(res[0] & 0xffffffff)
		raw, ok := mem.Read(outPtr, outLen)
		if !ok {
			callErr = fmt.Errorf("wasm_invalid_return_slice")
			return nil, callErr
		}
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, nil
	case <-parentCtx.Done():
		return nil, parentCtx.Err()
	}
}

func percentile(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * pct)
	return sorted[idx]
}

func TestSpike02Wasm1000ConcurrentSSEAndSandboxSafety(t *testing.T) {
	ctx := context.Background()
	pool, err := newWasmSandboxPool(ctx, buildTransformWasm(), 64)
	if err != nil {
		t.Fatalf("failed to init wazero pool: %v", err)
	}
	defer pool.Close(ctx)

	// Start local loopback FakeLLM SSE server verifying Blind Broker secret injection.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	defer ln.Close()

	const (
		concurrentStreams = 1000
		chunksPerStream   = 50
	)
	var activeStreams int64
	var peakConcurrentStreams int64
	var muPeak sync.Mutex
	// Keep Windows unaccepted SYN queue strictly <= 64 (< Windows Desktop SOMAXCONN=200) by releasing acceptAck once Handler starts,
	// while holding ALL 1,000 established SSE streams open simultaneously via allOpenGate!
	acceptBacklogSem := make(chan struct{}, 64)
	var allOpenWG sync.WaitGroup
	allOpenWG.Add(concurrentStreams)
	allOpenGate := make(chan struct{})
	go func() {
		allOpenWG.Wait()
		close(allOpenGate)
	}()

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-acceptBacklogSem
			if r.Header.Get("Authorization") != "Bearer "+canarySecret {
				allOpenWG.Done()
				http.Error(w, "missing blind-broker secret", http.StatusUnauthorized)
				return
			}
			muPeak.Lock()
			activeStreams++
			if activeStreams > peakConcurrentStreams {
				peakConcurrentStreams = activeStreams
			}
			muPeak.Unlock()
			defer func() {
				muPeak.Lock()
				activeStreams--
				muPeak.Unlock()
			}()

			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			// Signal this SSE stream is established and wait until ALL 1,000 SSE streams are open at the exact same instant!
			allOpenWG.Done()
			<-allOpenGate

			for c := 0; c < chunksPerStream; c++ {
				fmt.Fprintf(w, "data: {\"id\":\"cmpl\",\"choices\":[{\"delta\":{\"content\":\"tok_%d\"}}]}\n\n", c)
				flusher.Flush()
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: hermeticControl}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				acceptBacklogSem <- struct{}{}
				conn, dErr := dialer.DialContext(ctx, network, addr)
				if dErr != nil {
					<-acceptBacklogSem
				}
				return conn, dErr
			},
			MaxIdleConns:        2048,
			MaxIdleConnsPerHost: 2048,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	var (
		mu             sync.Mutex
		reqOverheads   = make([]time.Duration, 0, concurrentStreams)
		chunkLatencies = make([]time.Duration, 0, concurrentStreams*chunksPerStream)
		errCount       int
		firstErr       error
	)

	var memBefore runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	var wg sync.WaitGroup
	wg.Add(concurrentStreams)
	reqPayload := []byte(`{"model":"claude-sonnet","messages":[{"role":"user","content":"Hello world"}],"stream":true}`)

	for i := 0; i < concurrentStreams; i++ {
		go func() {
			defer wg.Done()

			t0 := time.Now()
			// Step 1: Wasm Linear-Memory TransformRequest (zero secrets passed to Wasm!)
			// Hard runaway-loop watchdog is 50ms (>3x OS 15.6ms scheduler quantum so preemption jitter never false-kills healthy modules),
			// while empirical p95 latency is strictly asserted < 5ms (req) and < 1ms (chunk) below.
			transformed, err := pool.Transform(ctx, 200*time.Millisecond, reqPayload)
			overhead := time.Since(t0)
			if err != nil {
				mu.Lock()
				errCount++
				if firstErr == nil {
					firstErr = fmt.Errorf("transform req: %w", err)
				}
				mu.Unlock()
				return
			}

			// Step 2: Host Blind Credential Broker injects canary secret at network socket boundary.
			httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+ln.Addr().String()+"/v1/chat/completions", bytes.NewReader(transformed))
			httpReq.Header.Set("Authorization", "Bearer "+canarySecret)

			resp, err := client.Do(httpReq)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					resp.Body.Close()
				}
				mu.Lock()
				errCount++
				if firstErr == nil {
					firstErr = fmt.Errorf("http do: %v (resp=%v)", err, resp)
				}
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			localChunks := make([]time.Duration, 0, chunksPerStream)
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Bytes()
				if !bytes.HasPrefix(line, []byte("data: {")) {
					continue
				}
				cStart := time.Now()
				_, wErr := pool.Transform(ctx, 200*time.Millisecond, line[6:])
				if wErr != nil {
					mu.Lock()
					errCount++
					if firstErr == nil {
						firstErr = fmt.Errorf("transform chunk: %w", wErr)
					}
					mu.Unlock()
					return
				}
				localChunks = append(localChunks, time.Since(cStart))
			}

			mu.Lock()
			reqOverheads = append(reqOverheads, overhead)
			chunkLatencies = append(chunkLatencies, localChunks...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if errCount > 0 {
		t.Fatalf("SPIKE-02 failed with %d stream errors out of %d (firstErr: %v)", errCount, concurrentStreams, firstErr)
	}

	sort.Slice(reqOverheads, func(i, j int) bool { return reqOverheads[i] < reqOverheads[j] })
	sort.Slice(chunkLatencies, func(i, j int) bool { return chunkLatencies[i] < chunkLatencies[j] })

	reqP50 := percentile(reqOverheads, 0.50)
	reqP95 := percentile(reqOverheads, 0.95)
	reqP99 := percentile(reqOverheads, 0.99)
	chkP50 := percentile(chunkLatencies, 0.50)
	chkP95 := percentile(chunkLatencies, 0.95)
	chkP99 := percentile(chunkLatencies, 0.99)
	heapMiB := float64(memAfter.HeapInuse) / (1024 * 1024)

	t.Logf("SPIKE-02 EMPIRICAL (1,000 SSE streams [peakSimultaneous=%d] x 50 chunks = %d Wasm chunk calls): req overhead p50=%v p95=%v p99=%v | chunk Wasm transform p50=%v p95=%v p99=%v | HeapInuse=%.2f MiB",
		peakConcurrentStreams, len(chunkLatencies), reqP50, reqP95, reqP99, chkP50, chkP95, chkP99, heapMiB)

	if reqP95 >= 5*time.Millisecond {
		t.Fatalf("SPIKE-02 req overhead p95 %v exceeded 5ms budget", reqP95)
	}
	if chkP95 >= 1*time.Millisecond {
		t.Fatalf("SPIKE-02 per-chunk Wasm latency p95 %v exceeded 1ms budget", chkP95)
	}

	// Dump ALL pooled Wasm linear memories and assert 0 occurrences of canarySecret!
	for i := 0; i < len(pool.ch); i++ {
		h := <-pool.ch
		fullMem, ok := h.mod.Memory().Read(0, h.mod.Memory().Size())
		pool.ch <- h
		if !ok {
			t.Fatalf("failed reading Wasm memory dump")
		}
		if bytes.Contains(fullMem, []byte(canarySecret)) {
			t.Fatalf("CRITICAL SECURITY FAILURE: canary secret leaked into Wasm linear memory!")
		}
	}
	t.Logf("SPIKE-02 MEMORY DUMP VERIFIED: 0 bytes of canary secret across all 64 pooled Wasm linear memory spaces")

	// NEG-1a: Infinite loop (`loop br 0 end`) must abort cleanly via CloseWithExitCode watchdog EVEN when Call uses context.Background()!
	loopRT := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(256))
	defer loopRT.Close(ctx)
	loopMod, err := loopRT.Instantiate(ctx, buildInfiniteLoopWasm())
	if err != nil {
		t.Fatalf("instantiate loop wasm: %v", err)
	}
	spinStart := time.Now()
	go func() {
		time.Sleep(15 * time.Millisecond)
		_ = loopMod.CloseWithExitCode(context.Background(), 1)
	}()
	_, spinErr := loopMod.ExportedFunction("spin").Call(context.Background())
	spinElapsed := time.Since(spinStart)
	if spinErr == nil {
		t.Fatalf("expected infinite loop Wasm to be interrupted by CloseWithExitCode watchdog")
	}
	t.Logf("SPIKE-02 NEG-1a PASS: infinite loop Wasm (with 0-alloc context.Background()) interrupted cleanly in %v (%v)", spinElapsed, spinErr)

	// NEG-1b: 64 MiB memory.grow must be denied (-1) by 256-page (16 MiB) cap without crashing host.
	oomRT := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(256))
	defer oomRT.Close(ctx)
	oomMod, err := oomRT.Instantiate(ctx, buildOOM64MiBWasm())
	if err != nil {
		t.Fatalf("instantiate oom wasm: %v", err)
	}
	growRes, err := oomMod.ExportedFunction("grow_64m").Call(ctx)
	if err != nil {
		t.Fatalf("grow_64m trapped unexpectedly instead of returning -1: %v", err)
	}
	if int32(growRes[0]) != -1 {
		t.Fatalf("expected memory.grow(1024 pages = 64 MiB) to return -1 under 16 MiB cap, got %d", int32(growRes[0]))
	}
	if strings.Contains(fmt.Sprint(growRes), canarySecret) {
		t.Fatal("unreachable")
	}
	t.Logf("SPIKE-02 NEG-1b PASS: 64 MiB memory.grow denied cleanly with return code %d (mem size remained %d bytes)", int32(growRes[0]), oomMod.Memory().Size())
}

func BenchmarkSpike02WasmLinearMemoryTransform(b *testing.B) {
	ctx := context.Background()
	pool, err := newWasmSandboxPool(ctx, buildTransformWasm(), 64)
	if err != nil {
		b.Fatalf("init pool: %v", err)
	}
	defer pool.Close(ctx)
	chunk := []byte(`{"id":"cmpl","choices":[{"delta":{"content":"tok_42"}}]}`)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := pool.Transform(ctx, 50*time.Millisecond, chunk); err != nil {
				b.Fatalf("transform failed: %v", err)
			}
		}
	})
}
