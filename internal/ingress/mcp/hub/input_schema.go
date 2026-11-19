// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/security/audit"
)

const maxSchemaDepth = 64

// validateArguments enforces the inputSchema subset used by MCP tools. Unsupported
// validation keywords fail closed rather than silently broadening the accepted input.
func validateArguments(storedSchema, arguments json.RawMessage) error {
	return validateArgumentsContext(context.Background(), storedSchema, arguments)
}

func validateArgumentsContext(ctx context.Context, storedSchema, arguments json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var stored struct {
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal(storedSchema, &stored); err != nil || len(stored.InputSchema) == 0 {
		return errors.New("stored tool schema is invalid")
	}
	var schema any
	if err := decodeJSON(stored.InputSchema, &schema); err != nil {
		return errors.New("stored input schema is invalid")
	}
	if _, err := audit.CanonicalizeJSON(stored.InputSchema); err != nil {
		return errors.New("stored input schema is not unambiguous JSON")
	}
	var value any
	if err := decodeJSON(arguments, &value); err != nil {
		return errors.New("tool arguments are invalid JSON")
	}
	if _, err := audit.CanonicalizeJSON(arguments); err != nil {
		return errors.New("tool arguments are not unambiguous JSON")
	}
	if _, ok := value.(map[string]any); !ok {
		return errors.New("tool arguments must be a JSON object")
	}
	return validateValue(ctx, value, schema, 0)
}

// ValidateInputSchema checks the supported MCP JSON Schema subset without choosing an instance.
func ValidateInputSchema(raw []byte) error {
	var schema any
	if err := decodeJSON(raw, &schema); err != nil {
		return err
	}
	node, ok := schema.(map[string]any)
	if !ok {
		return errors.New("input schema must be a JSON object")
	}
	return validateSchemaNode(context.Background(), node, 0)
}

func decodeJSON(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > maxJSONRPCBytes {
		return errors.New("JSON value outside allowed size")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	return validateJSONTree(raw, true, false)
}

func validateJSONTree(raw []byte, rejectDuplicates, allowInputSchemaDuplicates bool) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var visit func(int, bool) error
	visit = func(depth int, checkDuplicates bool) error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		if depth >= maxSchemaDepth {
			return errors.New("JSON nesting exceeds limit")
		}
		switch delim {
		case '{':
			var seen map[string]struct{}
			if checkDuplicates {
				seen = make(map[string]struct{})
			}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key must be a string")
				}
				if checkDuplicates {
					if _, exists := seen[key]; exists {
						return errors.New("duplicate JSON object key")
					}
					seen[key] = struct{}{}
				}
				childChecksDuplicates := checkDuplicates && !(allowInputSchemaDuplicates && key == "inputSchema")
				if err := visit(depth+1, childChecksDuplicates); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("invalid JSON object")
			}
		case '[':
			for dec.More() {
				if err := visit(depth+1, checkDuplicates); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("invalid JSON array")
			}
		default:
			return errors.New("invalid JSON value")
		}
		return nil
	}
	if err := visit(0, rejectDuplicates); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

// ValidateJSON enforces unambiguous JSON and the shared 64-level nesting bound.
func ValidateJSON(raw []byte) error {
	return rejectDuplicateJSONKeys(raw)
}

// ValidateJSONDepth bounds parsing depth while allowing duplicate keys for later field-specific validation.
func ValidateJSONDepth(raw []byte) error {
	return validateJSONTree(raw, false, false)
}

// ValidateMCPResponseJSON rejects ambiguous JSON-RPC response fields while allowing
// duplicate keys inside tools/list inputSchema for the schema validator to lock and audit.
func ValidateMCPResponseJSON(raw []byte) error {
	return validateJSONTree(raw, true, true)
}

func validateValue(ctx context.Context, value, rawSchema any, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth >= maxSchemaDepth {
		return errors.New("input schema nesting exceeds limit")
	}
	schema, ok := rawSchema.(map[string]any)
	if !ok {
		return errors.New("input schema node must be an object")
	}
	if err := validateSchemaNode(ctx, schema, depth); err != nil {
		return err
	}
	if kind, exists := schema["type"]; exists && !valueHasType(value, kind.(string)) {
		return fmt.Errorf("value must have type %s", kind)
	}
	var valueKey string
	valueKeyReady := false
	getValueKey := func() (string, error) {
		if !valueKeyReady {
			var err error
			valueKey, err = exactJSONKey(value)
			if err != nil {
				return "", err
			}
			valueKeyReady = true
		}
		return valueKey, nil
	}
	if values, exists := schema["enum"]; exists {
		choices := values.([]any)
		candidateKey, err := getValueKey()
		if err != nil {
			return errors.New("value cannot be compared with enum")
		}
		matched := false
		for _, choice := range choices {
			if err := ctx.Err(); err != nil {
				return err
			}
			choiceKey, err := exactJSONKey(choice)
			if err != nil {
				return errors.New("enum value cannot be compared")
			}
			if candidateKey == choiceKey {
				matched = true
				break
			}
		}
		if !matched {
			return errors.New("value is not in enum")
		}
	}
	if expected, exists := schema["const"]; exists {
		candidateKey, err := getValueKey()
		expectedKey, keyErr := exactJSONKey(expected)
		if err != nil || keyErr != nil || candidateKey != expectedKey {
			return errors.New("value does not match const")
		}
	}
	switch current := value.(type) {
	case map[string]any:
		return validateObject(ctx, current, schema, depth)
	case []any:
		return validateArray(ctx, current, schema, depth)
	case string:
		return validateString(current, schema)
	case json.Number:
		return validateNumber(current, schema)
	default:
		return nil
	}
}

