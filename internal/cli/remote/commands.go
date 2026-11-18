// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package remote implements machine-safe read commands backed by the Admin API.
package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/nguywnben/tanpopo/internal/cli/sanitize"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

const maxResponseBytes = 4 << 20

// Options contains the root command's shared output setting.
type Options struct {
	OutputFormat *string
	Stdout       io.Writer
	Stderr       io.Writer
	Client       *http.Client
	LookupEnv    func(string) (string, bool)
}

type endpoint struct{ use, short, path string }

// NewCommands builds the supported Admin API read command tree.
func NewCommands(opts Options) []*cobra.Command {
	config := group("config", "Inspect server configuration")
	config.AddCommand(getCommand(opts, endpoint{"show", "Show the instance overview", "/admin/v1/overview/summary"}))

	provider := group("provider", "Inspect provider adapters and connections")
	provider.AddCommand(getCommand(opts, endpoint{"list", "List provider adapters", "/admin/v1/provider-adapters"}))
	connections := group("connections", "Provider connection commands")
	connections.AddCommand(getCommand(opts, endpoint{"list", "List provider connections", "/admin/v1/connections"}))
	provider.AddCommand(connections)

	model := group("model", "Inspect supported and configured models")
	model.AddCommand(getCommand(opts, endpoint{"list", "List models", "/admin/v1/models"}))

	route := group("route", "Inspect and explain routes")
	route.AddCommand(getCommand(opts, endpoint{"list", "List routes", "/admin/v1/routes"}))
	route.AddCommand(&cobra.Command{Use: "explain", Short: "Explain a route decision", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return fmt.Errorf("route explain requires a request payload; use the Admin API endpoint /admin/v1/routes/explain")
	}})

	key := group("key", "Inspect virtual keys")
	key.AddCommand(getCommand(opts, endpoint{"list", "List virtual keys", "/admin/v1/virtual-keys"}))

	budget := group("budget", "Inspect budgets and usage")
	budget.AddCommand(getCommand(opts, endpoint{"inspect", "Inspect project budgets", "/admin/v1/budgets"}))

	audit := group("audit", "Inspect and verify the audit chain")
	audit.AddCommand(getCommand(opts, endpoint{"list", "List audit events", "/admin/v1/audit-events"}))
	audit.AddCommand(getCommand(opts, endpoint{"verify", "Verify the audit chain", "/admin/v1/audit-events/verify"}))

	killswitch := group("killswitch", "Inspect active kill switches")
	killswitch.AddCommand(getCommand(opts, endpoint{"list", "List active kill switches", "/admin/v1/kill-switches"}))
	return []*cobra.Command{config, provider, model, route, key, budget, audit, killswitch}
}

func group(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
}

