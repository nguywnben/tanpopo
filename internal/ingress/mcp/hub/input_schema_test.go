// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateArgumentsEnforcesNestedSchemaAndFailsClosed(t *testing.T) {
	t.Parallel()
	stored := json.RawMessage(`{"name":"search","description":"safe","inputSchema":{"type":"object","properties":{"query":{"type":"string","minLength":2},"limit":{"type":"integer","minimum":1,"maximum":20}},"required":["query"],"additionalProperties":false}}`)
	if err := validateArguments(stored, json.RawMessage(`{"query":"hi","limit":3}`)); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
	for _, args := range []string{
		`{"limit":3}`, `{"query":1}`, `{"query":"x"}`, `{"query":"hi","limit":0}`,
		`{"query":"hi","extra":true}`, `[]`, `{"query":"hi"} {}`, `{"query":"safe","query":"other"}`,
	} {
		if err := validateArguments(stored, json.RawMessage(args)); err == nil {
			t.Errorf("invalid arguments accepted: %s", args)
		}
	}
	unsupported := json.RawMessage(`{"inputSchema":{"type":"object","oneOf":[{"type":"object"}]}}`)
	if err := validateArguments(unsupported, json.RawMessage(`{}`)); err == nil {
		t.Fatal("unsupported schema keyword did not fail closed")
	}
	for _, schema := range []string{
		`{"inputSchema":{"type":["object","null"]}}`,
		`{"inputSchema":{"type":"object","properties":{"unused":true}}}`,
		`{"inputSchema":{"type":"object","additionalProperties":"yes"}}`,
		`{"inputSchema":{"type":"array","items":{"type":7}}}`,
	} {
		if err := validateArguments(json.RawMessage(schema), json.RawMessage(`{}`)); err == nil {
			t.Errorf("malformed schema was accepted: %s", schema)
		}
	}
}

func TestValidateArgumentsStopsWhenRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stored := json.RawMessage(`{"inputSchema":{"type":"object","properties":{"values":{"type":"array","uniqueItems":true,"items":{"type":"string"}}}}}`)
	if err := validateArgumentsContext(ctx, stored, json.RawMessage(`{"values":["one","two"]}`)); err != context.Canceled {
		t.Fatalf("cancelled validation error=%v, want context.Canceled", err)
	}
}

func TestRejectDuplicateJSONKeysBoundsNesting(t *testing.T) {
	raw := strings.Repeat("[", maxSchemaDepth+2) + `0` + strings.Repeat("]", maxSchemaDepth+2)
	if err := rejectDuplicateJSONKeys([]byte(raw)); err == nil {
		t.Fatal("deeply nested JSON exceeded the recursion limit without rejection")
	}
	allowed := strings.Repeat("[", maxSchemaDepth) + `0` + strings.Repeat("]", maxSchemaDepth)
	if err := ValidateJSON([]byte(allowed)); err != nil {
		t.Fatalf("exactly %d composite levels rejected: %v", maxSchemaDepth, err)
	}
	tooDeep := strings.Repeat("[", maxSchemaDepth+1) + `0` + strings.Repeat("]", maxSchemaDepth+1)
	if err := ValidateJSON([]byte(tooDeep)); err == nil {
		t.Fatal("JSON with one level beyond the limit was accepted")
	}
}

func TestValidateMCPResponseJSONAllowsDuplicatesOnlyInInputSchema(t *testing.T) {
	if err := ValidateMCPResponseJSON([]byte(`{"jsonrpc":"2.0","result":{"tools":[{"inputSchema":{"type":"object","type":"array"}}]}}`)); err != nil {
		t.Fatalf("duplicate inputSchema key should reach schema validation: %v", err)
	}
	for _, raw := range []string{
		`{"jsonrpc":"2.0","jsonrpc":"1.0","result":{}}`,
		`{"jsonrpc":"2.0","result":{"tools":[{"name":"one","name":"two","inputSchema":{}}]}}`,
	} {
		if err := ValidateMCPResponseJSON([]byte(raw)); err == nil {
			t.Errorf("duplicate response envelope/catalog key accepted: %s", raw)
		}
	}
}

func TestValidateArgumentsUsesExactJSONNumberConstraints(t *testing.T) {
	t.Parallel()
	stored := json.RawMessage(`{"inputSchema":{"type":"object","properties":{"count":{"type":"integer","maximum":9007199254740992},"fraction":{"type":"number","multipleOf":0.1}}}}`)
	if err := validateArguments(stored, json.RawMessage(`{"count":9007199254740992,"fraction":0.3}`)); err != nil {
		t.Fatalf("valid boundary and exact multiple rejected: %v", err)
	}
	if err := validateArguments(stored, json.RawMessage(`{"count":9007199254740993,"fraction":0.3}`)); err == nil {
		t.Fatal("integer above the exact maximum was accepted")
	}
	if err := validateArguments(stored, json.RawMessage(`{"count":9007199254740992,"fraction":0.31}`)); err == nil {
		t.Fatal("number that is not a multiple of 0.1 was accepted")
	}
	if err := validateArguments(json.RawMessage(`{"inputSchema":{"type":"object","properties":{"count":{"type":"integer"}}}}`), json.RawMessage(`{"count":1.0}`)); err != nil {
		t.Fatalf("mathematically integral decimal was rejected: %v", err)
	}
}

func TestEnumConstAndUniqueItemsCompareLargeNumbersExactly(t *testing.T) {
	const high = `9007199254740992`
	const next = `9007199254740993`
	if err := validateArguments(json.RawMessage(`{"inputSchema":{"type":"object","properties":{"n":{"enum":[`+high+`]}}}}`), json.RawMessage(`{"n":`+next+`}`)); err == nil {
		t.Fatal("enum accepted a distinct integer above IEEE-754 exact range")
	}
	if err := validateArguments(json.RawMessage(`{"inputSchema":{"type":"object","properties":{"n":{"const":`+high+`}}}}`), json.RawMessage(`{"n":`+next+`}`)); err == nil {
		t.Fatal("const accepted a distinct integer above IEEE-754 exact range")
	}
	uniqueSchema := json.RawMessage(`{"inputSchema":{"type":"object","properties":{"n":{"type":"array","uniqueItems":true,"items":{"type":"integer"}}}}}`)
	if err := validateArguments(uniqueSchema, json.RawMessage(`{"n":[`+high+`,`+next+`]}`)); err != nil {
		t.Fatalf("distinct large integers were treated as duplicate array items: %v", err)
	}
}
