// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package valkey implements Valkey Cluster coordination, cluster hash-tag safe
// Lua rate limiting and budget pre-hold scripts (CR-07), 1s valkey_hold_checkpoints
// flushing, Pub/Sub invalidation, and DR-02 state rebuild from PostgreSQL.
package valkey

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

// Canonical cluster keyspace prefix and errors (CR-07).
const (
	KeyspacePrefix    = "tp:v1"
	RevocationChannel = "tp:v1:cluster:config_changelog"
)

var (
	ErrCrossOrgKeyViolation = errors.New("valkey: multi-key operation spans distinct {org_id} hash tags")
	ErrInvalidKeyComponent  = errors.New("valkey: invalid key component")
	ErrTLSRequired          = errors.New("valkey: TLS is required for non-loopback Valkey endpoints when RequireTLS is true")
)

// Config configures a standalone or clustered Valkey client.
type Config struct {
	Addrs        []string
	Username     string
	Password     string
	RequireTLS   bool
	TLSConfig    *tls.Config
	DialTimeout  time.Duration
	DisableCache bool
	Now          func() time.Time
}

// Client wraps github.com/valkey-io/valkey-go with strict CR-07 hash-tag helpers
// and Pub/Sub publish/subscribe primitives.
type Client struct {
	raw   valkeygo.Client
	addrs []string
	now   func() time.Time

	closeOnce sync.Once
}

// Dial connects to standalone or clustered Valkey endpoints.
func Dial(cfg Config) (*Client, error) {
	addrs := normalizeAddrs(cfg.Addrs)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: at least one Valkey address is required", ErrInvalidKeyComponent)
	}
	var tlsCfg *tls.Config
	if cfg.TLSConfig != nil {
		tlsCfg = cfg.TLSConfig.Clone()
	} else if cfg.RequireTLS {
		if !allLoopback(addrs) {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
	}
	if cfg.RequireTLS && tlsCfg == nil {
		return nil, ErrTLSRequired
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 2 * time.Second
	}
	opts := valkeygo.ClientOption{
		InitAddress:       addrs,
		Username:          cfg.Username,
		Password:          cfg.Password,
		TLSConfig:         tlsCfg,
		DisableCache:      true, // Mandatory to prevent stale client-side cache reads across Lua/cluster slots.
		Dialer:            net.Dialer{Timeout: dialTimeout, Control: valkeySocketControl},
		ConnWriteTimeout:  3 * time.Second,
		AlwaysPipelining:  false,
		ShuffleInit:       len(addrs) > 1,
		ForceSingleClient: false,
	}
	if !cfg.DisableCache {
		opts.DisableCache = true
	}
	raw, err := valkeygo.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("valkey: dial %v: %w", addrs, err)
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Client{
		raw:   raw,
		addrs: addrs,
		now:   nowFn,
	}, nil
}

func valkeySocketControl(_ string, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("valkey: reject invalid dial target %q: %w", address, err)
	}
	addr := ap.Addr().Unmap()
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return fmt.Errorf("valkey: refuse link-local/metadata/multicast socket target %s", addr)
	}
	return nil
}

// Raw returns the underlying valkey-go client for advanced tests.
func (c *Client) Raw() valkeygo.Client {
	if c == nil {
		return nil
	}
	return c.raw
}

// Close closes all underlying Valkey connections cleanly.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		if c.raw != nil {
			c.raw.Close()
		}
	})
}

// Ping verifies live connectivity to the Valkey node or cluster.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return errors.New("valkey: nil client")
	}
	return c.raw.Do(ctx, c.raw.B().Ping().Build()).Error()
}

// Publish emits an invalidation payload on a Pub/Sub channel.
func (c *Client) Publish(ctx context.Context, channel, payload string) error {
	if c == nil || c.raw == nil {
		return errors.New("valkey: nil client")
	}
	if strings.TrimSpace(channel) == "" {
		return fmt.Errorf("%w: empty channel", ErrInvalidKeyComponent)
	}
	return c.raw.Do(ctx, c.raw.B().Publish().Channel(channel).Message(payload).Build()).Error()
}

