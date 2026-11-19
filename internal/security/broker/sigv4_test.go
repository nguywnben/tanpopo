// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package broker

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignAWSSigV4UsesRequestCanonicalFormAndHostClock(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := &CryptoSignSpec{Algorithm: SigningAWSSigV4, AWSRegion: "us-east-1", AWSService: "iam", SignedHeaders: []string{"host"}, SigningTimestampEpochSec: 1}
	secret := []byte(`{"access_key_id":"AKIDEXAMPLE","secret_access_key":"wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}`)
	signed, err := signAWSSigV4(req, secret, spec, time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	const want = "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=2f497961ca12e09a21d4e6ba1c4dc91bbe42c63831ac5a251287ebc52948f3f1"
	if signed != want {
		t.Fatalf("SigV4 signature mismatch\n got: %s\nwant: %s", signed, want)
	}
	if req.Header.Get("X-Amz-Date") != "20150830T123600Z" || req.Header.Get("X-Amz-Content-Sha256") != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("missing host-generated signing headers: %#v", req.Header)
	}
	if strings.Contains(signed, "wJalr") {
		t.Fatal("secret access key leaked into Authorization header")
	}
}

func TestSignAWSSigV4RejectsMalformedCredentialsAndNonTLS(t *testing.T) {
	spec := &CryptoSignSpec{Algorithm: SigningAWSSigV4, AWSRegion: "us-east-1", AWSService: "bedrock"}
	for _, tc := range []struct {
		name string
		url  string
		key  []byte
	}{
		{name: "bad key format", url: "https://bedrock.us-east-1.amazonaws.com/model", key: []byte(`{"access_key_id":"bad key","secret_access_key":"1234567890123456"}`)},
		{name: "invalid secret document", url: "https://bedrock.us-east-1.amazonaws.com/model", key: []byte(`not-json`)},
		{name: "plaintext HTTP", url: "http://bedrock.us-east-1.amazonaws.com/model", key: []byte(`{"access_key_id":"AKID","secret_access_key":"1234567890123456"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, tc.url, strings.NewReader(`{"input":"x"}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := signAWSSigV4(req, tc.key, spec, time.Now().UTC()); err == nil {
				t.Fatal("invalid signing input accepted")
			}
		})
	}
}
