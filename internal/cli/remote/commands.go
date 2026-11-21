// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package remote implements machine-safe read commands backed by the Admin API.
package remote

import (
	"bytes"
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
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
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
	Locale       i18n.Locale
}

func resolveLocale(opts Options) i18n.Locale {
	if opts.Locale == "" {
		return i18n.LocaleEn
	}
	return opts.Locale
}

type endpoint struct{ use, short, path string }

// NewCommands builds the supported Admin API read command tree.
func NewCommands(opts Options) []*cobra.Command {
	loc := resolveLocale(opts)
	config := group("config", i18n.T(loc, "cli.remote.config.short"))
	config.AddCommand(getCommand(opts, endpoint{"show", i18n.T(loc, "cli.remote.config.show.short"), "/admin/v1/overview/summary"}))

	provider := group("provider", i18n.T(loc, "cli.remote.provider.short"))
	provider.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.provider.list.short"), "/admin/v1/provider-adapters"}))
	connections := group("connections", i18n.T(loc, "cli.remote.provider.connections.short"))
	connections.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.provider.connections.list.short"), "/admin/v1/connections"}))
	provider.AddCommand(connections)

	model := group("model", i18n.T(loc, "cli.remote.model.short"))
	model.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.model.list.short"), "/admin/v1/models"}))

	route := group("route", i18n.T(loc, "cli.remote.route.short"))
	route.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.route.list.short"), "/admin/v1/routes"}))
	route.AddCommand(explainCommand(opts))

	key := group("key", i18n.T(loc, "cli.remote.key.short"))
	key.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.key.list.short"), "/admin/v1/virtual-keys"}))

	budget := group("budget", i18n.T(loc, "cli.remote.budget.short"))
	budget.AddCommand(getCommand(opts, endpoint{"inspect", i18n.T(loc, "cli.remote.budget.inspect.short"), "/admin/v1/budgets"}))

	audit := group("audit", i18n.T(loc, "cli.remote.audit.short"))
	audit.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.audit.list.short"), "/admin/v1/audit-events"}))
	audit.AddCommand(apiCommand(opts, endpoint{"verify", i18n.T(loc, "cli.remote.audit.verify.short"), "/admin/v1/audit-events/verify"}, http.MethodPost))

	killswitch := group("killswitch", i18n.T(loc, "cli.remote.killswitch.short"))
	killswitch.AddCommand(getCommand(opts, endpoint{"list", i18n.T(loc, "cli.remote.killswitch.list.short"), "/admin/v1/kill-switches"}))
	return []*cobra.Command{config, provider, model, route, key, budget, audit, killswitch}
}

func group(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
}

func getCommand(opts Options, ep endpoint) *cobra.Command {
	return apiCommand(opts, ep, http.MethodGet)
}

func apiCommand(opts Options, ep endpoint, method string) *cobra.Command {
	return &cobra.Command{Use: ep.use, Short: ep.short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return runRequest(cmd, opts, ep, method, nil)
	}}
}

func explainCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts)
	var payload string
	cmd := &cobra.Command{
		Use:   "explain",
		Short: i18n.T(loc, "cli.remote.route.explain.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(payload) == "" {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.remote.route.explain.err.required"), nil)
			}
			var decoded json.RawMessage
			if !json.Valid([]byte(payload)) {
				return cliError(testharness.ExitValidation, i18n.T(loc, "cli.remote.route.explain.err.json"), nil)
			}
			decoded = json.RawMessage(payload)
			return runRequest(cmd, opts, endpoint{"explain", i18n.T(loc, "cli.remote.route.explain.short"), "/admin/v1/routes/explain"}, http.MethodPost, decoded)
		},
	}
	cmd.Flags().StringVar(&payload, "payload", "", i18n.T(loc, "cli.remote.route.explain.flag.payload"))
	return cmd
}

func runRequest(cmd *cobra.Command, opts Options, ep endpoint, method string, payload []byte) error {
	loc := resolveLocale(opts)
	if len(payload) > 1<<20 {
		return cliError(testharness.ExitValidation, i18n.T(loc, "cli.remote.err.payload_size"), nil)
	}
	format := strings.ToLower(strings.TrimSpace(outputFormat(opts)))
	if format != "json" && format != "yaml" && format != "table" {
		return cliError(testharness.ExitValidation, i18n.T(loc, "cli.err.output_format"), nil)
	}
	base, token, org := lookup(opts, "TANPOPO_URL"), lookup(opts, "TANPOPO_ADMIN_TOKEN"), lookup(opts, "TANPOPO_ORG")
	if token == "" {
		return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.remote.err.token_required"), nil)
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return cliError(testharness.ExitValidation, i18n.T(loc, "cli.remote.err.url_invalid"), err)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return cliError(testharness.ExitSecurity, i18n.T(loc, "cli.remote.err.https_required"), nil)
	}
	apiURL := strings.TrimRight(base, "/") + ep.path
	req, err := http.NewRequestWithContext(cmd.Context(), method, apiURL, bytes.NewReader(payload))
	if err != nil {
		return cliError(testharness.ExitValidation, i18n.T(loc, "cli.remote.err.request_invalid"), err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept-Language", string(loc))
	if org != "" {
		req.Header.Set("X-Tanpopo-Org-Id", org)
	}
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	client := opts.Client
	if client == nil {
		client, err = adminClient(parsed)
		if err != nil {
			return cliError(testharness.ExitValidation, i18n.T(loc, "cli.remote.err.client_config"), err)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return cliError(testharness.ExitNetwork, i18n.T(loc, "cli.remote.err.request_failed"), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return cliError(testharness.ExitNetwork, i18n.T(loc, "cli.remote.err.read_failed"), err)
	}
	if len(body) > maxResponseBytes {
		return cliError(testharness.ExitContract, i18n.T(loc, "cli.remote.err.response_size"), nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(loc, resp.StatusCode)
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return cliError(testharness.ExitContract, i18n.T(loc, "cli.remote.err.invalid_json"), err)
	}
	if err := writeValue(opts.Stdout, format, value); err != nil {
		return cliError(testharness.ExitInternal, i18n.T(loc, "cli.remote.err.format_failed"), err)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
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
		MaxRedirects:             -1,
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
	header := make([]string, len(keys))
	for i, key := range keys {
		header[i] = sanitize.Terminal(key)
	}
	if _, err := fmt.Fprintln(w, strings.Join(header, "\t")); err != nil {
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

func statusError(loc i18n.Locale, status int) error {
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
	return cliError(code, i18n.T(loc, "cli.remote.err.http_status", "status", strconv.Itoa(status)), nil)
}

func cliError(code int, message string, cause error) error {
	return &testharness.ExitError{Code: code, Message: message, Cause: cause}
}
