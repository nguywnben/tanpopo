// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package schema_pin computes and verifies immutable pins for MCP tool schemas.
package schema_pin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/security/audit"
)

const (
	// MaxSchemaBytes bounds the full schema-pin input, including name and description.
	MaxSchemaBytes  = 1 << 20
	MaxCatalogBytes = 16 << 20
	maxJSONDepth    = 64
	maxDiffEntries  = 256
)

var (
	ErrInvalidDefinition = errors.New("schema pin: invalid tool definition")
	ErrSchemaTooLarge    = errors.New("schema pin: tool definition exceeds size limit")
	ErrCatalogTooLarge   = errors.New("schema pin: tool catalog exceeds size limit")
	ErrApprovedHash      = errors.New("schema pin: approved hash does not match approved definition")
)

// AddCatalogPageBytes accounts for paginated MCP response data without integer overflow.
func AddCatalogPageBytes(used, page int) (int, error) {
	if used < 0 || page < 0 || used > MaxCatalogBytes || page > MaxCatalogBytes-used {
		return used, ErrCatalogTooLarge
	}
	return used + page, nil
}

// Definition is the exact MCP tool content included in a pin.
// InputSchema must contain one JSON object.
type Definition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Change describes one deterministic JSON Pointer change between approved and observed content.
// Presence flags distinguish an absent property from an explicit JSON null.
type Change struct {
	Path            string          `json:"path"`
	Approved        json.RawMessage `json:"approved,omitempty"`
	Observed        json.RawMessage `json:"observed,omitempty"`
	ApprovedPresent bool            `json:"approvedPresent"`
	ObservedPresent bool            `json:"observedPresent"`
}

// Verification contains the observed definition's hash and any structured drift details.
type Verification struct {
	Hash          string   `json:"hash"`
	Drift         bool     `json:"drift"`
	Diff          []Change `json:"diff,omitempty"`
	DiffTruncated bool     `json:"diffTruncated,omitempty"`
}

// Hash returns the lowercase SHA-256 of RFC 8785 JCS({name, description, inputSchema}).
func Hash(def Definition) (string, error) {
	value, err := definitionValue(def)
	if err != nil {
		return "", err
	}
	canonical, err := audit.CanonicalizeValue(value)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize definition: %v", ErrInvalidDefinition, err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// Verify validates the stored approval and compares it with an observed definition.
// Invalid, stale, malformed, or oversized inputs return an error and never a successful result.
func Verify(approvedHash string, approved, observed Definition) (Verification, error) {
	if len(approvedHash) != sha256.Size*2 {
		return Verification{}, fmt.Errorf("%w: malformed digest", ErrApprovedHash)
	}
	if _, err := hex.DecodeString(approvedHash); err != nil {
		return Verification{}, fmt.Errorf("%w: malformed digest", ErrApprovedHash)
	}
	approvedActual, err := Hash(approved)
	if err != nil {
		return Verification{}, err
	}
	if !strings.EqualFold(approvedActual, approvedHash) {
		return Verification{}, ErrApprovedHash
	}
	observedHash, err := Hash(observed)
	if err != nil {
		return Verification{}, err
	}
	result := Verification{Hash: observedHash, Drift: !strings.EqualFold(approvedActual, observedHash)}
	if !result.Drift {
		return result, nil
	}
	approvedValue, err := definitionValue(approved)
	if err != nil {
		return Verification{}, err
	}
	observedValue, err := definitionValue(observed)
	if err != nil {
		return Verification{}, err
	}
	diffValues("", approvedValue, observedValue, true, true, &result)
	return result, nil
}

func definitionValue(def Definition) (map[string]any, error) {
	if !utf8.ValidString(def.Name) || !utf8.ValidString(def.Description) {
		return nil, fmt.Errorf("%w: invalid UTF-8 metadata", ErrInvalidDefinition)
	}
	if len(def.InputSchema) > MaxSchemaBytes || len(def.Name)+len(def.Description) > MaxSchemaBytes-len(def.InputSchema) {
		return nil, ErrSchemaTooLarge
	}
	schema, err := parseSchema(def.InputSchema)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name":        def.Name,
		"description": def.Description,
		"inputSchema": schema,
	}, nil
}

func parseSchema(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return nil, fmt.Errorf("%w: empty or invalid UTF-8 inputSchema", ErrInvalidDefinition)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	value, err := parseJSONValue(dec, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: inputSchema: %v", ErrInvalidDefinition, err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("%w: inputSchema must be a JSON object", ErrInvalidDefinition)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: inputSchema has trailing data", ErrInvalidDefinition)
	}
	return value.(map[string]any), nil
}

func parseJSONValue(dec *json.Decoder, depth int) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch token := tok.(type) {
	case json.Delim:
		if depth >= maxJSONDepth {
			return nil, fmt.Errorf("JSON nesting exceeds %d", maxJSONDepth)
		}
		switch token {
		case '{':
			object := make(map[string]any)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				if _, exists := object[key]; exists {
					return nil, fmt.Errorf("duplicate object key %q", key)
				}
				value, err := parseJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errors.New("unterminated object")
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for dec.More() {
				value, err := parseJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errors.New("unterminated array")
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", token)
		}
	case json.Number, string, bool, nil:
		return token, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %T", tok)
	}
}

func diffValues(path string, approved, observed any, approvedPresent, observedPresent bool, result *Verification) {
	if result.DiffTruncated {
		return
	}
	if approvedPresent && observedPresent {
		if a, okA := approved.(map[string]any); okA {
			if b, okB := observed.(map[string]any); okB {
				keys := make([]string, 0, len(a)+len(b))
				seen := make(map[string]struct{}, len(a)+len(b))
				for key := range a {
					keys = append(keys, key)
					seen[key] = struct{}{}
				}
				for key := range b {
					if _, ok := seen[key]; !ok {
						keys = append(keys, key)
					}
				}
				sort.Strings(keys)
				for _, key := range keys {
					before, beforeOK := a[key]
					after, afterOK := b[key]
					diffValues(path+"/"+escapePointer(key), before, after, beforeOK, afterOK, result)
				}
				return
			}
		}
		if a, okA := approved.([]any); okA {
			if b, okB := observed.([]any); okB {
				for i := 0; i < max(len(a), len(b)); i++ {
					var before, after any
					beforeOK, afterOK := i < len(a), i < len(b)
					if beforeOK {
						before = a[i]
					}
					if afterOK {
						after = b[i]
					}
					diffValues(path+"/"+strconv.Itoa(i), before, after, beforeOK, afterOK, result)
				}
				return
			}
		}
		if equivalent(approved, observed) {
			return
		}
	}
	change := Change{Path: path, ApprovedPresent: approvedPresent, ObservedPresent: observedPresent}
	if approvedPresent {
		change.Approved = canonicalRaw(approved)
	}
	if observedPresent {
		change.Observed = canonicalRaw(observed)
	}
	result.Diff = append(result.Diff, change)
	if len(result.Diff) > maxDiffEntries {
		result.Diff = result.Diff[:maxDiffEntries]
		result.DiffTruncated = true
	}
}

func equivalent(a, b any) bool {
	left, errLeft := audit.CanonicalizeValue(a)
	right, errRight := audit.CanonicalizeValue(b)
	return errLeft == nil && errRight == nil && bytes.Equal(left, right)
}

func canonicalRaw(value any) json.RawMessage {
	canonical, err := audit.CanonicalizeValue(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return append(json.RawMessage(nil), canonical...)
}

func escapePointer(part string) string {
	return strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
}
