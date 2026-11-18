// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package support

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	clisanitize "github.com/nguywnben/tanpopo/internal/cli/sanitize"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	opsdoctor "github.com/nguywnben/tanpopo/internal/ops/doctor"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type doctorReportDocument struct {
	SchemaVersion string        `json:"schema_version"`
	Checks        []doctorCheck `json:"checks"`
}

// NewDoctorCommand reports local file diagnostics and loopback-only service probes.
func NewDoctorCommand(opts Options) *cobra.Command {
	if strings.TrimSpace(opts.RootDir) == "" {
		opts.RootDir = envOr("TANPOPO_DATA_DIR", "data")
	}
	var artifacts []string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local Tanpopo files and service health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := parseDoctorArtifactPaths(artifacts)
			if err != nil {
				return err
			}
			report, err := collectDoctorReport(cmd.Context(), opts.RootDir, paths)
			if err != nil {
				return fmt.Errorf("collect local doctor report: %w", err)
			}
			var outputErr error
			format := "table"
			if opts.OutputFormat != nil {
				format = strings.ToLower(strings.TrimSpace(*opts.OutputFormat))
			}
			switch format {
			case "json":
				outputErr = json.NewEncoder(opts.Stdout).Encode(report)
			case "yaml":
				outputErr = yaml.NewEncoder(opts.Stdout).Encode(report)
			case "table", "":
				w := tabwriter.NewWriter(opts.Stdout, 0, 0, 2, ' ', 0)
				if _, err := fmt.Fprintln(w, "CHECK	STATUS"); err != nil {
					return err
				}
				for _, check := range report.Checks {
					row := clisanitize.Terminal(check.Name) + "	" + clisanitize.Terminal(check.Status)
					if _, err := fmt.Fprintln(w, row); err != nil {
						return err
					}
				}
				outputErr = w.Flush()
			default:
				return errors.New("--output must be table, json, or yaml")
			}
			if outputErr != nil {
				return outputErr
			}
			for _, check := range report.Checks {
				if check.Status == strings.ToLower(string(opsdoctor.StatusFail)) {
					return errors.New("doctor found failing preflight checks")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.RootDir, "root", opts.RootDir, "Tanpopo data/config root")
	cmd.Flags().StringArrayVar(&artifacts, "artifact", nil, "Pinned extension archive as extension-id=path (repeat for each lock entry)")
	return cmd
}

func doctorReport(ctx context.Context, root string) ([]byte, error) {
	report, err := collectDoctorReport(ctx, root, nil)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func collectDoctorReport(ctx context.Context, root string, artifactPaths map[string]string) (doctorReportDocument, error) {
	if err := ctx.Err(); err != nil {
		return doctorReportDocument{}, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return doctorReportDocument{}, err
	}
	dbPath := strings.TrimSpace(os.Getenv("TANPOPO_DB_PATH"))
	if dbPath == "" {
		dbPath = filepath.Join(root, "tanpopo.db")
	}
	lockPath := strings.TrimSpace(os.Getenv("TANPOPO_LOCK_FILE"))
	if lockPath == "" {
		lockPath = filepath.Join(root, "tanpopo.lock")
	}
	listen := strings.TrimSpace(os.Getenv("TANPOPO_LISTEN_ADDR"))
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	probes := opsdoctor.Probes{
		ClockSkew: func(context.Context) opsdoctor.Status {
			value := strings.TrimSpace(os.Getenv("TANPOPO_EXPECTED_TIME"))
			if value == "" {
				return opsdoctor.StatusWarn
			}
			expected, parseErr := time.Parse(time.RFC3339, value)
			if parseErr != nil {
				return opsdoctor.StatusFail
			}
			if delta := time.Since(expected); delta > 5*time.Second || delta < -5*time.Second {
				return opsdoctor.StatusFail
			}
			return opsdoctor.StatusPass
		},
		PortsAvailable: func(context.Context) opsdoctor.Status {
			addresses := []string{listen}
			if admin := strings.TrimSpace(os.Getenv("TANPOPO_ADMIN_LISTEN_ADDR")); admin != "" {
				addresses = append(addresses, admin)
			}
			for _, address := range addresses {
				listener, listenErr := net.Listen("tcp", address)
				if listenErr != nil {
					return opsdoctor.StatusFail
				}
				_ = listener.Close()
			}
			return opsdoctor.StatusPass
		},
		MasterKEKReady: func(context.Context) opsdoctor.Status {
			keyID := strings.TrimSpace(os.Getenv("TANPOPO_MASTER_KEY_ID"))
			if keyID == "" {
				keyID = "kek_v1"
			}
			key, keyErr := vault.LoadStaticKEKFromEnv(keyID, os.LookupEnv)
			if keyErr != nil {
				return opsdoctor.StatusFail
			}
			key.Close()
			return opsdoctor.StatusPass
		},
		StorageFSLock: func(probeCtx context.Context) opsdoctor.Status {
			info, statErr := os.Stat(root)
			if statErr != nil || !info.IsDir() {
				return opsdoctor.StatusFail
			}
			free, freeErr := diskFreeBytes(root)
			if freeErr != nil || free < 1<<30 {
				return opsdoctor.StatusFail
			}
			dbInfo, dbErr := os.Lstat(dbPath)
			if dbErr != nil || !dbInfo.Mode().IsRegular() || dbInfo.Mode()&os.ModeSymlink != 0 {
				return opsdoctor.StatusFail
			}
			store, openErr := sqlite.Open(probeCtx, sqlite.Config{DBPath: dbPath, SkipMigrate: true})
			if openErr != nil {
				return opsdoctor.StatusFail
			}
			defer store.Close()
			var mode string
			if queryErr := store.RawDB().QueryRowContext(probeCtx, "PRAGMA journal_mode").Scan(&mode); queryErr != nil || !strings.EqualFold(mode, "wal") {
				return opsdoctor.StatusFail
			}
			return opsdoctor.StatusPass
		},
		BundleLockfileIntegrity: func(context.Context) opsdoctor.Status {
			data, readErr := os.ReadFile(lockPath)
			if readErr != nil {
				return opsdoctor.StatusFail
			}
			locked, parseErr := lockfile.ParseBytes(data)
			if parseErr != nil || locked == nil || len(artifactPaths) != len(locked.Extensions) {
				return opsdoctor.StatusFail
			}
			for packageID := range locked.Extensions {
				artifactPath, ok := artifactPaths[packageID]
				if !ok {
					return opsdoctor.StatusFail
				}
				info, statErr := os.Lstat(artifactPath)
				if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
					return opsdoctor.StatusFail
				}
				raw, readErr := os.ReadFile(artifactPath)
				if readErr != nil || locked.VerifyArtifactDigest(packageID, sha256Hex(raw)) != nil {
					return opsdoctor.StatusFail
				}
			}
			return opsdoctor.StatusPass
		},
		TLSPosture: func(context.Context) opsdoctor.Status {
			host, _, splitErr := net.SplitHostPort(listen)
			if splitErr == nil && (host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())) {
				return opsdoctor.StatusWarn
			}
			return opsdoctor.StatusFail
		},
		EgressDNSSSRF: func(context.Context) opsdoctor.Status {
			blockedPorts := map[int]bool{8080: true, 9090: true}
			for _, target := range []string{
				"http://169.254.169.254/latest/meta-data/",
				"http://[fd00:ec2::254]/latest/meta-data/",
				"http://127.0.0.1:8080/healthz",
				"http://127.0.0.1:9090/metrics",
			} {
				if egress.ValidateStaticEndpointURL(context.Background(), target, true, nil, blockedPorts) == nil {
					return opsdoctor.StatusFail
				}
			}
			return opsdoctor.StatusPass
		},
	}
	checks := make([]doctorCheck, 0, 8)
	for _, check := range opsdoctor.Run(ctx, probes).Checks {
		checks = append(checks, doctorCheck{Name: check.ID, Status: strings.ToLower(string(check.Status))})
	}
	return doctorReportDocument{SchemaVersion: "1", Checks: checks}, nil
}

