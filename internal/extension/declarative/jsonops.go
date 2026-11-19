// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package declarative

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type stepBudget struct {
	remaining int
}

func newStepBudget() *stepBudget {
	return &stepBudget{remaining: MaxOperatorSteps}
}

func (b *stepBudget) consume(steps int) error {
	b.remaining -= steps
	if b.remaining < 0 {
		return ErrOperatorBudgetExceeded
	}
	return nil
}

// ValidateJSONDepth checks JSON container nesting depth in O(1) auxiliary memory without recursion,
// rejecting payloads exceeding maxDepth before any heap tree allocation occurs (NEG-1 / FZ-01).
func ValidateJSONDepth(data []byte, maxDepth int) error {
	if len(data) > MaxPayloadBytes {
		return fmt.Errorf("%w: json payload %d bytes exceeds max %d", ErrInvalidDeclarativeSpec, len(data), MaxPayloadBytes)
	}
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxDepth {
				return fmt.Errorf("%w (depth %d > %d)", ErrJSONDepthExceeded, depth, maxDepth)
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return fmt.Errorf("%w: unbalanced closing JSON delimiter", ErrInvalidDeclarativeSpec)
			}
		}
	}
	if inString || depth != 0 {
		return fmt.Errorf("%w: unterminated JSON value", ErrInvalidDeclarativeSpec)
	}
	return nil
}

func decodeBoundedJSONMap(data []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	if err := ValidateJSONDepth(trimmed, MaxJSONDepth); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: json decode: %v", ErrInvalidDeclarativeSpec, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing JSON tokens prohibited", ErrInvalidDeclarativeSpec)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func getByPath(root any, p parsedPath, budget *stepBudget) (any, bool, error) {
	if len(p.tokens) == 0 {
		return nil, false, nil
	}
	cur := root
	for _, tok := range p.tokens {
		if err := budget.consume(1); err != nil {
			return nil, false, err
		}
		if cur == nil {
			return nil, false, nil
		}
		if tok.isIndex {
			sliceVal, ok := cur.([]any)
			if !ok || tok.index < 0 || tok.index >= len(sliceVal) {
				return nil, false, nil
			}
			cur = sliceVal[tok.index]
			continue
		}
		mapVal, ok := cur.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		next, exists := mapVal[tok.key]
		if !exists {
			return nil, false, nil
		}
		cur = next
	}
	return cur, true, nil
}

func setByPath(root map[string]any, p parsedPath, val any, overwrite bool, budget *stepBudget) error {
	if len(p.tokens) == 0 {
		return nil
	}
	if p.tokens[0].isIndex {
		return fmt.Errorf("%w: root segment %q cannot be array index", ErrInvalidDeclarativeSpec, p.raw)
	}
	cur := root
	for i := 0; i < len(p.tokens)-1; i++ {
		if err := budget.consume(1); err != nil {
			return err
		}
		tok := p.tokens[i]
		if tok.isIndex {
			return fmt.Errorf("%w: intermediate array index write unsupported in %q", ErrInvalidDeclarativeSpec, p.raw)
		}
		child, exists := cur[tok.key]
		if !exists || child == nil {
			nextMap := make(map[string]any)
			cur[tok.key] = nextMap
			cur = nextMap
			continue
		}
		nextMap, ok := child.(map[string]any)
		if !ok {
			if !overwrite {
				return nil
			}
			nextMap = make(map[string]any)
			cur[tok.key] = nextMap
		}
		cur = nextMap
	}
	if err := budget.consume(1); err != nil {
		return err
	}
	last := p.tokens[len(p.tokens)-1]
	if last.isIndex {
		return fmt.Errorf("%w: terminal array index write unsupported in %q", ErrInvalidDeclarativeSpec, p.raw)
	}
	if !overwrite {
		if _, exists := cur[last.key]; exists {
			return nil
		}
	}
	cur[last.key] = deepCloneJSONSafe(val)
	return nil
}

func deleteByPath(root map[string]any, p parsedPath, budget *stepBudget) error {
	if len(p.tokens) == 0 {
		return nil
	}
	cur := root
	for i := 0; i < len(p.tokens)-1; i++ {
		if err := budget.consume(1); err != nil {
			return err
		}
		tok := p.tokens[i]
		if tok.isIndex {
			return nil
		}
		next, ok := cur[tok.key].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	if err := budget.consume(1); err != nil {
		return err
	}
	last := p.tokens[len(p.tokens)-1]
	if !last.isIndex {
		delete(cur, last.key)
	}
	return nil
}

func deepCloneJSONSafe(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		cp := make(map[string]any, len(tv))
		for k, val := range tv {
			cp[k] = deepCloneJSONSafe(val)
		}
		return cp
	case []any:
		cp := make([]any, len(tv))
		for i, val := range tv {
			cp[i] = deepCloneJSONSafe(val)
		}
		return cp
	default:
		return v
	}
}

