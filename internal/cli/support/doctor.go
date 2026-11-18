// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package support

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/nguywnben/tanpopo/internal/security/egress"
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
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local Tanpopo files and service health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := collectDoctorReport(cmd.Context(), opts.RootDir)
			if err != nil {
				return fmt.Errorf("collect local doctor report: %w", err)
			}
			format := "table"
			if opts.OutputFormat != nil {
				format = strings.ToLower(strings.TrimSpace(*opts.OutputFormat))
			}
			switch format {
			case "json":
				return json.NewEncoder(opts.Stdout).Encode(report)
			case "yaml":
				return yaml.NewEncoder(opts.Stdout).Encode(report)
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
				return w.Flush()
			default:
				return errors.New("--output must be table, json, or yaml")
			}
		},
	}
	cmd.Flags().StringVar(&opts.RootDir, "root", opts.RootDir, "Tanpopo data/config root")
	return cmd
}

func doctorReport(ctx context.Context, root string) ([]byte, error) {
	report, err := collectDoctorReport(ctx, root)
	if err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func collectDoctorReport(ctx context.Context, root string) (doctorReportDocument, error) {
	if err := ctx.Err(); err != nil {
		return doctorReportDocument{}, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return doctorReportDocument{}, err
	}
	rootInfo, rootErr := os.Stat(root)
	rootStatus := "ok"
	if rootErr != nil || !rootInfo.IsDir() {
		rootStatus = "unavailable"
	}
	dbPath := strings.TrimSpace(os.Getenv("TANPOPO_DB_PATH"))
	if dbPath == "" {
		dbPath = filepath.Join(root, "tanpopo.db")
	}
	lockPath := strings.TrimSpace(os.Getenv("TANPOPO_LOCK_FILE"))
	if lockPath == "" {
		lockPath = filepath.Join(root, "tanpopo.lock")
	}
	checks := []doctorCheck{{Name: "data_root", Status: rootStatus}}
	for _, item := range []struct {
		name string
		path string
		dir  bool
	}{
		{name: "sqlite_database", path: dbPath},
		{name: "configuration", path: filepath.Join(root, "tanpopo.yaml")},
		{name: "extension_lock_inventory", path: lockPath},
		{name: "prometheus_metrics", path: filepath.Join(root, "metrics.prom")},
		{name: "logs_directory", path: filepath.Join(root, "logs"), dir: true},
	} {
		checks = append(checks, doctorCheck{Name: item.name, Status: pathStatus(item.path, item.dir)})
	}
	checks = append(checks, probeLocalHealth(ctx)...)
	return doctorReportDocument{SchemaVersion: "1", Checks: checks}, nil
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
