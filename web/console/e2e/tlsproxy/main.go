// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Command tlsproxy terminates ephemeral self-signed TLS on 127.0.0.1 for
// browser Playwright E2E tests (including WebKit __Host- Secure cookies).
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "HTTPS listen address")
	target := flag.String("target", "", "upstream HTTP URL (e.g. http://127.0.0.1:8080)")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "tlsproxy: --target is required")
		os.Exit(2)
	}
	u, err := url.Parse(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tlsproxy: parse target: %v\n", err)
		os.Exit(2)
	}

	cert, spkiPin, err := selfSignedLoopbackCert()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tlsproxy: generate cert: %v\n", err)
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tlsproxy: listen: %v\n", err)
		os.Exit(1)
	}
	tlsLn := tls.NewListener(ln, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"http/1.1"},
	})

	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.FlushInterval = -1
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Header.Set("X-Forwarded-Proto", "https")
	}

	srv := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf("READY https://%s %s\n", ln.Addr().String(), spkiPin)
	_ = os.Stdout.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	if err := srv.Serve(tlsLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "tlsproxy: serve: %v\n", err)
		os.Exit(1)
	}
}

func selfSignedLoopbackCert() (tls.Certificate, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	spkiDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	sum := sha256.Sum256(spkiDER)
	spkiPin := base64.StdEncoding.EncodeToString(sum[:])
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(48 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}, spkiPin, nil
}
