// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// DefaultClusterPollInterval defines ADR-0006 Layer 3 config_changelog polling interval.
	DefaultClusterPollInterval = 2 * time.Second
	// DefaultPartitionMaxStale defines FMEA-07 stale-node fail-closed threshold.
	DefaultPartitionMaxStale = 10 * time.Second
	// DefaultPGNotifyChannel is the PostgreSQL LISTEN/NOTIFY channel for config mutations.
	DefaultPGNotifyChannel = "tanpopo_config_changelog"
	// DefaultValkeyChangelogChannel matches valkey.RevocationChannel.
	DefaultValkeyChangelogChannel = "tp:v1:cluster:config_changelog"
)

var (
	// ErrClusterPartitioned indicates a node lost both PostgreSQL and Valkey connectivity beyond MaxStale.
	ErrClusterPartitioned = fmt.Errorf("%w: stale_security_state: node partitioned from PostgreSQL and Valkey > 10s", ErrSnapshotStale)
)

// PubSubBus abstracts Layer 1 distributed Pub/Sub (implemented by *valkey.Client) without
// introducing a downward dependency from Tier-4 snapshot to Tier-6 cluster packages.
type PubSubBus interface {
	Ping(ctx context.Context) error
	Publish(ctx context.Context, channel, payload string) error
	Subscribe(ctx context.Context, channel string, handler func(payload string)) error
}

// ChangeEvent is the compact JSON envelope broadcast on Layer 1 (Valkey Pub/Sub)
// and Layer 2 (PostgreSQL LISTEN/NOTIFY) whenever config_changelog advances.
type ChangeEvent struct {
	ConfigEpoch  int64  `json:"config_epoch,omitempty"`
	OrgID        string `json:"org_id,omitempty"`
	EntityKind   string `json:"entity_kind"`
	EntityID     string `json:"entity_id"`
	MutationType string `json:"mutation_type"`
	Emergency    bool   `json:"emergency,omitempty"`
}

// ClusterSyncConfig configures ADR-0006 triple-redundant revocation sync and FMEA-07 circuit breaking.
type ClusterSyncConfig struct {
	Manager            *Manager
	DB                 *sql.DB
	PGListenDSN        string // Dedicated non-pooled DSN for PostgreSQL LISTEN/NOTIFY (optional if non-PG or polling-only).
	Bus                PubSubBus
	DisableValkeyPubSub bool   // Forces Layer 2 (PG LISTEN/NOTIFY) + Layer 3 (poll) testing when true.
	ValkeyChannel      string
	PGNotifyChannel    string
	PollInterval       time.Duration
	MaxStale           time.Duration
	NowMs              func() int64
	OnWake             func(ev ChangeEvent) // Optional observer hook for telemetry/testing.
}

// ClusterSync coordinates Layer 1 (Valkey Pub/Sub), Layer 2 (PostgreSQL LISTEN/NOTIFY),
// and Layer 3 (2s config_changelog polling + >10s partitioned-node circuit breaker).
type ClusterSync struct {
	mgr              *Manager
	db               *sql.DB
	dialect          port.Dialect
	pgListenDSN      string
	bus              PubSubBus
	disableValkeySub bool
	valkeyChan       string
	pgNotifyChan     string
	pollInterval     time.Duration
	maxStaleMs       int64
	nowMs            func() int64
	onWake           func(ev ChangeEvent)

	lastPGOKMs     atomic.Int64
	lastValkeyOKMs atomic.Int64
	pgFaulted      atomic.Bool // Optional fault-injection toggle for FMEA-07 tests.
	valkeyFaulted  atomic.Bool
	running        atomic.Bool
}

