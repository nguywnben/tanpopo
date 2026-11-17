// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"bytes"
	"fmt"
	"testing"
)

func TestZstdRawWriterFramesEmptyExactAndMultiBlockPayloads(t *testing.T) {
	for _, size := range []int{0, 64, zstdRawBlockSize, zstdRawBlockSize + 17, 2*zstdRawBlockSize + 3} {
		t.Run(fmt.Sprintf("size-%d", size), func(t *testing.T) {
			payload := bytes.Repeat([]byte("z"), size)
			var encoded bytes.Buffer
			writer, err := newZstdRawWriter(&encoded)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := writer.Write(payload); err != nil || n != len(payload) {
				t.Fatalf("Write() = %d, %v; want %d, nil", n, err, len(payload))
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			frame := encoded.Bytes()
			if len(frame) < 9 || !bytes.Equal(frame[:4], []byte{0x28, 0xb5, 0x2f, 0xfd}) || frame[4] != 0 || frame[5] != 0x38 {
				t.Fatalf("invalid Zstandard frame header: %x", frame[:min(len(frame), 8)])
			}
			decoded, last := make([]byte, 0, size), false
			for pos := 6; !last; {
				if pos+3 > len(frame) {
					t.Fatal("truncated Zstandard block header")
				}
				header := uint32(frame[pos]) | uint32(frame[pos+1])<<8 | uint32(frame[pos+2])<<16
				pos += 3
				last = header&1 != 0
				if (header>>1)&3 != 0 {
					t.Fatalf("block type = %d, want raw", (header>>1)&3)
				}
				blockSize := int(header >> 3)
				if pos+blockSize > len(frame) {
					t.Fatal("truncated Zstandard raw block")
				}
				decoded = append(decoded, frame[pos:pos+blockSize]...)
				pos += blockSize
				if last && pos != len(frame) {
					t.Fatal("trailing bytes after final Zstandard block")
				}
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatalf("decoded payload size %d, want %d", len(decoded), len(payload))
			}
		})
	}
}
