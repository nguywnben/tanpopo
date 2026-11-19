// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package broker

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

func signAWSSigV4(req *http.Request, encodedCredentials []byte, spec *CryptoSignSpec, now time.Time) (string, error) {
	if req == nil || req.URL == nil || spec == nil || spec.Algorithm != SigningAWSSigV4 || req.URL.Scheme != "https" || req.URL.Hostname() == "" {
		return "", fmt.Errorf("%w: invalid AWS SigV4 request", ErrInvalidEgressRequest)
	}
	region := strings.TrimSpace(spec.AWSRegion)
	service := strings.TrimSpace(spec.AWSService)
	if !awsScopePattern.MatchString(region) || !strings.Contains(region, "-") || !awsScopePattern.MatchString(service) {
		return "", fmt.Errorf("%w: invalid AWS SigV4 scope", ErrInvalidEgressRequest)
	}
	credentials, err := decodeAWSCredentials(encodedCredentials)
	if err != nil {
		return "", fmt.Errorf("%w: AWS signing credential must be a structured vault secret", ErrInvalidEgressRequest)
	}
	defer clear(credentials.accessKeyID)
	defer clear(credentials.secretAccessKey)
	defer clear(credentials.sessionToken)
	if !awsTokenPattern.Match(credentials.accessKeyID) || len(credentials.secretAccessKey) < 16 || !awsSecretPattern.Match(credentials.secretAccessKey) || (len(credentials.sessionToken) > 0 && !awsSessionTokenRegex.Match(credentials.sessionToken)) {
		return "", fmt.Errorf("%w: invalid AWS signing credential", ErrInvalidEgressRequest)
	}
	if now.IsZero() {
		return "", fmt.Errorf("%w: trusted signing clock is required", ErrInvalidEgressRequest)
	}
	dateTime := now.UTC().Format("20060102T150405Z")
	date := now.UTC().Format("20060102")
	var body []byte
	var bodyErr error
	if req.Body != nil {
		body, bodyErr = io.ReadAll(req.Body)
		if bodyErr != nil {
			return "", fmt.Errorf("%w: cannot read request body for signing", ErrInvalidEgressRequest)
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	payloadHash := sha256Hex(body)
	req.Header.Set("X-Amz-Date", dateTime)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if len(credentials.sessionToken) > 0 {
		req.Header.Set("X-Amz-Security-Token", string(credentials.sessionToken))
	}

	canonicalHeaders, signedHeaders, err := awsCanonicalHeaders(req, spec.SignedHeaders)
	if err != nil {
		return "", err
	}
	canonicalQuery, err := awsCanonicalQuery(req.URL.RawQuery)
	if err != nil {
		return "", err
	}
	canonicalRequest := strings.Join([]string{
		req.Method,
		awsCanonicalURI(req.URL),
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	canonicalHash := sha256Hex([]byte(canonicalRequest))
	scope := date + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + dateTime + "\n" + scope + "\n" + canonicalHash

	initialKey := append([]byte("AWS4"), credentials.secretAccessKey...)
	kDate := hmacSHA256(initialKey, []byte(date))
	clear(initialKey)
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signatureBytes := hmacSHA256(kSigning, []byte(stringToSign))
	signature := hex.EncodeToString(signatureBytes)
	clear(signatureBytes)
	clear(kDate)
	clear(kRegion)
	clear(kService)
	clear(kSigning)
	return "AWS4-HMAC-SHA256 Credential=" + string(credentials.accessKeyID) + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature, nil
}

var (
	awsTokenPattern       = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	awsScopePattern       = regexp.MustCompile(`^[a-z0-9-]+$`)
	awsSessionTokenRegex = regexp.MustCompile(`^[A-Za-z0-9/+=._-]+$`)
	awsSecretPattern     = regexp.MustCompile(`^[A-Za-z0-9/+=]+$`)
)

type awsSigV4Credentials struct {
	accessKeyID     []byte
	secretAccessKey []byte
	sessionToken    []byte
}

func decodeAWSCredentials(raw []byte) (awsSigV4Credentials, error) {
	var out awsSigV4Credentials
	index := skipJSONSpace(raw, 0)
	if index >= len(raw) || raw[index] != '{' {
		return out, fmt.Errorf("expected credential object")
	}
	index++
	seen := make(map[string]bool, 3)
	for {
		index = skipJSONSpace(raw, index)
		if index < len(raw) && raw[index] == '}' {
			index++
			break
		}
		keyBytes, next, err := readUnescapedJSONString(raw, index)
		if err != nil {
			clearAWSKeyParts(&out)
			return out, err
		}
		key := string(keyBytes)
		index = skipJSONSpace(raw, next)
		if index >= len(raw) || raw[index] != ':' || seen[key] {
			clear(keyBytes)
			clearAWSKeyParts(&out)
			return out, fmt.Errorf("invalid or duplicate credential field")
		}
		seen[key] = true
		value, next, err := readUnescapedJSONString(raw, skipJSONSpace(raw, index+1))
		clear(keyBytes)
		if err != nil {
			clearAWSKeyParts(&out)
			return out, err
		}
		switch key {
		case "access_key_id":
			out.accessKeyID = value
		case "secret_access_key":
			out.secretAccessKey = value
		case "session_token":
			out.sessionToken = value
		default:
			clear(value)
			clearAWSKeyParts(&out)
			return out, fmt.Errorf("unknown credential field")
		}
		index = skipJSONSpace(raw, next)
		if index < len(raw) && raw[index] == ',' {
			index = skipJSONSpace(raw, index+1)
			if index >= len(raw) || raw[index] == '}' {
				clearAWSKeyParts(&out)
				return out, fmt.Errorf("trailing comma in credential object")
			}
			continue
		}
		if index < len(raw) && raw[index] == '}' {
			index++
			break
		}
		clearAWSKeyParts(&out)
		return out, fmt.Errorf("malformed credential object")
	}
	if skipJSONSpace(raw, index) != len(raw) || len(out.accessKeyID) == 0 || len(out.secretAccessKey) == 0 {
		clearAWSKeyParts(&out)
		return out, fmt.Errorf("incomplete credential object")
	}
	return out, nil
}

func readUnescapedJSONString(raw []byte, start int) ([]byte, int, error) {
	if start >= len(raw) || raw[start] != '"' {
		return nil, start, fmt.Errorf("expected string")
	}
	for end := start + 1; end < len(raw); end++ {
		if raw[end] == '\\' || raw[end] < 0x20 {
			return nil, end, fmt.Errorf("escaped or control characters are not allowed in AWS credentials")
		}
		if raw[end] == '"' {
			return append([]byte(nil), raw[start+1:end]...), end + 1, nil
		}
	}
	return nil, len(raw), fmt.Errorf("unterminated string")
}

func skipJSONSpace(raw []byte, index int) int {
	for index < len(raw) && (raw[index] == ' ' || raw[index] == '\n' || raw[index] == '\r' || raw[index] == '\t') {
		index++
	}
	return index
}

func clearAWSKeyParts(credentials *awsSigV4Credentials) {
	clear(credentials.accessKeyID)
	clear(credentials.secretAccessKey)
	clear(credentials.sessionToken)
}

func awsCanonicalHeaders(req *http.Request, requested []string) (canonical, signed string, err error) {
	set := map[string]struct{}{"host": {}, "x-amz-content-sha256": {}, "x-amz-date": {}}
	if req.Header.Get("Content-Type") != "" {
		set["content-type"] = struct{}{}
	}
	if req.Header.Get("X-Amz-Security-Token") != "" {
		set["x-amz-security-token"] = struct{}{}
	}
	for _, header := range requested {
		header = strings.ToLower(strings.TrimSpace(header))
		if !isValidHTTPToken(header) || isProhibitedFramingOrHopHeader(header) || header == "authorization" {
			return "", "", fmt.Errorf("%w: invalid AWS signed header", ErrInvalidEgressRequest)
		}
		set[header] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines []string
	for _, name := range names {
		value := req.Host
		if value == "" {
			value = req.URL.Host
		}
		value = strings.ToLower(value)
		if name != "host" {
			values := req.Header.Values(name)
			if len(values) == 0 {
				return "", "", fmt.Errorf("%w: signed header %q is absent", ErrInvalidEgressRequest, name)
			}
			value = strings.Join(values, ",")
		}
		value = strings.Join(strings.Fields(value), " ")
		lines = append(lines, name+":"+value)
	}
	return strings.Join(lines, ""), strings.Join(names, ";"), nil
}

func awsCanonicalURI(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i := range segments {
		if decoded, err := url.PathUnescape(segments[i]); err == nil {
			segments[i] = awsEncode(decoded, false)
		}
	}
	return strings.Join(segments, "/")
}

func awsCanonicalQuery(raw string) (string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", fmt.Errorf("%w: malformed query string", ErrInvalidEgressRequest)
	}
	var pairs []string
	for key, items := range values {
		encodedKey := awsEncode(key, true)
		for _, item := range items {
			pairs = append(pairs, encodedKey+"="+awsEncode(item, true))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&"), nil
}

func awsEncode(value string, encodeSlash bool) string {
	const hexDigits = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || strings.ContainsRune("-_.~", rune(b)) || (!encodeSlash && b == '/') {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(hexDigits[b>>4])
			out.WriteByte(hexDigits[b&15])
		}
	}
	return out.String()
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}