func getCommand(opts Options, ep endpoint) *cobra.Command {
	return &cobra.Command{Use: ep.use, Short: ep.short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		base, token, org := lookup(opts, "TANPOPO_URL"), lookup(opts, "TANPOPO_ADMIN_TOKEN"), lookup(opts, "TANPOPO_ORG")
		if token == "" {
			return cliError(testharness.ExitSecurity, "TANPOPO_ADMIN_TOKEN is required for Admin API access", nil)
		}
		parsed, err := url.Parse(base)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return cliError(testharness.ExitValidation, "TANPOPO_URL must be an http(s) URL without user info, query, or fragment", err)
		}
		apiURL := strings.TrimRight(base, "/") + ep.path
		req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, apiURL, nil)
		if err != nil {
			return cliError(testharness.ExitValidation, "invalid Admin API request", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if org != "" {
			req.Header.Set("X-Tanpopo-Org-Id", org)
		}
		client := opts.Client
		if client == nil {
			client, err = adminClient(parsed)
			if err != nil {
				return cliError(testharness.ExitValidation, "could not configure guarded Admin API client", err)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return cliError(testharness.ExitNetwork, "Admin API request failed", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		if err != nil {
			return cliError(testharness.ExitNetwork, "could not read Admin API response", err)
		}
		if len(body) > maxResponseBytes {
			return cliError(testharness.ExitContract, "Admin API response exceeds the 4 MiB CLI limit", nil)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return statusError(resp.StatusCode)
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return cliError(testharness.ExitContract, "Admin API returned invalid JSON", err)
		}
		format := strings.ToLower(strings.TrimSpace(outputFormat(opts)))
		if err := writeValue(opts.Stdout, format, value); err != nil {
			return cliError(testharness.ExitInternal, "could not format Admin API response", err)
		}
		return nil
	}}
}

func adminClient(target *url.URL) (*http.Client, error) {
	host := target.Hostname()
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
		return nil, errors.New("invalid Admin API port")
	}
	policy := egress.Policy{
		AllowedHosts: []string{target.Host},
		AllowedPorts: []int{portNumber},
		RequireTLS:   target.Scheme == "https",
	}
	if strings.EqualFold(host, "localhost") {
		policy.AllowPrivateIP = true
		policy.AllowedCIDRs = []string{"127.0.0.0/8", "::1/128"}
	} else if ip, parseErr := netip.ParseAddr(host); parseErr == nil && (ip.IsPrivate() || ip.IsLoopback()) {
		if ip.Is4() {
			policy.AllowedCIDRs = []string{netip.PrefixFrom(ip, 32).String()}
		} else {
			policy.AllowedCIDRs = []string{netip.PrefixFrom(ip, 128).String()}
		}
		policy.AllowPrivateIP = true
	}
	return egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy:                   policy,
		Timeout:                  10 * time.Second,
		MaxRedirects:             0,
		DenyCrossOriginRedirects: true,
	})
}

func lookup(opts Options, name string) string {
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, _ := lookup(name)
	if name == "TANPOPO_URL" && strings.TrimSpace(value) == "" {
		return "http://127.0.0.1:8080"
	}
	return strings.TrimSpace(value)
}

func outputFormat(opts Options) string {
	if opts.OutputFormat == nil {
		return "table"
	}
	return *opts.OutputFormat
}

func writeValue(out io.Writer, format string, value any) error {
	if out == nil {
		return errors.New("stdout writer is required")
	}
	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(true)
		return enc.Encode(value)
	case "yaml":
		enc := yaml.NewEncoder(out)
		enc.SetIndent(2)
		defer enc.Close()
		return enc.Encode(value)
	case "table", "":
		return writeTable(out, value)
	default:
		return cliError(testharness.ExitValidation, "--output must be table, json, or yaml", nil)
	}
}

func writeTable(out io.Writer, value any) error {
	var rows []map[string]any
	if object, ok := value.(map[string]any); ok {
		if data, ok := object["data"].([]any); ok {
			for _, item := range data {
				if row, ok := item.(map[string]any); ok {
					rows = append(rows, row)
				}
			}
		}
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if len(rows) == 0 {
		if object, ok := value.(map[string]any); ok {
			keys := make([]string, 0, len(object))
			for key := range object {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if _, err := fmt.Fprintf(w, "%s\t%v\n", sanitize.Terminal(key), sanitize.Terminal(fmt.Sprint(object[key]))); err != nil {
					return err
				}
			}
		} else {
			if _, err := fmt.Fprintln(w, sanitize.Terminal(fmt.Sprint(value))); err != nil {
				return err
			}
		}
		return w.Flush()
	}
	keys := make([]string, 0)
	for key := range rows[0] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if _, err := fmt.Fprintln(w, strings.Join(keys, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		values := make([]string, len(keys))
		for i, key := range keys {
			values[i] = sanitize.Terminal(fmt.Sprint(row[key]))
		}
		if _, err := fmt.Fprintln(w, strings.Join(values, "\t")); err != nil {
			return err
		}
	}
	return w.Flush()
}

func statusError(status int) error {
	code := testharness.ExitContract
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusConflict {
		code = testharness.ExitSecurity
	}
	if status >= 500 {
		code = testharness.ExitInternal
	}
	if status == http.StatusUnprocessableEntity {
		code = testharness.ExitContract
	}
	return cliError(code, "Admin API returned HTTP "+strconv.Itoa(status), nil)
}

func cliError(code int, message string, cause error) error {
	return &testharness.ExitError{Code: code, Message: message, Cause: cause}
}