// NewClusterSync initializes ClusterSync and attaches its fail-closed readiness gate
// directly onto cfg.Manager.
func NewClusterSync(cfg ClusterSyncConfig) (*ClusterSync, error) {
	if cfg.Manager == nil {
		return nil, fmt.Errorf("snapshot: ClusterSync requires non-nil Manager")
	}
	pollInt := cfg.PollInterval
	if pollInt <= 0 {
		pollInt = DefaultClusterPollInterval
	}
	maxStale := cfg.MaxStale
	if maxStale <= 0 {
		maxStale = DefaultPartitionMaxStale
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = cfg.Manager.nowMs
	}
	vkChan := strings.TrimSpace(cfg.ValkeyChannel)
	if vkChan == "" {
		vkChan = DefaultValkeyChangelogChannel
	}
	pgChan := strings.TrimSpace(cfg.PGNotifyChannel)
	if pgChan == "" {
		pgChan = DefaultPGNotifyChannel
	}
	db := cfg.DB
	if db == nil {
		db = cfg.Manager.db
	}
	var d port.Dialect = port.DialectSQLite
	if db != nil {
		d = cfg.Manager.dialect
	}

	cs := &ClusterSync{
		mgr:              cfg.Manager,
		db:               db,
		dialect:          d,
		pgListenDSN:      strings.TrimSpace(cfg.PGListenDSN),
		bus:              cfg.Bus,
		disableValkeySub: cfg.DisableValkeyPubSub,
		valkeyChan:       vkChan,
		pgNotifyChan:     pgChan,
		pollInterval:     pollInt,
		maxStaleMs:       maxStale.Milliseconds(),
		nowMs:            nowFn,
		onWake:           cfg.OnWake,
	}
	now := nowFn()
	if cfg.Manager.LastSuccessMs() > 0 {
		cs.lastPGOKMs.Store(cfg.Manager.LastSuccessMs())
	} else {
		cs.lastPGOKMs.Store(now)
	}
	if cfg.Bus != nil {
		cs.lastValkeyOKMs.Store(now)
	}
	cfg.Manager.SetClusterReadyGate(cs.CheckServingReady)
	return cs, nil
}

// Start launches Layer 1 (Valkey Pub/Sub), Layer 2 (PG LISTEN/NOTIFY), and Layer 3 (poll + heartbeat)
// until ctx is canceled.
func (cs *ClusterSync) Start(ctx context.Context) {
	if cs == nil || !cs.running.CompareAndSwap(false, true) {
		return
	}
	defer cs.running.Store(false)

	done := make(chan struct{})
	defer close(done)

	// Layer 1: Valkey Pub/Sub listener.
	if cs.bus != nil && !cs.disableValkeySub {
		go cs.runValkeySubscriberLoop(ctx)
	}
	// Layer 2: PostgreSQL LISTEN/NOTIFY listener.
	if cs.pgListenDSN != "" && cs.dialect == port.DialectPostgres {
		go cs.runPGNotifyLoop(ctx)
	}
	// Layer 3: config_changelog 2s poll + Valkey/PG heartbeat.
	cs.runPollAndHeartbeatLoop(ctx)
}

func (cs *ClusterSync) runValkeySubscriberLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if cs.valkeyFaulted.Load() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}
		_ = cs.bus.Subscribe(ctx, cs.valkeyChan, func(payload string) {
			if cs.valkeyFaulted.Load() {
				return
			}
			cs.lastValkeyOKMs.Store(cs.nowMs())
			cs.handlePayload(ctx, payload)
		})
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (cs *ClusterSync) runPGNotifyLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if cs.pgFaulted.Load() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}
		cs.listenPGOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (cs *ClusterSync) listenPGOnce(ctx context.Context) {
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	conn, err := pgx.Connect(dialCtx, cs.pgListenDSN)
	cancel()
	if err != nil {
		return
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		_ = conn.Close(closeCtx)
		closeCancel()
	}()
	// Channel name is validated internally; quote safely as identifier.
	listenSQL := "LISTEN " + pgx.Identifier{cs.pgNotifyChan}.Sanitize()
	if _, err := conn.Exec(ctx, listenSQL); err != nil {
		return
	}
	cs.lastPGOKMs.Store(cs.nowMs())
	for {
		if ctx.Err() != nil || cs.pgFaulted.Load() {
			return
		}
		waitCtx, wcancel := context.WithTimeout(ctx, cs.pollInterval)
		notif, err := conn.WaitForNotification(waitCtx)
		wcancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				if pingErr := conn.Ping(ctx); pingErr == nil && !cs.pgFaulted.Load() {
					cs.lastPGOKMs.Store(cs.nowMs())
					continue
				}
			}
			return
		}
		if cs.pgFaulted.Load() {
			return
		}
		cs.lastPGOKMs.Store(cs.nowMs())
		cs.handlePayload(ctx, notif.Payload)
	}
}

func (cs *ClusterSync) runPollAndHeartbeatLoop(ctx context.Context) {
	// Start Manager's background watcher loop on fast wakeup while driving poll ticks + pings.
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		cs.mgr.StartWatcher(ctx, cs.pollInterval)
	}()

	heartbeatInterval := cs.pollInterval
	if heartbeatInterval > 500*time.Millisecond {
		heartbeatInterval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	defer func() { <-watcherDone }()

	lastPoll := cs.nowMs()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cs.ProbeBackends(ctx)
			now := cs.nowMs()
			if now-lastPoll >= cs.pollInterval.Milliseconds() {
				lastPoll = now
				if !cs.pgFaulted.Load() {
					if err := cs.mgr.Refresh(ctx); err == nil {
						cs.lastPGOKMs.Store(cs.nowMs())
					}
				}
			}
		}
	}
}

