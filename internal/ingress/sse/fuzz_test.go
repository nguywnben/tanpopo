// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package sse_test

import (
	"testing"

	"github.com/nguywnben/tanpopo/internal/ingress/sse"
)

func FuzzSSEParser(f *testing.F) {
	seeds := [][]byte{
		[]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"),
		[]byte(": ping\n\nevent: content_block_delta\ndata: {\"delta\":{\"text\":\"ok\"}}\n\n"),
		[]byte("data: partial"),
		[]byte("\r\ndata: crlf\r\n\r\n"),
		[]byte{0xff, 0xfe, 'd', 'a', 't', 'a', ':', ' ', '{', '\n'},
		[]byte(""),
	}
	for _, s := range seeds {
		f.Add(s, 32)
	}

	const testMaxLine = 4096
	f.Fuzz(func(t *testing.T, data []byte, split int) {
		if split < 0 {
			split = -split
		}
		if len(data) > 0 {
			split = split % (len(data) + 1)
		} else {
			split = 0
		}

		// Two-part streaming split must enforce bounded maxLine and never panic.
		part1 := data[:split]
		part2 := data[split:]

		_, rem1, err1 := sse.ScanCompleteSSELines(nil, part1, testMaxLine, len(part2) == 0)
		if err1 != nil {
			return
		}
		if len(rem1) > testMaxLine {
			t.Fatalf("partial buffer exceeded maxLine (%d > %d)", len(rem1), testMaxLine)
		}
		if len(part2) > 0 {
			_, rem2, err2 := sse.ScanCompleteSSELines(rem1, part2, testMaxLine, true)
			if err2 != nil {
				return
			}
			if len(rem2) != 0 {
				t.Fatalf("expected drained partial buffer at EOF, got %d bytes", len(rem2))
			}
		}
	})
}
