// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestZstdWriterCompressesAndRoundTripsPayloads(t *testing.T) {
	for _, size := range []int{0, 64, 128 * 1024, 512 * 1024} {
		t.Run(fmt.Sprintf("size-%d", size), func(t *testing.T) {
			payload := bytes.Repeat([]byte("tanpopo backup payload\n"), size/23+1)[:size]
			var encoded bytes.Buffer
			writer, err := newZstdWriter(&encoded)
			if err != nil {
				t.Fatal(err)
			}
			for pos := 0; pos < len(payload); {
				end := min(pos+4093, len(payload))
				if _, err := writer.Write(payload[pos:end]); err != nil {
					t.Fatal(err)
				}
				pos = end
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			decoder, err := zstd.NewReader(bytes.NewReader(encoded.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := io.ReadAll(decoder)
			decoder.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatalf("decoded %d bytes, want %d", len(decoded), len(payload))
			}
			if size >= 128*1024 && encoded.Len() >= size/2 {
				t.Fatalf("repetitive payload compressed to %d bytes; want less than %d", encoded.Len(), size/2)
			}
		})
	}
}
