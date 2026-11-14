// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package idgen

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIDGen10000MonotonicAndCollisionFreeAC2(t *testing.T) {
	t.Parallel()

	const count = 10000
	seen := make(map[string]struct{}, count)
	var prev string

	for i := 0; i < count; i++ {
		id, err := New(PrefixConnection)
		if err != nil {
			t.Fatalf("New(%q) failed at %d: %v", PrefixConnection, i, err)
		}
		if len(id) != 30 {
			t.Fatalf("expected length 30, got %d (%q)", len(id), id)
		}
		if len(id) > 40 {
			t.Fatalf("exceeded VARCHAR(40): %d", len(id))
		}
		if !Valid(id) {
			t.Fatalf("Valid(%q) returned false", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("collision detected at %d: %q", i, id)
		}
		seen[id] = struct{}{}

		if prev != "" && id <= prev {
			t.Fatalf("non-monotonic ordering at %d: prev=%q cur=%q", i, prev, id)
		}
		prev = id
	}
}

func TestIDGenAll26CanonicalPrefixesAndRoundTrip(t *testing.T) {
	t.Parallel()

	prefixes := AllPrefixes()
	if len(prefixes) != 26 {
		t.Fatalf("expected 26 canonical prefixes, got %d", len(prefixes))
	}

	fixedTime := time.Date(2026, 11, 14, 18, 0, 0, 123000000, time.UTC)
	gen := NewGenerator(func() time.Time { return fixedTime })

	for _, p := range prefixes {
		id, err := gen.New(p)
		if err != nil {
			t.Fatalf("gen.New(%q): %v", p, err)
		}
		if len(id) != 30 {
			t.Fatalf("prefix %q produced len %d (%q)", p, len(id), id)
		}
		gotPrefix, uuidBytes, gotTS, err := Parse(id)
		if err != nil {
			t.Fatalf("Parse(%q): %v", id, err)
		}
		if gotPrefix != p {
			t.Fatalf("expected prefix %q, got %q", p, gotPrefix)
		}
		if uuidBytes[6]>>4 != 7 || uuidBytes[8]>>6 != 2 {
			t.Fatalf("unexpected UUIDv7 version/variant bits: %x %x", uuidBytes[6], uuidBytes[8])
		}
		if gotTS.UnixMilli() != fixedTime.UnixMilli() {
			t.Fatalf("expected ts %d ms, got %d ms", fixedTime.UnixMilli(), gotTS.UnixMilli())
		}
	}
}

func TestIDGenClockRewindCounterOverflowAndConcurrency(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	curTime := time.Date(2026, 11, 14, 18, 0, 10, 0, time.UTC)
	gen := NewGenerator(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return curTime
	})

	first, err := gen.New(PrefixOrg)
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// Simulate NTP clock stepping backwards 5 seconds.
	mu.Lock()
	curTime = curTime.Add(-5 * time.Second)
	mu.Unlock()

	second, err := gen.New(PrefixOrg)
	if err != nil {
		t.Fatalf("second after clock rewind: %v", err)
	}
	if second <= first {
		t.Fatalf("expected monotonic ID on clock rewind: first=%q second=%q", first, second)
	}

	// Force counter saturation (maxCounter24) and confirm ms advance keeps strict ordering.
	gen.mu.Lock()
	gen.seq = maxCounter24
	beforeMs := gen.lastMs
	gen.mu.Unlock()

	overflowID, err := gen.New(PrefixOrg)
	if err != nil {
		t.Fatalf("overflow: %v", err)
	}
	if overflowID <= second {
		t.Fatalf("expected strict increase on seq overflow: second=%q overflow=%q", second, overflowID)
	}
	_, _, overTS, err := Parse(overflowID)
	if err != nil || uint64(overTS.UnixMilli()) != beforeMs+1 {
		t.Fatalf("expected incremented ms on counter overflow: before=%d got=%v err=%v", beforeMs, overTS, err)
	}

	// Concurrent safety under -race.
	const workers = 16
	const perWorker = 50
	var wg sync.WaitGroup
	results := make(chan string, workers*perWorker)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				results <- MustNew(PrefixProject)
			}
		}()
	}
	wg.Wait()
	close(results)

	uniq := make(map[string]struct{}, workers*perWorker)
	for id := range results {
		if _, ok := uniq[id]; ok {
			t.Fatalf("concurrent duplicate ID: %q", id)
		}
		uniq[id] = struct{}{}
	}
}

func TestIDGenValidationAndNegativeCases(t *testing.T) {
	t.Parallel()

	for _, badPrefix := range []string{"", "bad", "org_", "ORG", "unknown"} {
		if _, err := New(badPrefix); !errors.Is(err, ErrInvalidPrefix) {
			t.Fatalf("expected ErrInvalidPrefix for %q, got %v", badPrefix, err)
		}
	}

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_ = MustNew("invalid")
	}()
	if !panicked {
		t.Fatalf("expected MustNew to panic on invalid prefix")
	}

	for _, badID := range []string{
		"",
		"bad_0123456789abcdefghjkmnpqrs",
		"org_short",
		"org_0123456789abcdefghjkmnpqrs!", // invalid char '!'
		"org_ilou0123456789abcdefghjkmn", // invalid Crockford chars i/l/o/u
		"org_zzzzzzzzzzzzzzzzzzzzzzzzzz", // top padding overflow
		"org_00000000000000000000000000", // version != 7
	} {
		if Valid(badID) {
			t.Fatalf("expected Valid(%q) == false", badID)
		}
		if _, _, _, err := Parse(badID); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("expected ErrInvalidID for %q, got %v", badID, err)
		}
	}
}
