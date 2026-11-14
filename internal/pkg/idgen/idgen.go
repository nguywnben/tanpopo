// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package idgen generates lexicographically monotonic, collision-resistant
// 128-bit UUIDv7 identifiers encoded in lowercase Crockford Base32 with
// canonical Tanpopo domain prefixes (<prefix>_<26_chars>, total length 30 <= 40).
package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const crockfordAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// Canonical system-wide entity prefixes (TP-IMP-0007 / ARCH-DATA-01).
const (
	PrefixOrg              = "org"
	PrefixProject          = "prj"
	PrefixEnv              = "env"
	PrefixPrincipal        = "prc"
	PrefixVirtualKey       = "vky"
	PrefixSecret           = "sec"
	PrefixExtension        = "ext"
	PrefixExtensionVersion = "exv"
	PrefixInstallation     = "ins"
	PrefixAdapter          = "adp"
	PrefixConnection       = "con"
	PrefixLogicalModel     = "lmd"
	PrefixDeployment       = "dpl"
	PrefixPolicy           = "pol"
	PrefixRoute            = "rte"
	PrefixBudget           = "bdg"
	PrefixReservation      = "rsv"
	PrefixSettlement       = "set"
	PrefixTrace            = "trc"
	PrefixAudit            = "aud"
	PrefixKillSwitch       = "ksw"
	PrefixMCP              = "mcp"
	PrefixApproval         = "apr"
	PrefixWebhook          = "whk"
	PrefixAgent            = "agt"
	PrefixTask             = "tsk"
)

var canonicalPrefixes = []string{
	PrefixOrg, PrefixProject, PrefixEnv, PrefixPrincipal, PrefixVirtualKey, PrefixSecret,
	PrefixExtension, PrefixExtensionVersion, PrefixInstallation, PrefixAdapter, PrefixConnection,
	PrefixLogicalModel, PrefixDeployment, PrefixPolicy, PrefixRoute, PrefixBudget,
	PrefixReservation, PrefixSettlement, PrefixTrace, PrefixAudit, PrefixKillSwitch,
	PrefixMCP, PrefixApproval, PrefixWebhook, PrefixAgent, PrefixTask,
}

var (
	validPrefixSet map[string]bool
	decodeTable    [256]int8
)

func init() {
	validPrefixSet = make(map[string]bool, len(canonicalPrefixes))
	for _, p := range canonicalPrefixes {
		validPrefixSet[p] = true
	}
	for i := range decodeTable {
		decodeTable[i] = -1
	}
	for idx, ch := range crockfordAlphabet {
		decodeTable[byte(ch)] = int8(idx)
	}
}

var (
	// ErrInvalidPrefix indicates an unrecognized domain ID prefix.
	ErrInvalidPrefix = errors.New("idgen: invalid domain prefix")
	// ErrInvalidID indicates a malformed domain ID string.
	ErrInvalidID = errors.New("idgen: invalid domain identifier")
)

const maxCounter24 uint32 = 0xffffff

// Generator produces monotonically ordered prefixed UUIDv7 identifiers.
type Generator struct {
	mu     sync.Mutex
	nowFn  func() time.Time
	lastMs uint64
	seq    uint32
}

var defaultGen = NewGenerator(nil)

// NewGenerator constructs an isolated UUIDv7 Generator using nowFn (or time.Now if nil).
func NewGenerator(nowFn func() time.Time) *Generator {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Generator{nowFn: nowFn}
}

// IsValidPrefix reports whether prefix is one of the 26 canonical domain prefixes.
func IsValidPrefix(prefix string) bool {
	return validPrefixSet[prefix]
}

// AllPrefixes returns a defensive slice of all 26 canonical domain prefixes.
func AllPrefixes() []string {
	out := make([]string, len(canonicalPrefixes))
	copy(out, canonicalPrefixes)
	return out
}

// New mints a canonical prefixed UUIDv7 identifier from the default generator.
func New(prefix string) (string, error) {
	return defaultGen.New(prefix)
}

