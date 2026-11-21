// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	adminhttp "github.com/nguywnben/tanpopo/internal/admin/http"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/registry"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	mcphub "github.com/nguywnben/tanpopo/internal/ingress/mcp/hub"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/oauth"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	telemetry "github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/webhook"
	"github.com/nguywnben/tanpopo/internal/webui"
)

func newServeCommand(loc i18n.Locale) *cobra.Command {
	role := "all"
	listen := envOr("TANPOPO_LISTEN_ADDR", "127.0.0.1:8080")
	cmd := &cobra.Command{
		Use:   "serve",
		Short: i18n.T(loc, "cli.serve.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if role != "all" && role != "data-plane" && role != "control-plane" {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: i18n.T(loc, "cli.serve.err.invalid_role", "role", fmt.Sprintf("%q", role))}
			}
			if _, err := drainTimeoutFromEnv(os.LookupEnv); err != nil {
				return &testharness.ExitError{Code: testharness.ExitValidation, Message: i18n.T(loc, "cli.serve.err.invalid_drain"), Cause: err}
			}
			if err := serve(cmd.Context(), role, listen); err != nil {
				return &testharness.ExitError{Code: testharness.ExitInternal, Message: i18n.T(loc, "cli.serve.err.failed"), Cause: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", role, i18n.T(loc, "cli.serve.flag.role"))
	cmd.Flags().StringVar(&listen, "listen", listen, i18n.T(loc, "cli.serve.flag.listen"))
	return cmd
}

func serve(ctx context.Context, role, listen string) (serveErr error) {
	drainTimeout, err := drainTimeoutFromEnv(os.LookupEnv)
	if err != nil {
		return err
	}
	handler, closeRuntime, err := buildRuntime(ctx, role, listen)
	if err != nil {
		return err
	}
	defer func() { serveErr = errors.Join(serveErr, closeRuntime()) }()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

const defaultDrainTimeout = 90 * time.Second
const maxDrainTimeout = 300 * time.Second

func drainTimeoutFromEnv(lookup func(string) (string, bool)) (time.Duration, error) {
	raw, ok := lookup("TANPOPO_DRAIN_TIMEOUT")
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultDrainTimeout, nil
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || timeout < 0 || timeout > maxDrainTimeout {
		return 0, fmt.Errorf("TANPOPO_DRAIN_TIMEOUT must be between 0s and %s", maxDrainTimeout)
	}
	return timeout, nil
}

func buildRuntime(ctx context.Context, role, listen string) (http.Handler, func() error, error) {
	return buildRuntimeWithBeforeSQLiteClose(ctx, role, listen, nil)
}

// buildRuntimeWithBeforeSQLiteClose exposes the last quiescent SQLite state to
// shutdown tests. Production callers use buildRuntime and install no observer.
func buildRuntimeWithBeforeSQLiteClose(ctx context.Context, role, listen string, beforeSQLiteClose func(*sql.DB)) (http.Handler, func() error, error) {
	if role != "all" && role != "data-plane" && role != "control-plane" {
		return nil, nil, fmt.Errorf("invalid service role %q", role)
	}
	dataDir := envOr("TANPOPO_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("secure data directory: %w", err)
	}
	pluginsDir := envOr("TANPOPO_PLUGINS_DIR", filepath.Join(dataDir, "plugins"))
	if err := os.MkdirAll(pluginsDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create plugins directory: %w", err)
	}
	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_PG_DSN"))
	storageEngine := strings.ToLower(strings.TrimSpace(os.Getenv("TANPOPO_STORAGE_ENGINE")))
	switch storageEngine {
	case "", "sqlite", "postgres":
	default:
		return nil, nil, fmt.Errorf("unsupported TANPOPO_STORAGE_ENGINE %q: expected sqlite or postgres", storageEngine)
	}
	if storageEngine == "postgres" || (storageEngine == "" && pgDSN != "") {
		pgStore, openErr := postgres.Open(ctx, postgres.Options{
			DSN:                      pgDSN,
			SkipMigrate:              true,
			PartitionLookaheadMonths: -1,
		})
		if openErr != nil {
			return nil, nil, fmt.Errorf("open PostgreSQL store: %w", openErr)
		}
		_ = pgStore.Close()
		return nil, nil, errors.New("tanpopo serve PostgreSQL runtime serving is gated until TP-IMP-0040 cluster cutover completes FORCE RLS across all control/data-plane stores; use SQLite WAL for serving and `tanpopo db migrate-engine` / `tanpopo doctor` to validate PostgreSQL 16+")
	}
	dbPath := envOr("TANPOPO_DB_PATH", filepath.Join(dataDir, "tanpopo.db"))
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		return nil, nil, fmt.Errorf("open local SQLite store: %w", err)
	}
	closeStore := func() { _ = store.Close() }
	db := store.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("initialize audit logger: %w", err)
	}
	var ledgerService *ledger.Service
	kek, err := vault.LoadStaticKEKFromEnv("kek_v1", os.LookupEnv)
	if err != nil {
		closeStore()
		return nil, nil, fmt.Errorf("load Master KEK: %w", err)
	}
	closeKeys := func() { kek.Close() }
	vaultStore, err := vault.NewStore(vault.Config{DB: db, KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, fmt.Errorf("initialize vault: %w", err)
	}
	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: db})
	if err != nil {
		closeKeys()
		closeStore()
		return nil, nil, fmt.Errorf("initialize configuration snapshot: %w", err)
	}
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	watcherDone := make(chan struct{})
	var webhookDone chan struct{}
	var closeWebhookDispatcher func() error
	var webhookDispatcher *webhook.Dispatcher
	var approvalMaintenanceDone chan struct{}
	var marketplaceDone <-chan struct{}
	go func() {
		defer close(watcherDone)
		snaps.StartWatcher(runtimeCtx, time.Second)
	}()
	var closeDataServer func() error
	var dataServerReady func(context.Context) error
	var telemetryPipeline *telemetry.Pipeline
	var cleanupOnce sync.Once
	var shutdownErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			lastActiveDBStage := ""
			lastActiveDBConnections := 0
			recordActiveDBConnections := func(stage string) {
				if inUse := db.Stats().InUse; inUse > 0 {
					lastActiveDBStage = stage
					lastActiveDBConnections = inUse
				}
			}
			recordActiveDBConnections("shutdown start")
			if closeDataServer != nil {
				shutdownErr = errors.Join(shutdownErr, closeDataServer())
				recordActiveDBConnections("after data server close")
			}
			if telemetryPipeline != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				shutdownErr = errors.Join(shutdownErr, telemetryPipeline.Shutdown(shutdownCtx))
				cancel()
				recordActiveDBConnections("after telemetry shutdown")
			}
			if webhookDispatcher != nil {
				webhookDispatcher.StopClaiming()
			}
			cancelRuntime()
			if webhookDone != nil {
				<-webhookDone
			}
			if closeWebhookDispatcher != nil {
				shutdownErr = errors.Join(shutdownErr, closeWebhookDispatcher())
			}
			if approvalMaintenanceDone != nil {
				<-approvalMaintenanceDone
			}
			<-watcherDone
			recordActiveDBConnections("after snapshot watcher shutdown")
			if marketplaceDone != nil {
				<-marketplaceDone
				recordActiveDBConnections("after marketplace watcher shutdown")
			}
			vaultStore.Close()
			closeKeys()
			recordActiveDBConnections("before SQLite close")
			if beforeSQLiteClose != nil {
				beforeSQLiteClose(db)
			}
			if err := store.Close(); err != nil {
				if lastActiveDBStage != "" {
					shutdownErr = errors.Join(shutdownErr, fmt.Errorf("sqlite: last observed %d active connection(s) at %s", lastActiveDBConnections, lastActiveDBStage))
				}
				shutdownErr = errors.Join(shutdownErr, err)
			}
		})
		return shutdownErr
	}
	catalogRepo, err := catalog.NewRepository(catalog.Config{DB: db, Snapshots: snaps, Audit: auditLog})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize provider catalog: %w", err)
	}
	redirectURI := strings.TrimSpace(os.Getenv("TANPOPO_OAUTH_REDIRECT_URI"))
	allowHermeticHTTP := strings.TrimSpace(os.Getenv("TANPOPO_TEST_HERMETIC")) == "1"
	if redirectURI == "" && allowHermeticHTTP {
		host, port, splitErr := net.SplitHostPort(listen)
		if splitErr == nil {
			host = strings.Trim(host, "[]")
			ip := net.ParseIP(host)
			if host == "localhost" || (ip != nil && ip.IsLoopback()) {
				redirectURI = "http://" + net.JoinHostPort(host, port) + "/admin/v1/oauth/callback"
			}
		}
	}
	brokerUnsealCap := vault.MintUnsealCapabilityForBroker()
	webhookDispatcher, err = webhook.New(webhook.Config{
		// This binary runtime currently opens SQLite. A PostgreSQL runtime must
		// inject its dedicated dispatcher pool and opt into role/RLS validation.
		DB: db, Postgres: false, Resolver: egress.NewStdHostResolver(),
		Unseal: func(ctx context.Context, orgID, credentialID string) ([]byte, func(), error) {
			secret, unsealErr := vaultStore.UnsealWithCapability(ctx, brokerUnsealCap, orgID, credentialID)
			if unsealErr != nil {
				return nil, nil, unsealErr
			}
			if secret.SecretKind != vault.SecretKindWebhookHMAC {
				secret.Zeroize()
				return nil, nil, errors.New("webhook signing credential has an invalid kind")
			}
			return secret.Plaintext, secret.Zeroize, nil
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize webhook dispatcher: %w", err)
	}
	closeWebhookDispatcher = webhookDispatcher.Close
	killSwitch, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{
		DB: db, Audit: auditLog, Streams: killswitch.NewStreamRegistry(),
		RecordWebhookTx: func(ctx context.Context, tx *sql.Tx, orgID, projectID, eventKey, eventType string, payload []byte) error {
			if orgID == "" {
				var body struct {
					TargetID string `json:"target_id"`
				}
				if err := json.Unmarshal(payload, &body); err != nil || body.TargetID == "" {
					return fmt.Errorf("resolve extension kill-switch target")
				}
				return recordExtensionWebhookOutbox(ctx, tx, webhookDispatcher, body.TargetID, "kill_switch:"+eventKey, eventType, payload)
			}
			return webhookDispatcher.RecordEventTx(ctx, tx, webhook.Event{
				ID: webhook.StableEventID("kill_switch:" + eventKey), OrgID: orgID,
				ProjectID: projectID, Type: eventType, Payload: payload,
			})
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize kill switches: %w", err)
	}
	snaps.RegisterKillSwitchSync(killSwitch.RefreshFromDB)
	ledgerService, err = ledger.NewService(ledger.Config{
		DB: db, Audit: auditLog,
		AlertOutbox: func(ctx context.Context, tx *sql.Tx, ev ledger.AlertEvent) error {
			var scopeType, scopeID string
			var projectID string
			if err := tx.QueryRowContext(ctx, `SELECT scope_type,scope_id FROM budget_ledgers WHERE org_id=? AND id=?`, ev.OrgID, ev.LedgerID).Scan(&scopeType, &scopeID); err != nil {
				return err
			}
			switch scopeType {
			case "PROJECT":
				projectID = scopeID
			case "ENV":
				if err := tx.QueryRowContext(ctx, `SELECT project_id FROM environments WHERE org_id=? AND id=?`, ev.OrgID, scopeID).Scan(&projectID); err != nil {
					return err
				}
			case "VIRTUAL_KEY":
				if err := tx.QueryRowContext(ctx, `SELECT project_id FROM virtual_keys WHERE org_id=? AND id=?`, ev.OrgID, scopeID).Scan(&projectID); err != nil {
					return err
				}
			}
			payload, marshalErr := json.Marshal(map[string]any{
				"code": ev.Code, "ledger_id": ev.LedgerID, "scope_type": ev.ScopeType,
				"scope_id": ev.ScopeID, "threshold_pct": ev.ThresholdPct,
				"utilization_pct": ev.UtilizationPct, "request_id": ev.RequestID,
				"occurred_at_ms": ev.OccurredAtMs,
			})
			if marshalErr != nil {
				return marshalErr
			}
			if scopeType != "ORG" && projectID == "" {
				return fmt.Errorf("resolve project for budget alert scope %s", scopeType)
			}
			return webhookDispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID("budget:" + ev.LedgerID + ":" + ev.Code), OrgID: ev.OrgID, ProjectID: projectID, Type: "budget.threshold", Payload: payload})
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize usage ledger: %w", err)
	}
	webhookDone = make(chan struct{})
	go func() {
		defer close(webhookDone)
		_ = webhookDispatcher.Run(runtimeCtx)
	}()
	var oauthService *oauth.Service
	if redirectURI != "" {
		oauthService, err = oauth.NewService(oauth.ServiceConfig{DB: db, Vault: vaultStore, UnsealCapability: brokerUnsealCap, Audit: auditLog, HostResolver: egress.NewStdHostResolver(), RedirectURI: redirectURI, AllowLoopbackHTTP: allowHermeticHTTP})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize OAuth coordinator: %w", err)
		}
	}
	lockfile := envOr("TANPOPO_LOCK_FILE", filepath.Join(dataDir, "tanpopo.lock"))
	installerService, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB: db, Audit: auditLog, AuditEntryPoint: "admin_api", PluginsDir: pluginsDir, LockfilePath: lockfile,
		RecordQuarantineEventTx: func(ctx context.Context, tx *sql.Tx, installationID, packageID, state string) error {
			payload, err := json.Marshal(map[string]any{"installation_id": installationID, "package_id": packageID, "state": state})
			if err != nil {
				return err
			}
			return recordExtensionWebhookOutbox(ctx, tx, webhookDispatcher, installationID, "extension.quarantined:"+installationID, "extension.quarantined", payload)
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize extension installer: %w", err)
	}
	registryStore, err := registry.NewSQLStore(db, time.Now)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize marketplace registry store: %w", err)
	}
	marketplaceDone = startMarketplaceRuntime(runtimeCtx, registryStore, installerService, snaps, auditLog)
	brokerResolver := planeBrokerResolver{provider: catalogRepo, mcp: mcphub.BrokerResolver{DB: db}}
	blindBroker, err := broker.NewBlindBroker(broker.Config{
		Resolver: brokerResolver, PolicyResolver: brokerResolver, Vault: vaultStore,
		UnsealCap: brokerUnsealCap, Audit: auditLog, OAuthTokens: oauthService,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("initialize credential broker: %w", err)
	}
	mux := http.NewServeMux()
	if role == "all" || role == "data-plane" {
		mcpServer, serverErr := mcphub.NewServer(mcphub.Config{
			DB: db, Snapshots: snaps, Broker: blindBroker, Audit: auditLog,
			Upstream: &mcphub.BrokerClient{Broker: blindBroker},
			RecordApprovalPendingTx: func(ctx context.Context, tx *sql.Tx, requestID string, binding approval.Binding) error {
				payload, marshalErr := json.Marshal(map[string]string{"approval_request_id": requestID})
				if marshalErr != nil {
					return marshalErr
				}
				return webhookDispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID("mcp.approval.pending:" + requestID), OrgID: binding.OrgID, ProjectID: binding.ProjectID,
					Type: "mcp.approval.pending", Payload: payload})
			},
		})
		if serverErr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize MCP Governance Hub: %w", serverErr)
		}
		approvalMaintenanceDone = make(chan struct{})
		go func() {
			defer close(approvalMaintenanceDone)
			mcpServer.RunApprovalMaintenance(runtimeCtx)
		}()
		mux.Handle("/mcp", mcpServer)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if ctx.Err() != nil || store.Ping(checkCtx) != nil || vaultStore.CheckServingReady(checkCtx) != nil || snaps.CheckServingReady() != nil || (dataServerReady != nil && dataServerReady(checkCtx) != nil) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	var dataServer *ingresshttp.Server
	if role == "all" || role == "data-plane" {
		telemetryPipeline, err = telemetry.NewPipeline(telemetry.PipelineConfig{
			ServiceName: "tanpopo", ServiceVersion: version, DB: db, Vault: vaultStore,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize telemetry pipeline: %w", err)
		}
		dataServer, err = ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots: snaps, Broker: blindBroker, KillSwitch: killSwitch, PluginsDir: pluginsDir, Audit: auditLog, IdempotencyDB: db,
			BudgetLedger: ledgerService,
			Telemetry:    telemetryPipeline,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Data Plane: %w", err)
		}
		closeDataServer = func() error {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return dataServer.Close(shutdownCtx)
		}
		dataServerReady = dataServer.CheckServingReady
		mux.Handle("/v1/", dataServer)
		mux.Handle("/metrics", dataServer.MetricsHandler())
	}
	if role == "all" || role == "control-plane" {
		bootstrapManager, err := bootstrap.NewManager(bootstrap.ManagerConfig{
			DB: db, TokenPath: envOr("TANPOPO_BOOTSTRAP_TOKEN_FILE", filepath.Join(dataDir, "bootstrap.token")), AuditSink: auditLog,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize bootstrap service: %w", err)
		}
		if _, err := bootstrapManager.EnsureToken(ctx); err != nil && !errors.Is(err, bootstrap.ErrBootstrapAlreadyCompleted) {
			cleanup()
			return nil, nil, fmt.Errorf("ensure bootstrap token: %w", err)
		}
		authService, err := auth.NewLocalService(auth.ServiceConfig{DB: db, AuditSink: auditLog, AllowedOrigins: allowedOrigins(listen)})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize local authentication: %w", err)
		}
		vkeys, err := vkey.NewService(vkey.ServiceConfig{DB: db, Snapshots: snaps, AuditSink: auditLog})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize virtual keys: %w", err)
		}
		var providerRuntimeFactory func(context.Context, string, string) (extport.ProviderRuntime, func(), error)
		if dataServer != nil {
			providerRuntimeFactory = dataServer.AcquireProviderRuntime
		}
		adminServer, err := adminhttp.NewServer(adminhttp.ServerConfig{
			Store: store, DB: db, Bootstrap: bootstrapManager, Auth: authService, VKeys: vkeys, Ledgers: ledgerService,
			Vault: vaultStore, Snapshots: snaps, KillSwitch: killSwitch, Catalog: catalogRepo,
			Installer: installerService, ArchiveResolver: newRuntimeArchiveResolver(os.Getenv(installer.LocalMarketplaceArchiveRootEnv), registryStore, vaultOCIRegistryCredentialResolver{db: db, vault: vaultStore, cap: brokerUnsealCap}),
			WebhookEnqueue: func(ctx context.Context, orgID, projectID, eventType string, payload json.RawMessage) error {
				_, enqueueErr := webhookDispatcher.Enqueue(ctx, webhook.Event{OrgID: orgID, ProjectID: projectID, Type: eventType, Payload: payload})
				return enqueueErr
			},
			Broker: blindBroker, OAuth: oauthService, ProviderRuntimeFactory: providerRuntimeFactory, Audit: auditLog, Version: version,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("initialize Admin API: %w", err)
		}
		mux.Handle("/admin/v1/", adminServer)
		mux.Handle("/", webui.Handler())
	}
	return mux, cleanup, nil
}