func parseDoctorArtifactPaths(items []string) (map[string]string, error) {
	paths := make(map[string]string, len(items))
	for _, item := range items {
		id, path, ok := strings.Cut(item, "=")
		id, path = strings.TrimSpace(id), strings.TrimSpace(path)
		if !ok || id == "" || path == "" || strings.Contains(path, "=") {
			return nil, errors.New("--artifact must use extension-id=path")
		}
		if _, duplicate := paths[id]; duplicate {
			return nil, errors.New("--artifact extension IDs must be unique")
		}
		paths[id] = path
	}
	return paths, nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func pathStatus(path string, directory bool) string {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case err != nil:
		return "unavailable"
	case info.Mode()&os.ModeSymlink != 0:
		return "unsafe_path"
	case directory && !info.IsDir():
		return "unavailable"
	case !directory && !info.Mode().IsRegular():
		return "unavailable"
	default:
		return "present"
	}
}

func probeLocalHealth(ctx context.Context) []doctorCheck {
	base := strings.TrimSpace(os.Getenv("TANPOPO_URL"))
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	target, err := url.Parse(base)
	if err != nil || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" ||
		(target.Scheme != "http" && target.Scheme != "https") || !loopbackHost(target.Hostname()) {
		return []doctorCheck{{Name: "process_liveness", Status: "not_probed_nonlocal"}, {Name: "service_readiness", Status: "not_probed_nonlocal"}}
	}
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		return []doctorCheck{{Name: "process_liveness", Status: "unavailable"}, {Name: "service_readiness", Status: "unavailable"}}
	}
	policy := egress.Policy{AllowedHosts: []string{target.Host}, AllowedPorts: []int{portNumber}, RequireTLS: target.Scheme == "https"}
	host := target.Hostname()
	if strings.EqualFold(host, "localhost") {
		policy.AllowPrivateIP = true
		policy.AllowedCIDRs = []string{"127.0.0.0/8", "::1/128"}
	} else if ip, parseErr := netip.ParseAddr(host); parseErr == nil && ip.IsLoopback() {
		policy.AllowPrivateIP = true
		if ip.Is4() {
			policy.AllowedCIDRs = []string{netip.PrefixFrom(ip, 32).String()}
		} else {
			policy.AllowedCIDRs = []string{netip.PrefixFrom(ip, 128).String()}
		}
	}
	client, err := egress.NewGuardedHTTPClient(egress.ClientConfig{Policy: policy, Timeout: time.Second, MaxRedirects: -1, DenyCrossOriginRedirects: true})
	if err != nil {
		return []doctorCheck{{Name: "process_liveness", Status: "unavailable"}, {Name: "service_readiness", Status: "unavailable"}}
	}
	checks := make([]doctorCheck, 0, 2)
	for _, probe := range []struct{ name, path string }{{"process_liveness", "/healthz"}, {"service_readiness", "/readyz"}} {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		req, reqErr := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(base, "/")+probe.path, nil)
		status := "unavailable"
		if reqErr == nil {
			resp, doErr := client.Do(req)
			if doErr == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					status = "ok"
				} else if resp.StatusCode == http.StatusServiceUnavailable && probe.name == "service_readiness" {
					status = "not_ready"
				}
			}
		}
		cancel()
		checks = append(checks, doctorCheck{Name: probe.name, Status: status})
	}
	return checks
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