func extractInt64(v any) (int64, bool) {
	switch tv := v.(type) {
	case json.Number:
		if n, err := tv.Int64(); err == nil {
			return n, true
		}
		if f, err := tv.Float64(); err == nil {
			return int64(f), true
		}
	case float64:
		return int64(tv), true
	case int64:
		return tv, true
	case int:
		return int64(tv), true
	}
	return 0, false
}

func extractString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func extractBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func renderEndpointURL(baseURL string, parts []templatePart, implicitOpenAIPath bool, vars map[string]string, allowedHosts map[string]struct{}, suffixes []string, budget *stepBudget) (string, error) {
	baseParsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (baseParsed.Scheme != "http" && baseParsed.Scheme != "https") || baseParsed.Hostname() == "" || baseParsed.User != nil {
		return "", fmt.Errorf("%w: invalid endpoint_base_url %q", ErrInvalidDeclarativeSpec, baseURL)
	}
	if len(allowedHosts) > 0 || len(suffixes) > 0 {
		hostname := strings.ToLower(baseParsed.Hostname())
		hostPort := strings.ToLower(baseParsed.Host)
		_, okHost := allowedHosts[hostname]
		_, okHostPort := allowedHosts[hostPort]
		if !okHost && !okHostPort && !hostMatchesSuffix(hostname, suffixes) {
			return "", fmt.Errorf("%w: endpoint host %q is outside adapter allowedHosts", ErrInvalidDeclarativeSpec, baseParsed.Host)
		}
	}

	var relBuilder strings.Builder
	for _, part := range parts {
		if err := budget.consume(1); err != nil {
			return "", err
		}
		if !part.isVar {
			relBuilder.WriteString(part.literal)
			continue
		}
		val, ok := vars[part.varName]
		if !ok || strings.TrimSpace(val) == "" {
			return "", fmt.Errorf("%w: missing required template variable %q", ErrInvalidDeclarativeSpec, part.varName)
		}
		if strings.ContainsAny(val, "{}$\r\n\x00@\\") || strings.Contains(val, "..") {
			return "", fmt.Errorf("%w: unsafe template variable value for %q", ErrRecursiveTemplate, part.varName)
		}
		relBuilder.WriteString(url.PathEscape(val))
	}

	relStr := relBuilder.String()
	if implicitOpenAIPath && strings.HasSuffix(strings.TrimRight(baseParsed.EscapedPath(), "/"), "/v1") {
		relStr = strings.TrimPrefix(relStr, "/v1")
	}
	if strings.Contains(relStr, "://") || strings.HasPrefix(relStr, "//") || strings.Contains(relStr, "@") {
		return "", fmt.Errorf("%w: rendered relative path cannot alter authority", ErrInvalidDeclarativeSpec)
	}
	if strings.TrimSpace(relStr) == "" {
		return baseParsed.String(), nil
	}

	relParsed, err := url.Parse(relStr)
	if err != nil || relParsed.IsAbs() || relParsed.Host != "" || relParsed.User != nil {
		return "", fmt.Errorf("%w: invalid rendered relative path %q", ErrInvalidDeclarativeSpec, relStr)
	}

	joined := *baseParsed
	basePath := strings.TrimRight(baseParsed.Path, "/")
	relPath := relParsed.Path
	if relPath != "" && !strings.HasPrefix(relPath, "/") {
		relPath = "/" + relPath
	}
	joined.Path = basePath + relPath
	baseEscaped := strings.TrimRight(baseParsed.EscapedPath(), "/")
	relEscaped := relParsed.EscapedPath()
	if relEscaped != "" && !strings.HasPrefix(relEscaped, "/") {
		relEscaped = "/" + relEscaped
	}
	joined.RawPath = baseEscaped + relEscaped
	if relParsed.RawQuery != "" {
		if joined.RawQuery != "" {
			joined.RawQuery = joined.RawQuery + "&" + relParsed.RawQuery
		} else {
			joined.RawQuery = relParsed.RawQuery
		}
	}
	joined.Fragment = ""

	if strings.ToLower(joined.Hostname()) != strings.ToLower(baseParsed.Hostname()) {
		return "", fmt.Errorf("%w: resolved target host changed unexpectedly", ErrInvalidDeclarativeSpec)
	}
	return joined.String(), nil
}