func recordExtensionWebhookOutbox(ctx context.Context, tx *sql.Tx, dispatcher *webhook.Dispatcher, installationID, eventKey, eventType string, payload []byte) error {
	rows, err := tx.QueryContext(ctx, `SELECT org_id FROM org_extension_policies WHERE package_id=(SELECT package_id FROM extension_installations WHERE id=?) AND enabled=1
		UNION SELECT pc.org_id FROM provider_connections pc JOIN provider_adapters pa ON pa.id=pc.adapter_id WHERE pa.installation_id=? ORDER BY 1`, installationID, installationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var orgIDs []string
	for rows.Next() {
		var orgID string
		if err := rows.Scan(&orgID); err != nil {
			return err
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, orgID := range orgIDs {
		if err := dispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID(eventKey + ":" + orgID), OrgID: orgID, Type: eventType, Payload: payload}); err != nil {
			return err
		}
	}
	return nil
}

func newServeArchiveResolver(root string) adminhttp.ExtensionArchiveResolver {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	resolver, err := installer.NewLocalMarketplaceArchiveResolver(root)
	if err != nil {
		return func(context.Context, string, string, string) (*installer.StageInstallRequest, func(), error) {
			return nil, nil, err
		}
	}
	return func(ctx context.Context, _ string, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
		return resolver.Resolve(ctx, sourceURI, expectedSHA256)
	}
}

func newRuntimeArchiveResolver(root string, index registry.IndexStore, credentials registry.RegistryCredentialResolver) adminhttp.ExtensionArchiveResolver {
	local := newServeArchiveResolver(root)
	oci := registry.NewOCIArchiveResolver(index, credentials, egress.NewStdHostResolver())
	return func(ctx context.Context, orgID, sourceURI, expectedSHA256 string) (*installer.StageInstallRequest, func(), error) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sourceURI)), "oci://") {
			return oci.Resolve(ctx, orgID, sourceURI, expectedSHA256)
		}
		if local == nil {
			return nil, nil, installer.ErrMarketplaceArchiveResolverUnavailable
		}
		return local(ctx, orgID, sourceURI, expectedSHA256)
	}
}

func allowedOrigins(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil
	}
	host = strings.Trim(host, "[]")
	return []string{"http://" + net.JoinHostPort(host, port)}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