// Subscribe streams Pub/Sub messages on channel until ctx is canceled.
func (c *Client) Subscribe(ctx context.Context, channel string, handler func(payload string)) error {
	if c == nil || c.raw == nil {
		return errors.New("valkey: nil client")
	}
	if strings.TrimSpace(channel) == "" || handler == nil {
		return fmt.Errorf("%w: channel and handler are required", ErrInvalidKeyComponent)
	}
	return c.raw.Receive(ctx, c.raw.B().Subscribe().Channel(channel).Build(), func(msg valkeygo.PubSubMessage) {
		handler(msg.Message)
	})
}

// ValidateOrgID checks that orgID cannot inject extra Valkey hash tags or delimiters.
func ValidateOrgID(orgID string) error {
	return validateKeySegment("org_id", orgID, false)
}

func validateKeySegment(label, val string, allowColon bool) error {
	if val == "" || len(val) > 128 {
		return fmt.Errorf("%w: invalid %s length", ErrInvalidKeyComponent, label)
	}
	for i := 0; i < len(val); i++ {
		b := val[i]
		if b <= 0x20 || b == 0x7f || b == '{' || b == '}' {
			return fmt.Errorf("%w: forbidden character in %s", ErrInvalidKeyComponent, label)
		}
		if !allowColon && b == ':' {
			return fmt.Errorf("%w: ':' forbidden in %s", ErrInvalidKeyComponent, label)
		}
	}
	return nil
}

// Key constructs a hash-slot-safe Valkey key following CR-07 (`tp:v1:{<org_id>}:<subsystem>:<entity>`).
func Key(orgID, subsystem, entity string) (string, error) {
	if err := ValidateOrgID(orgID); err != nil {
		return "", err
	}
	if err := validateKeySegment("subsystem", subsystem, false); err != nil {
		return "", err
	}
	if err := validateKeySegment("entity", entity, true); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:{%s}:%s:%s", KeyspacePrefix, orgID, subsystem, entity), nil
}

// ExtractHashTag returns the CRC16 cluster hash tag substring inside `{...}` per Valkey Cluster spec.
func ExtractHashTag(key string) (string, bool) {
	open := strings.IndexByte(key, '{')
	if open < 0 {
		return "", false
	}
	closeRel := strings.IndexByte(key[open+1:], '}')
	if closeRel <= 0 {
		return "", false
	}
	return key[open+1 : open+1+closeRel], true
}

// AssertSameHashTag enforces CR-07: every multi-key command or Lua EVAL MUST touch
// keys sharing the exact same non-empty `{<org_id>}` cluster hash tag.
func AssertSameHashTag(keys ...string) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: at least one key is required", ErrInvalidKeyComponent)
	}
	var expected string
	for i, k := range keys {
		tag, ok := ExtractHashTag(k)
		if !ok || tag == "" {
			return fmt.Errorf("%w: key %q missing {org_id} hash tag", ErrCrossOrgKeyViolation, k)
		}
		if i == 0 {
			expected = tag
			continue
		}
		if tag != expected {
			return fmt.Errorf("%w: key %q hash tag {%s} differs from {%s}", ErrCrossOrgKeyViolation, k, tag, expected)
		}
	}
	return nil
}

// EvalLua executes a Lua script after strictly validating all keys share the same `{<org_id>}` hash tag.
func (c *Client) EvalLua(ctx context.Context, script string, keys []string, args ...string) (valkeygo.ValkeyResult, error) {
	if c == nil || c.raw == nil {
		return valkeygo.ValkeyResult{}, errors.New("valkey: nil client")
	}
	if err := AssertSameHashTag(keys...); err != nil {
		return valkeygo.ValkeyResult{}, err
	}
	cmd := c.raw.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build()
	res := c.raw.Do(ctx, cmd)
	return res, res.Error()
}

// IsCrossSlotError checks if Valkey returned a CROSSSLOT error (which should never be masked).
func IsCrossSlotError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToUpper(err.Error()), "CROSSSLOT")
}

func normalizeAddrs(raw []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out
}

func allLoopback(addrs []string) bool {
	for _, addr := range addrs {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		host = strings.Trim(host, "[]")
		if host == "localhost" {
			continue
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}
