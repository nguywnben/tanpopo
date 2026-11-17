// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package backup

import (
	"errors"
	"io"
)

const zstdRawBlockSize = 128 * 1024

// zstdRawWriter emits a standards-compliant streaming Zstandard frame with raw
// blocks. It keeps backup framing interoperable without introducing a runtime
// compression dependency; Unit 32 can decode the same frame for verification.
type zstdRawWriter struct {
	dst    io.Writer
	block  []byte
	closed bool
}

func newZstdRawWriter(dst io.Writer) (*zstdRawWriter, error) {
	w := &zstdRawWriter{dst: dst, block: make([]byte, 0, zstdRawBlockSize)}
	// Zstandard magic, a frame header with unknown content size, and a 128 KiB
	// window descriptor. Blocks use the raw (uncompressed) block type.
	if err := writeAll(dst, []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x38}); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *zstdRawWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("zstd: write after close")
	}
	written := 0
	for len(p) > 0 {
		if len(w.block) == zstdRawBlockSize {
			if err := w.emit(false); err != nil {
				return written, err
			}
		}
		n := zstdRawBlockSize - len(w.block)
		if n > len(p) {
			n = len(p)
		}
		w.block = append(w.block, p[:n]...)
		p = p[n:]
		written += n
	}
	return written, nil
}

func (w *zstdRawWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.emit(true)
}

func (w *zstdRawWriter) emit(last bool) error {
	value := uint32(len(w.block)) << 3
	if last {
		value |= 1
	}
	var header [3]byte
	header[0] = byte(value)
	header[1] = byte(value >> 8)
	header[2] = byte(value >> 16)
	if err := writeAll(w.dst, header[:]); err != nil {
		return err
	}
	if err := writeAll(w.dst, w.block); err != nil {
		return err
	}
	w.block = w.block[:0]
	return nil
}

func writeAll(dst io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := dst.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