func validateSchemaNode(ctx context.Context, schema map[string]any, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for key := range schema {
		switch key {
		case "type", "required", "properties", "additionalProperties", "items", "enum", "const", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "format", "minItems", "maxItems", "uniqueItems", "minProperties", "maxProperties", "title", "description", "$schema", "$id", "default", "examples", "deprecated", "readOnly", "writeOnly":
		default:
			return fmt.Errorf("unsupported input schema keyword %q", key)
		}
	}
	if raw, exists := schema["type"]; exists {
		kind, ok := raw.(string)
		if !ok || !oneOf(kind, "object", "array", "string", "number", "integer", "boolean", "null") {
			return errors.New("schema type must be a supported string")
		}
	}
	if raw, exists := schema["enum"]; exists {
		if _, ok := raw.([]any); !ok {
			return errors.New("schema enum must be an array")
		}
	}
	if raw, exists := schema["required"]; exists {
		items, ok := raw.([]any)
		if !ok {
			return errors.New("schema required must be an array")
		}
		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, ok := item.(string); !ok {
				return errors.New("schema required entries must be strings")
			}
		}
	}
	if raw, exists := schema["properties"]; exists {
		properties, ok := raw.(map[string]any)
		if !ok {
			return errors.New("schema properties must be an object")
		}
		for _, child := range properties {
			if err := ctx.Err(); err != nil {
				return err
			}
			node, ok := child.(map[string]any)
			if !ok {
				return errors.New("property schema must be an object")
			}
			if err := validateSchemaNode(ctx, node, depth+1); err != nil {
				return err
			}
		}
	}
	for _, keyword := range []string{"items", "additionalProperties"} {
		raw, exists := schema[keyword]
		if !exists {
			continue
		}
		if keyword == "additionalProperties" {
			if _, ok := raw.(bool); ok {
				continue
			}
		}
		node, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("schema %s must be an object%s", keyword, map[bool]string{true: " or boolean"}[keyword == "additionalProperties"])
		}
		if err := validateSchemaNode(ctx, node, depth+1); err != nil {
			return err
		}
	}
	if raw, exists := schema["uniqueItems"]; exists {
		if _, ok := raw.(bool); !ok {
			return errors.New("schema uniqueItems must be boolean")
		}
	}
	if raw, exists := schema["pattern"]; exists {
		pattern, ok := raw.(string)
		if !ok {
			return errors.New("schema pattern must be a string")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return errors.New("schema pattern is invalid")
		}
	}
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
		if raw, exists := schema[key]; exists {
			value, err := schemaNumber(raw)
			if err != nil || key == "multipleOf" && value.Sign() <= 0 {
				return fmt.Errorf("schema %s must be a finite valid number", key)
			}
		}
	}
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties"} {
		if raw, exists := schema[key]; exists {
			if _, ok := schemaInteger(raw); !ok {
				return fmt.Errorf("schema %s must be a non-negative integer", key)
			}
		}
	}
	if depth > maxSchemaDepth {
		return errors.New("input schema nesting exceeds limit")
	}
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateObject(ctx context.Context, value map[string]any, schema map[string]any, depth int) error {
	if requiredRaw, exists := schema["required"]; exists {
		required, ok := requiredRaw.([]any)
		if !ok {
			return errors.New("schema required must be an array")
		}
		for _, item := range required {
			if err := ctx.Err(); err != nil {
				return err
			}
			name, ok := item.(string)
			if !ok {
				return errors.New("schema required entries must be strings")
			}
			if _, exists := value[name]; !exists {
				return fmt.Errorf("required property %q is missing", name)
			}
		}
	}
	if err := checkCount(len(value), schema, "minProperties", "maxProperties"); err != nil {
		return err
	}
	properties := map[string]any{}
	if raw, exists := schema["properties"]; exists {
		var ok bool
		properties, ok = raw.(map[string]any)
		if !ok {
			return errors.New("schema properties must be an object")
		}
	}
	additional := schema["additionalProperties"]
	for name, entry := range value {
		if err := ctx.Err(); err != nil {
			return err
		}
		child, exists := properties[name]
		if !exists {
			switch rule := additional.(type) {
			case bool:
				if !rule {
					return fmt.Errorf("additional property %q is forbidden", name)
				}
			case map[string]any:
				if err := validateValue(ctx, entry, rule, depth+1); err != nil {
					return fmt.Errorf("property %q: %w", name, err)
				}
			}
			continue
		}
		if err := validateValue(ctx, entry, child, depth+1); err != nil {
			return fmt.Errorf("property %q: %w", name, err)
		}
	}
	return nil
}