// ProbeBackends checks both PostgreSQL and Valkey health and updates monotonic connectivity timestamps.
func (cs *ClusterSync) ProbeBackends(ctx context.Context) {
	if cs == nil {
		return
	}
	if cs.db != nil && !cs.pgFaulted.Load() {
		pctx, cancel := context.WithTimeout(ctx, time.Second)
		if err := cs.mgr.Refresh(pctx); err == nil {
			cs.lastPGOKMs.Store(cs.nowMs())
		}
		cancel()
	}
	if cs.bus != nil && !cs.valkeyFaulted.Load() {
		vctx, cancel := context.WithTimeout(ctx, time.Second)
		if err := cs.bus.Ping(vctx); err == nil {
			cs.lastValkeyOKMs.Store(cs.nowMs())
		}
		cancel()
	}
}

// SetFaulted simulates network partition fault injection against PostgreSQL and/or Valkey for FMEA-07 testing.
func (cs *ClusterSync) SetFaulted(pgDown, valkeyDown bool) {
	if cs == nil {
		return
	}
	cs.pgFaulted.Store(pgDown)
	cs.valkeyFaulted.Store(valkeyDown)
}

// IsPartitioned returns true when the node has exceeded MaxStale without contact to PostgreSQL
// AND (if configured) Valkey.
func (cs *ClusterSync) IsPartitioned() bool {
	if cs == nil {
		return false
	}
	now := cs.nowMs()
	pgAge := now - cs.lastPGOKMs.Load()
	pgDown := pgAge > cs.maxStaleMs || cs.lastPGOKMs.Load() == 0
	if cs.bus == nil {
		return pgDown
	}
	vkAge := now - cs.lastValkeyOKMs.Load()
	vkDown := vkAge > cs.maxStaleMs || cs.lastValkeyOKMs.Load() == 0
	return pgDown && vkDown
}

// CheckServingReady enforces the FMEA-07 > 10s stale-node fail-closed breaker.
func (cs *ClusterSync) CheckServingReady() error {
	if cs == nil {
		return nil
	}
	if cs.IsPartitioned() {
		return ErrClusterPartitioned
	}
	return nil
}

// PublishChange broadcasts a config_changelog mutation across BOTH Valkey Pub/Sub (Layer 1)
// and PostgreSQL NOTIFY (Layer 2). Errors are returned only if both channels fail.
func (cs *ClusterSync) PublishChange(ctx context.Context, ev ChangeEvent) error {
	if cs == nil {
		return nil
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("snapshot: marshal ChangeEvent: %w", err)
	}
	payload := string(raw)
	var vkErr, pgErr error
	if cs.bus != nil {
		vkErr = cs.bus.Publish(ctx, cs.valkeyChan, payload)
	}
	if cs.db != nil && cs.dialect == port.DialectPostgres {
		_, pgErr = cs.db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, cs.pgNotifyChan, payload)
	}
	if vkErr != nil && pgErr != nil {
		return errors.Join(vkErr, pgErr)
	}
	if vkErr != nil && (cs.db == nil || cs.dialect != port.DialectPostgres) {
		return vkErr
	}
	return pgErr
}

// PublishPGNotify emits a Layer 2 PostgreSQL NOTIFY directly using caller DB/TX.
func PublishPGNotify(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, ev ChangeEvent) error {
	if db == nil {
		return nil
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, DefaultPGNotifyChannel, string(raw))
	return err
}

func (cs *ClusterSync) handlePayload(ctx context.Context, payload string) {
	var ev ChangeEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		cs.mgr.Notify()
		return
	}
	nowMs := cs.nowMs()
	switch {
	case ev.EntityKind == "virtual_key" && (ev.MutationType == port.MutationRevoke || ev.Emergency):
		cs.mgr.FenceKeyRevocation(ev.EntityID, "EMERGENCY", nowMs, ev.ConfigEpoch)
	case ev.EntityKind == "virtual_key" && ev.MutationType == port.MutationShred:
		cs.mgr.FenceInvalidateKeyDigest(ev.EntityID, nowMs, ev.ConfigEpoch)
	case ev.EntityKind == "project" && ev.MutationType == port.MutationShred && ev.OrgID != "":
		cs.mgr.Streams().AbortScope(ev.OrgID, ev.EntityID)
	case ev.EntityKind == "organization" && ev.MutationType == port.MutationShred:
		cs.mgr.Streams().AbortScope(ev.EntityID, "")
	case ev.EntityKind == "emergency_kill_switch":
		cs.mgr.killSwitchSyncDirty.Store(true)
	}
	if cs.onWake != nil {
		cs.onWake(ev)
	}
	_ = cs.mgr.Refresh(ctx)
	cs.mgr.Notify()
}