// MustNew mints a canonical prefixed UUIDv7 identifier or panics on an invalid prefix.
func MustNew(prefix string) string {
	id, err := New(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

// New mints a lexicographically monotonic <prefix>_<26_crockford_chars> UUIDv7 ID.
func (g *Generator) New(prefix string) (string, error) {
	if !IsValidPrefix(prefix) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
	}

	var randBuf [9]byte
	if _, err := rand.Read(randBuf[:]); err != nil {
		return "", fmt.Errorf("idgen: crypto/rand read: %w", err)
	}

	g.mu.Lock()
	nowFn := g.nowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	msRaw := nowFn().UTC().UnixMilli()
	var nowMs uint64
	if msRaw > 0 {
		nowMs = uint64(msRaw)
	}

	if nowMs > g.lastMs {
		g.lastMs = nowMs
		// Seed initial sub-ms counter from 12 CSPRNG bits, leaving >16.7M monotonic steps per ms.
		g.seq = uint32(binary.BigEndian.Uint16(randBuf[:2]) & 0x0fff)
	} else {
		if g.seq >= maxCounter24 {
			g.lastMs++
			g.seq = 0
		} else {
			g.seq++
		}
	}
	tsMs := g.lastMs & 0xffffffffffff
	seq := g.seq
	g.mu.Unlock()

	var u [16]byte
	// 48-bit big-endian unix_ts_ms.
	u[0] = byte(tsMs >> 40)
	u[1] = byte(tsMs >> 32)
	u[2] = byte(tsMs >> 24)
	u[3] = byte(tsMs >> 16)
	u[4] = byte(tsMs >> 8)
	u[5] = byte(tsMs)

	// RFC 9562 version 7 (0b0111) + 12-bit rand_a carrying top 12 bits of 24-bit monotonic seq.
	u[6] = 0x70 | byte((seq>>20)&0x0f)
	u[7] = byte((seq >> 12) & 0xff)

	// RFC 4122/9562 variant (0b10) + low 12 bits of 24-bit seq + 50 bits of CSPRNG.
	u[8] = 0x80 | byte((seq>>6)&0x3f)
	u[9] = byte((seq&0x3f)<<2) | (randBuf[2] & 0x03)
	copy(u[10:], randBuf[3:9])

	return prefix + "_" + encode128Crockford(u), nil
}

// Parse validates and decodes a canonical prefixed UUIDv7 identifier.
func Parse(id string) (prefix string, uuidBytes [16]byte, ts time.Time, err error) {
	p, body, ok := strings.Cut(id, "_")
	if !ok || !IsValidPrefix(p) || len(body) != 26 {
		return "", uuidBytes, time.Time{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	u, derr := decode128Crockford(body)
	if derr != nil {
		return "", uuidBytes, time.Time{}, fmt.Errorf("%w: %v", ErrInvalidID, derr)
	}
	if u[6]>>4 != 7 || u[8]>>6 != 2 {
		return "", uuidBytes, time.Time{}, fmt.Errorf("%w: non-UUIDv7 version/variant bits", ErrInvalidID)
	}
	ms := (uint64(u[0]) << 40) |
		(uint64(u[1]) << 32) |
		(uint64(u[2]) << 24) |
		(uint64(u[3]) << 16) |
		(uint64(u[4]) << 8) |
		uint64(u[5])
	return p, u, time.UnixMilli(int64(ms)).UTC(), nil
}

// Valid reports whether id is a well-formed canonical prefixed UUIDv7 identifier.
func Valid(id string) bool {
	_, _, _, err := Parse(id)
	return err == nil
}

// encode128Crockford encodes 128 bits MSB-first into 26 lowercase Crockford Base32 chars
// with 2 leading zero pad bits so lexicographical string comparison matches big-endian uint128.
func encode128Crockford(u [16]byte) string {
	var out [26]byte
	for c := 0; c < 26; c++ {
		var val byte
		for b := 0; b < 5; b++ {
			bitPos := c*5 + b // 0..129; 0..1 are leading zero padding
			val <<= 1
			if bitPos >= 2 {
				uBit := bitPos - 2
				byteIdx := uBit / 8
				shift := 7 - (uBit % 8)
				val |= (u[byteIdx] >> shift) & 1
			}
		}
		out[c] = crockfordAlphabet[val]
	}
	return string(out[:])
}

func decode128Crockford(s string) ([16]byte, error) {
	var u [16]byte
	if len(s) != 26 {
		return u, fmt.Errorf("expected 26 chars, got %d", len(s))
	}
	first := decodeTable[s[0]]
	if first < 0 {
		return u, fmt.Errorf("invalid base32 char %q", s[0])
	}
	if first&0x18 != 0 {
		return u, fmt.Errorf("overflow in top padding bits")
	}
	for c := 0; c < 26; c++ {
		v := decodeTable[s[c]]
		if v < 0 {
			return u, fmt.Errorf("invalid base32 char %q", s[c])
		}
		for b := 0; b < 5; b++ {
			bitPos := c*5 + b
			if bitPos < 2 {
				continue
			}
			bit := byte((v >> (4 - b)) & 1)
			uBit := bitPos - 2
			byteIdx := uBit / 8
			shift := 7 - (uBit % 8)
			u[byteIdx] |= bit << shift
		}
	}
	return u, nil
}