func validateArray(ctx context.Context, value []any, schema map[string]any, depth int) error {
	if err := checkCount(len(value), schema, "minItems", "maxItems"); err != nil {
		return err
	}
	if unique, _ := schema["uniqueItems"].(bool); unique {
		seen := make(map[string]struct{}, len(value))
		for _, item := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			canonical, err := exactJSONKey(item)
			if err != nil {
				return errors.New("array item cannot be compared for uniqueness")
			}
			key := canonical
			if _, exists := seen[key]; exists {
				return errors.New("array items must be unique")
			}
			seen[key] = struct{}{}
		}
	}
	if items, exists := schema["items"]; exists {
		for i, item := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := validateValue(ctx, item, items, depth+1); err != nil {
				return fmt.Errorf("array item %d: %w", i, err)
			}
		}
	}
	return nil
}

func validateString(value string, schema map[string]any) error {
	length := utf8.RuneCountInString(value)
	if err := checkCount(length, schema, "minLength", "maxLength"); err != nil {
		return err
	}
	if patternRaw, exists := schema["pattern"]; exists {
		pattern, ok := patternRaw.(string)
		if !ok {
			return errors.New("schema pattern must be a string")
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil || !compiled.MatchString(value) {
			return errors.New("string does not match schema pattern")
		}
	}
	return nil
}

func validateNumber(value json.Number, schema map[string]any) error {
	number, err := schemaRational(value)
	if err != nil {
		return errors.New("invalid numeric value")
	}
	if raw, exists := schema["minimum"]; exists {
		bound, err := schemaNumber(raw)
		if err != nil || number.Cmp(bound) < 0 {
			return errors.New("number is below minimum")
		}
	}
	if raw, exists := schema["maximum"]; exists {
		bound, err := schemaNumber(raw)
		if err != nil || number.Cmp(bound) > 0 {
			return errors.New("number is above maximum")
		}
	}
	if raw, exists := schema["exclusiveMinimum"]; exists {
		bound, err := schemaNumber(raw)
		if err != nil || number.Cmp(bound) <= 0 {
			return errors.New("number is not above exclusiveMinimum")
		}
	}
	if raw, exists := schema["exclusiveMaximum"]; exists {
		bound, err := schemaNumber(raw)
		if err != nil || number.Cmp(bound) >= 0 {
			return errors.New("number is not below exclusiveMaximum")
		}
	}
	if raw, exists := schema["multipleOf"]; exists {
		factor, err := schemaNumber(raw)
		if err != nil || factor.Sign() <= 0 || !new(big.Rat).Quo(number, factor).IsInt() {
			return errors.New("number does not satisfy multipleOf")
		}
	}
	return nil
}

func schemaNumber(value any) (*big.Rat, error) {
	number, ok := value.(json.Number)
	if !ok {
		return nil, errors.New("schema number must be numeric")
	}
	return schemaRational(number)
}

func schemaRational(number json.Number) (*big.Rat, error) {
	text := number.String()
	if len(text) > maxJSONRPCBytes {
		return nil, errors.New("numeric value exceeds size limit")
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.ParseInt(text[index+1:], 10, 32)
		if err != nil || exponent < -10000 || exponent > 10000 {
			return nil, errors.New("numeric exponent exceeds supported range")
		}
	}
	rational, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, errors.New("invalid JSON number")
	}
	return rational, nil
}

func checkCount(count int, schema map[string]any, minimumKey, maximumKey string) error {
	if raw, exists := schema[minimumKey]; exists {
		bound, ok := schemaInteger(raw)
		if !ok || count < bound {
			return fmt.Errorf("value does not satisfy %s", minimumKey)
		}
	}
	if raw, exists := schema[maximumKey]; exists {
		bound, ok := schemaInteger(raw)
		if !ok || count > bound {
			return fmt.Errorf("value does not satisfy %s", maximumKey)
		}
	}
	return nil
}

func schemaInteger(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.Atoi(number.String())
	return parsed, err == nil && parsed >= 0
}

func valueHasType(value any, kind string) bool {
	switch kind {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		parsed, err := schemaRational(number)
		return err == nil && parsed.IsInt()
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func exactJSONKey(value any) (string, error) {
	switch current := value.(type) {
	case json.Number:
		rational, err := schemaRational(current)
		if err != nil {
			return "", err
		}
		return "number:" + rational.RatString(), nil
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var result strings.Builder
		result.WriteString("object:{")
		for _, key := range keys {
			encodedKey, _ := json.Marshal(key)
			encodedValue, err := exactJSONKey(current[key])
			if err != nil {
				return "", err
			}
			result.Write(encodedKey)
			result.WriteByte(':')
			result.WriteString(encodedValue)
			result.WriteByte(';')
		}
		result.WriteByte('}')
		return result.String(), nil
	case []any:
		var result strings.Builder
		result.WriteString("array:[")
		for _, item := range current {
			encodedItem, err := exactJSONKey(item)
			if err != nil {
				return "", err
			}
			result.WriteString(encodedItem)
			result.WriteByte(';')
		}
		result.WriteByte(']')
		return result.String(), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}
