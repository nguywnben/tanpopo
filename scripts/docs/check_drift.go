// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Command check_drift enforces zero drift between the compiled tanpopo CLI,
// OpenAPI 3.1 contracts, monitoring alert runbooks (INC-01..INC-07), Markdown
// documentation links, Apache-2.0 metadata, and live Quickstart health probes.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DriftConfig controls the documentation synchronization checks.
type DriftConfig struct {
	RootDir            string
	CLIBinary          string
	ExecuteQuickstart  bool
	QuickstartTimeout  time.Duration
	CommandHelpFetcher func(args ...string) (string, error)
}

// DriftReport summarizes every check executed by RunDriftCheck.
type DriftReport struct {
	CLICommandsChecked  int           `json:"cli_commands_checked"`
	CLIInvocationsSeen  int           `json:"cli_invocations_seen"`
	OpenAPIPathsChecked int           `json:"openapi_paths_checked"`
	IncidentRunbooksOK  int           `json:"incident_runbooks_ok"`
	MarkdownLinksOK     int           `json:"markdown_links_ok"`
	QuickstartElapsed   time.Duration `json:"quickstart_elapsed"`
}

// CLICommandSpec captures valid subcommands and flags parsed from --help.
type CLICommandSpec struct {
	Path        string
	Subcommands map[string]bool
	Flags       map[string]bool
}

func main() {
	rootFlag := flag.String("root", ".", "Repository root directory")
	skipQuickstart := flag.Bool("skip-quickstart", false, "Skip live Quickstart binary validation")
	flag.Parse()

	absRoot, err := filepath.Abs(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check_drift: resolve root: %v\n", err)
		os.Exit(1)
	}

	report, err := RunDriftCheck(DriftConfig{
		RootDir:           absRoot,
		ExecuteQuickstart: !*skipQuickstart,
		QuickstartTimeout: 60 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: check_drift:\n%v\n", err)
		os.Exit(1)
	}

	fmt.Printf(
		"PASS check_drift: cli_cmds=%d cli_refs=%d openapi_paths=%d inc_runbooks=%d md_links=%d quickstart=%s\n",
		report.CLICommandsChecked,
		report.CLIInvocationsSeen,
		report.OpenAPIPathsChecked,
		report.IncidentRunbooksOK,
		report.MarkdownLinksOK,
		report.QuickstartElapsed.Round(time.Millisecond),
	)
}

// RunDriftCheck executes all TP-IMP-0046 synchronization gates.
func RunDriftCheck(cfg DriftConfig) (*DriftReport, error) {
	if cfg.RootDir == "" {
		return nil, errors.New("root directory is required")
	}
	if cfg.QuickstartTimeout <= 0 {
		cfg.QuickstartTimeout = 60 * time.Second
	}

	report := &DriftReport{}
	tempDir := ""
	binPath := cfg.CLIBinary
	if cfg.CommandHelpFetcher == nil && binPath == "" {
		var mkErr error
		tempDir, mkErr = os.MkdirTemp("", "tanpopo-check-drift-*")
		if mkErr != nil {
			return nil, fmt.Errorf("create temp dir: %w", mkErr)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()

		binName := "tanpopo"
		if runtime.GOOS == "windows" {
			binName = "tanpopo.exe"
		}
		binPath = filepath.Join(tempDir, binName)
		buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/tanpopo")
		buildCmd.Dir = cfg.RootDir
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := buildCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build ./cmd/tanpopo: %w\n%s", err, strings.TrimSpace(string(out)))
		}
	}

	fetcher := cfg.CommandHelpFetcher
	if fetcher == nil {
		fetcher = func(args ...string) (string, error) {
			helpArgs := append(append([]string{}, args...), "--help")
			cmd := exec.Command(binPath, helpArgs...)
			cmd.Dir = cfg.RootDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("run %q --help: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
			return string(out), nil
		}
	}

	cliTree, err := CrawlCLITree(fetcher)
	if err != nil {
		return nil, fmt.Errorf("crawl CLI tree: %w", err)
	}
	report.CLICommandsChecked = len(cliTree)

	cliDocFiles := []string{
		"README.md",
		"CONTRIBUTING.md",
		"SECURITY.md",
		"CHANGELOG.md",
		"docs/guides/README.md",
		"docs/guides/quickstart.md",
		"docs/guides/plugin-authoring-sdk.md",
		"docs/guides/production-ha-and-airgap.md",
		"docs/guides/security-and-mcp-governance.md",
	}
	if matches, globErr := filepath.Glob(filepath.Join(cfg.RootDir, "docs", "guides", "*", "user-guide.md")); globErr == nil {
		sort.Strings(matches)
		for _, m := range matches {
			if rel, relErr := filepath.Rel(cfg.RootDir, m); relErr == nil {
				cliDocFiles = append(cliDocFiles, filepath.ToSlash(rel))
			}
		}
	}
	invSeen, err := CheckCLIDocsSync(cfg.RootDir, cliDocFiles, cliTree)
	if err != nil {
		return nil, err
	}
	report.CLIInvocationsSeen = invSeen

	openAPICount, err := CheckOpenAPISpecs(cfg.RootDir, cliDocFiles)
	if err != nil {
		return nil, err
	}
	report.OpenAPIPathsChecked = openAPICount

	incCount, err := CheckMonitoringRunbooks(cfg.RootDir)
	if err != nil {
		return nil, err
	}
	report.IncidentRunbooksOK = incCount

	linkDocFiles, err := collectPortalAndGuideFiles(cfg.RootDir)
	if err != nil {
		return nil, err
	}
	linksChecked, err := CheckMarkdownLinksAndAssets(cfg.RootDir, linkDocFiles)
	if err != nil {
		return nil, err
	}
	report.MarkdownLinksOK = linksChecked

	if err := CheckHygieneAndLicensing(cfg.RootDir, linkDocFiles); err != nil {
		return nil, err
	}

	if cfg.ExecuteQuickstart {
		if binPath == "" {
			return nil, errors.New("CLIBinary is required when ExecuteQuickstart is true")
		}
		elapsed, err := ExecuteQuickstartProbe(cfg.RootDir, binPath, cfg.QuickstartTimeout)
		if err != nil {
			return nil, fmt.Errorf("quickstart live validation: %w", err)
		}
		report.QuickstartElapsed = elapsed
	}

	return report, nil
}

// CrawlCLITree recursively inspects `--help` output from root and all subcommands.
func CrawlCLITree(fetchHelp func(args ...string) (string, error)) (map[string]*CLICommandSpec, error) {
	specs := make(map[string]*CLICommandSpec)
	var walk func(path []string) error
	walk = func(path []string) error {
		key := strings.Join(path, " ")
		if _, seen := specs[key]; seen {
			return nil
		}
		out, err := fetchHelp(path...)
		if err != nil {
			return err
		}
		spec := ParseCLIHelp(key, out)
		specs[key] = spec

		subNames := make([]string, 0, len(spec.Subcommands))
		for sub := range spec.Subcommands {
			if sub == "completion" || sub == "help" {
				continue
			}
			subNames = append(subNames, sub)
		}
		sort.Strings(subNames)
		for _, sub := range subNames {
			next := append(append([]string{}, path...), sub)
			if err := walk(next); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(nil); err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, errors.New("empty CLI help tree")
	}
	return specs, nil
}

var (
	subcommandLineRe = regexp.MustCompile(`^\s{2}([a-z][a-z0-9-]+)\s+\S`)
	longFlagRe       = regexp.MustCompile(`--([a-z][a-z0-9-]+)`)
	shortFlagRe      = regexp.MustCompile(`(?:^|\s)(-[a-zA-Z])(?:,|\s|$)`)
)

// ParseCLIHelp parses Cobra `--help` output into a CLICommandSpec.
func ParseCLIHelp(cmdPath, output string) *CLICommandSpec {
	spec := &CLICommandSpec{
		Path:        cmdPath,
		Subcommands: make(map[string]bool),
		Flags:       make(map[string]bool),
	}
	section := ""
	for _, rawLine := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		switch trimmed {
		case "Available Commands:", "Additional Commands:":
			section = "commands"
			continue
		case "Flags:", "Global Flags:":
			section = "flags"
			continue
		case "":
			if section != "" && !strings.HasPrefix(rawLine, " ") {
				section = ""
			}
			continue
		}
		if !strings.HasPrefix(rawLine, " ") && strings.HasSuffix(trimmed, ":") {
			section = ""
			continue
		}

		switch section {
		case "commands":
			if m := subcommandLineRe.FindStringSubmatch(rawLine); m != nil {
				spec.Subcommands[m[1]] = true
			}
		case "flags":
			for _, m := range longFlagRe.FindAllStringSubmatch(rawLine, -1) {
				spec.Flags["--"+m[1]] = true
			}
			for _, m := range shortFlagRe.FindAllStringSubmatch(rawLine, -1) {
				spec.Flags[m[1]] = true
			}
		}
	}
	// Cobra accepts --help / -h on all commands implicitly even when omitted.
	spec.Flags["--help"] = true
	spec.Flags["-h"] = true
	return spec
}

type cliInvocation struct {
	File string
	Line int
	Cmd  string
}

var inlineCLICodeRe = regexp.MustCompile("`([^`\\n]+)`")

// CheckCLIDocsSync verifies every `tanpopo ...` CLI invocation in docs against cliTree.
func CheckCLIDocsSync(rootDir string, relFiles []string, cliTree map[string]*CLICommandSpec) (int, error) {
	var errs []string
	totalSeen := 0

	for _, rel := range relFiles {
		abs := filepath.Join(rootDir, filepath.FromSlash(rel))
		raw, err := os.ReadFile(abs)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: read error: %v", rel, err))
			continue
		}
		invocations := extractCLIInvocations(rel, string(raw))
		for _, inv := range invocations {
			totalSeen++
			if msg := validateCLIInvocation(inv, cliTree); msg != "" {
				errs = append(errs, msg)
			}
		}
	}
	if len(errs) > 0 {
		return totalSeen, fmt.Errorf("CLI documentation drift detected:\n- %s", strings.Join(errs, "\n- "))
	}
	if totalSeen == 0 {
		return 0, errors.New("no tanpopo CLI invocations found in monitored documentation files")
	}
	return totalSeen, nil
}

func extractCLIInvocations(relFile, content string) []cliInvocation {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	var out []cliInvocation
	inShellBlock := false
	inCodeBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				lang := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")))
				inShellBlock = lang == "bash" || lang == "sh" || lang == "shell" || lang == "console"
			} else {
				inCodeBlock = false
				inShellBlock = false
			}
			continue
		}

		if inShellBlock {
			// Join backslash-continued shell commands cleanly.
			startLine := i + 1
			joined := strings.TrimSpace(strings.TrimSuffix(line, "\\"))
			hasCont := strings.HasSuffix(strings.TrimRight(line, " \t"), "\\")
			for hasCont && i+1 < len(lines) {
				i++
				next := strings.TrimSpace(lines[i])
				hasCont = strings.HasSuffix(strings.TrimRight(lines[i], " \t"), "\\")
				joined += " " + strings.TrimSpace(strings.TrimSuffix(next, "\\"))
			}
			// Split simple chained commands (`&&`, `;`) inside bash lines.
			for _, seg := range splitShellSegments(joined) {
				if looksLikeTanpopoInvocation(seg) {
					out = append(out, cliInvocation{File: relFile, Line: startLine, Cmd: seg})
				}
			}
			continue
		}

		if !inCodeBlock {
			for _, m := range inlineCLICodeRe.FindAllStringSubmatch(line, -1) {
				snippet := strings.TrimSpace(m[1])
				if looksLikeTanpopoInvocation(snippet) {
					out = append(out, cliInvocation{File: relFile, Line: i + 1, Cmd: snippet})
				}
			}
		}
	}
	return out
}

func splitShellSegments(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "#") {
		return nil
	}
	parts := strings.Split(s, "&&")
	var out []string
	for _, p := range parts {
		for _, q := range strings.Split(p, ";") {
			q = strings.TrimSpace(q)
			if q != "" {
				out = append(out, q)
			}
		}
	}
	return out
}

func looksLikeTanpopoInvocation(seg string) bool {
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return false
	}
	head := fields[0]
	return head == "tanpopo" || head == "./bin/tanpopo" || head == "./tanpopo"
}

func validateCLIInvocation(inv cliInvocation, cliTree map[string]*CLICommandSpec) string {
	tokens := tokenizeShellWords(inv.Cmd)
	if len(tokens) <= 1 {
		return ""
	}
	// Strip binary token (`tanpopo` / `./bin/tanpopo`).
	args := tokens[1:]

	// Ignore multi-pattern table summary notations ONLY before any `--flag` appears,
	// so `tanpopo bundle export|verify|import` skips cleanly while `tanpopo serve --bogus | cat` fails.
	for _, tok := range args {
		if strings.HasPrefix(tok, "-") {
			break
		}
		if tok == "/" || strings.Contains(tok, "|") || strings.Contains(tok, "...") || tok == "<cmd>" || tok == "<subcommand>" {
			return ""
		}
	}

	// Resolve deepest command path matching non-flag leading tokens.
	cmdPath := ""
	idx := 0
	for idx < len(args) {
		tok := args[idx]
		if strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "<") || strings.HasPrefix(tok, "./") || strings.HasPrefix(tok, "/") {
			break
		}
		candidate := tok
		if cmdPath != "" {
			candidate = cmdPath + " " + tok
		}
		if _, ok := cliTree[candidate]; ok {
			cmdPath = candidate
			idx++
			continue
		}
		// Could this be a positional argument or a missing/renamed subcommand?
		parent := cliTree[cmdPath]
		if parent != nil && len(parent.Subcommands) > 0 && idx == 0 {
			return fmt.Sprintf("%s:%d: unknown subcommand %q in %q", inv.File, inv.Line, tok, inv.Cmd)
		}
		if parent != nil && len(parent.Subcommands) > 0 && isLikelySubcommandToken(tok) && !acceptsPositionalArg(cmdPath) {
			return fmt.Sprintf("%s:%d: unknown subcommand %q under %q in %q", inv.File, inv.Line, tok, cmdPath, inv.Cmd)
		}
		// Remaining tokens from here are positional args.
		break
	}

	spec := cliTree[cmdPath]
	if spec == nil {
		return fmt.Sprintf("%s:%d: command path %q not registered in CLI binary", inv.File, inv.Line, cmdPath)
	}

	// Validate every explicit `--flag` or `-x` flag in the invocation (stop at shell pipes/redirects).
	for j := idx; j < len(args); j++ {
		tok := args[j]
		if tok == "--" || tok == "|" || tok == ">" || tok == ">>" || tok == "2>&1" {
			break
		}
		if strings.HasPrefix(tok, "--") {
			flagName := tok
			if eq := strings.IndexByte(flagName, '='); eq > 0 {
				flagName = flagName[:eq]
			}
			if !spec.Flags[flagName] {
				return fmt.Sprintf("%s:%d: undocumented or stale CLI flag %q for command %q in %q", inv.File, inv.Line, flagName, nonEmptyOr(cmdPath, "tanpopo"), inv.Cmd)
			}
			continue
		}
		if strings.HasPrefix(tok, "-") && len(tok) == 2 && !isNumericToken(tok) {
			if !spec.Flags[tok] {
				return fmt.Sprintf("%s:%d: undocumented or stale CLI short flag %q for command %q in %q", inv.File, inv.Line, tok, nonEmptyOr(cmdPath, "tanpopo"), inv.Cmd)
			}
		}
	}
	return ""
}

func isNumericToken(tok string) bool {
	return len(tok) == 2 && tok[0] == '-' && tok[1] >= '0' && tok[1] <= '9'
}

func tokenizeShellWords(s string) []string {
	var tokens []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case ' ', '\t':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func isLikelySubcommandToken(tok string) bool {
	if strings.ContainsAny(tok, "./:=@") {
		return false
	}
	return true
}

func acceptsPositionalArg(cmdPath string) bool {
	switch cmdPath {
	case "plugin init", "plugin validate", "plugin test", "plugin pack", "plugin migrate-check", "bundle verify", "bundle import":
		return true
	}
	return false
}

func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

type openAPIDoc struct {
	OpenAPI string                 `yaml:"openapi"`
	Paths   map[string]interface{} `yaml:"paths"`
}

var docAPIPathRe = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])(/(?:healthz|readyz|metrics|mcp|v1/[a-zA-Z0-9_{}/-]+|admin/v1/[a-zA-Z0-9_{}/-]+|a2a/[a-zA-Z0-9_{}/-]+|\.well-known/agent\.json))`)

// CheckOpenAPISpecs parses openapi/{dataplane,admin}-v1.yaml and checks documented endpoint parity.
func CheckOpenAPISpecs(rootDir string, docFiles []string) (int, error) {
	allPaths := make(map[string]bool)
	for _, specRel := range []string{"openapi/dataplane-v1.yaml", "openapi/admin-v1.yaml"} {
		raw, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(specRel)))
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", specRel, err)
		}
		var doc openAPIDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return 0, fmt.Errorf("parse %s: %w", specRel, err)
		}
		if !strings.HasPrefix(doc.OpenAPI, "3.1.") {
			return 0, fmt.Errorf("%s: expected OpenAPI 3.1.x, got %q", specRel, doc.OpenAPI)
		}
		if len(doc.Paths) == 0 {
			return 0, fmt.Errorf("%s: OpenAPI paths map is empty", specRel)
		}
		for p := range doc.Paths {
			allPaths[p] = true
		}
	}

	var errs []string
	for _, rel := range docFiles {
		raw, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for idx, line := range lines {
			for _, sub := range docAPIPathRe.FindAllStringSubmatch(line, -1) {
				m := sub[1]
				p := strings.TrimRight(m, ".,;:)\"'`")
				if strings.HasSuffix(p, "/v1/...") || strings.Contains(p, "/...") || strings.Contains(p, ".proto") {
					continue
				}
				if p == "/v1" || p == "/admin/v1" {
					continue
				}
				if !matchesOpenAPIPath(p, allPaths) {
					errs = append(errs, fmt.Sprintf("%s:%d: endpoint %q missing from OpenAPI specs", rel, idx+1, p))
				}
			}
		}
	}
	if len(errs) > 0 {
		return len(allPaths), fmt.Errorf("OpenAPI contract drift detected:\n- %s", strings.Join(errs, "\n- "))
	}
	return len(allPaths), nil
}

func matchesOpenAPIPath(documented string, declared map[string]bool) bool {
	if declared[documented] {
		return true
	}
	docSegs := strings.Split(strings.Trim(documented, "/"), "/")
	for specPath := range declared {
		specSegs := strings.Split(strings.Trim(specPath, "/"), "/")
		if len(docSegs) != len(specSegs) {
			continue
		}
		match := true
		for i := range docSegs {
			if specSegs[i] == docSegs[i] {
				continue
			}
			if strings.HasPrefix(specSegs[i], "{") && strings.HasSuffix(specSegs[i], "}") {
				continue
			}
			match = false
			break
		}
		if match {
			return true
		}
	}
	return false
}

type prometheusRulesDoc struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Alert       string            `yaml:"alert"`
			Labels      map[string]string `yaml:"labels"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

var incidentHeadingRe = regexp.MustCompile(`(?m)^###\s+` + "`" + `(INC-\d+)` + "`" + `:`)

// CheckMonitoringRunbooks validates INC-01..07 runbook parity across alerts.yaml, runbook docs, and Journey 3.
func CheckMonitoringRunbooks(rootDir string) (int, error) {
	alertsRaw, err := os.ReadFile(filepath.Join(rootDir, "deploy", "monitoring", "alerts.yaml"))
	if err != nil {
		return 0, fmt.Errorf("read alerts.yaml: %w", err)
	}
	var alerts prometheusRulesDoc
	if err := yaml.Unmarshal(alertsRaw, &alerts); err != nil {
		return 0, fmt.Errorf("parse alerts.yaml: %w", err)
	}

	runbookRaw, err := os.ReadFile(filepath.Join(rootDir, "docs", "operations", "monitoring-incidents.md"))
	if err != nil {
		return 0, fmt.Errorf("read monitoring-incidents.md: %w", err)
	}
	declaredIncidents := make(map[string]bool)
	for _, m := range incidentHeadingRe.FindAllStringSubmatch(string(runbookRaw), -1) {
		declaredIncidents[m[1]] = true
	}

	j3Raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "guides", "production-ha-and-airgap.md"))
	if err != nil {
		return 0, fmt.Errorf("read docs/guides/production-ha-and-airgap.md: %w", err)
	}
	j3Text := string(j3Raw)

	var errs []string
	for i := 1; i <= 7; i++ {
		id := fmt.Sprintf("INC-%02d", i)
		if !declaredIncidents[id] {
			errs = append(errs, fmt.Sprintf("missing heading for %s in docs/operations/monitoring-incidents.md", id))
		}
		lowerAnchor := strings.ToLower(id)
		if !strings.Contains(j3Text, "monitoring-incidents.md#"+lowerAnchor) {
			errs = append(errs, fmt.Sprintf("docs/guides/production-ha-and-airgap.md missing link to monitoring-incidents.md#%s", lowerAnchor))
		}
	}

	rulesChecked := 0
	for _, g := range alerts.Groups {
		for _, r := range g.Rules {
			rulesChecked++
			rb := strings.TrimSpace(r.Labels["runbook"])
			ref := strings.TrimSpace(r.Annotations["runbook_ref"])
			if rb == "" || !declaredIncidents[rb] {
				errs = append(errs, fmt.Sprintf("alert %q has invalid/missing runbook label %q", r.Alert, rb))
			}
			expectedRef := "docs/operations/monitoring-incidents.md#" + strings.ToLower(rb)
			if ref != expectedRef {
				errs = append(errs, fmt.Sprintf("alert %q runbook_ref=%q, expected %q", r.Alert, ref, expectedRef))
			}
		}
	}
	if rulesChecked == 0 {
		errs = append(errs, "no alert rules found in deploy/monitoring/alerts.yaml")
	}
	if len(errs) > 0 {
		return len(declaredIncidents), fmt.Errorf("monitoring runbook drift detected:\n- %s", strings.Join(errs, "\n- "))
	}
	return len(declaredIncidents), nil
}

func collectPortalAndGuideFiles(rootDir string) ([]string, error) {
	files := []string{
		"README.md",
		"SECURITY.md",
		"CONTRIBUTING.md",
		"CHANGELOG.md",
		"docs/README.md",
		"docs/guides/README.md",
		"docs/guides/quickstart.md",
		"docs/guides/plugin-authoring-sdk.md",
		"docs/guides/production-ha-and-airgap.md",
		"docs/guides/security-and-mcp-governance.md",
	}
	err := filepath.WalkDir(filepath.Join(rootDir, "docs", "guides"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		if d.Name() == "user-guide.md" {
			rel, relErr := filepath.Rel(rootDir, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

var (
	mdLinkRe   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	htmlImgRe  = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	headingRe  = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	slugStrip  = regexp.MustCompile(`[^a-z0-9\s-]`)
)

// CheckMarkdownLinksAndAssets validates every local file link and image asset in portal files.
func CheckMarkdownLinksAndAssets(rootDir string, relFiles []string) (int, error) {
	headingCache := make(map[string]map[string]bool)
	loadHeadings := func(absTarget string) (map[string]bool, error) {
		if h, ok := headingCache[absTarget]; ok {
			return h, nil
		}
		raw, err := os.ReadFile(absTarget)
		if err != nil {
			return nil, err
		}
		anchors := make(map[string]bool)
		for _, m := range headingRe.FindAllStringSubmatch(string(raw), -1) {
			anchors[githubSlug(m[1])] = true
		}
		headingCache[absTarget] = anchors
		return anchors, nil
	}

	var errs []string
	checked := 0
	for _, rel := range relFiles {
		absFile := filepath.Join(rootDir, filepath.FromSlash(rel))
		raw, err := os.ReadFile(absFile)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: missing file: %v", rel, err))
			continue
		}
		content := stripFencedCodeBlocks(string(raw))
		dir := filepath.Dir(absFile)

		checkTarget := func(target string) {
			if target == "" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				return
			}
			checked++
			targetPath := target
			fragment := ""
			if idx := strings.IndexByte(target, '#'); idx >= 0 {
				targetPath = target[:idx]
				fragment = strings.TrimSpace(target[idx+1:])
			}
			absTarget := absFile
			if targetPath != "" {
				absTarget = filepath.Clean(filepath.Join(dir, filepath.FromSlash(targetPath)))
			}
			info, statErr := os.Stat(absTarget)
			if statErr != nil {
				errs = append(errs, fmt.Sprintf("%s: broken link target %q", rel, target))
				return
			}
			if fragment != "" && !info.IsDir() && strings.HasSuffix(strings.ToLower(absTarget), ".md") {
				anchors, hErr := loadHeadings(absTarget)
				if hErr != nil {
					errs = append(errs, fmt.Sprintf("%s: read anchor target %q: %v", rel, target, hErr))
					return
				}
				if !matchesGitHubFragment(fragment, anchors) {
					errs = append(errs, fmt.Sprintf("%s: broken heading anchor %q (normalized #%s not found)", rel, target, fragment))
				}
			}
		}

		for _, m := range mdLinkRe.FindAllStringSubmatch(content, -1) {
			checkTarget(m[1])
		}
		for _, m := range htmlImgRe.FindAllStringSubmatch(content, -1) {
			checkTarget(m[1])
		}
	}

	svgRaw, err := os.ReadFile(filepath.Join(rootDir, "docs", "assets", "tanpopo-logo.svg"))
	if err != nil || !strings.Contains(string(svgRaw), "<svg") {
		errs = append(errs, "docs/assets/tanpopo-logo.svg missing or invalid SVG markup")
	}

	if len(errs) > 0 {
		return checked, fmt.Errorf("broken Markdown links or assets:\n- %s", strings.Join(errs, "\n- "))
	}
	return checked, nil
}

func stripFencedCodeBlocks(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func githubSlug(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	s = strings.ReplaceAll(s, "`", "")
	s = slugStrip.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func matchesGitHubFragment(fragment string, anchors map[string]bool) bool {
	if anchors[fragment] {
		return true
	}
	// Accept canonical prefix matches such as #inc-01 -> inc-01-budget-exhaustion...
	if strings.HasPrefix(fragment, "inc-") {
		for k := range anchors {
			if strings.HasPrefix(k, fragment+"-") || k == fragment {
				return true
			}
		}
	}
	return false
}

var docSecretLeakRe = regexp.MustCompile(`(?:sk-live-|sk-proj-|AKIA[0-9A-Z]{16}|-----BEGIN (?:RSA |EC |DSA |ENCRYPTED |OPENSSH )?PRIVATE KEY-----)`)

// CheckHygieneAndLicensing checks Apache-2.0 files and ensures docs contain placeholders only.
func CheckHygieneAndLicensing(rootDir string, relFiles []string) error {
	for _, required := range []string{"LICENSE", "NOTICE", "SECURITY.md", "CONTRIBUTING.md", "CHANGELOG.md", "README.md"} {
		info, err := os.Stat(filepath.Join(rootDir, required))
		if err != nil || info.Size() == 0 {
			return fmt.Errorf("required repository root file %s missing or empty", required)
		}
	}
	licenseRaw, err := os.ReadFile(filepath.Join(rootDir, "LICENSE"))
	if err != nil || !strings.Contains(string(licenseRaw), "Apache License") {
		return errors.New("LICENSE is not Apache License 2.0")
	}
	noticeRaw, err := os.ReadFile(filepath.Join(rootDir, "NOTICE"))
	if err != nil || !strings.Contains(string(noticeRaw), "Apache-2.0") {
		return errors.New("NOTICE must declare Apache-2.0 notices")
	}
	for _, rel := range relFiles {
		raw, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if docSecretLeakRe.Match(raw) {
			return fmt.Errorf("%s contains potential real credential material", rel)
		}
	}
	return nil
}

// ExecuteQuickstartProbe boots the compiled tanpopo binary hermetically, runs first-run
// bootstrap + login + Virtual Key creation, and verifies /readyz, /v1/models, and /v1/chat/completions
// complete within the Quickstart timeout (< 60s).
func ExecuteQuickstartProbe(rootDir, binPath string, timeout time.Duration) (time.Duration, error) {
	started := time.Now()
	tmpDir, err := os.MkdirTemp("", "tanpopo-quickstart-*")
	if err != nil {
		return 0, fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate free port: %w", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	masterRaw := sha256.Sum256([]byte("quickstart-drift-test-master-key"))
	masterHex := hex.EncodeToString(masterRaw[:])
	tokenFile := filepath.Join(tmpDir, "bootstrap.token")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "serve", "--role", "all", "--listen", addr, "--offline")
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(),
		"TANPOPO_DATA_DIR="+tmpDir,
		"TANPOPO_PLUGINS_DIR="+filepath.Join(tmpDir, "plugins"),
		"TANPOPO_STORAGE_ENGINE=sqlite",
		"TANPOPO_PG_DSN=",
		"TANPOPO_VALKEY_ADDRS=",
		"TANPOPO_ADMIN_LISTEN_ADDR=",
		"TANPOPO_DB_PATH="+filepath.Join(tmpDir, "tanpopo.db"),
		"TANPOPO_LOCK_FILE="+filepath.Join(tmpDir, "tanpopo.lock"),
		"TANPOPO_BOOTSTRAP_TOKEN_FILE="+tokenFile,
		"TANPOPO_MASTER_KEY="+masterHex,
		"TANPOPO_DRAIN_TIMEOUT=2s",
		"TANPOPO_TEST_HERMETIC=1",
	)
	var logBuf bytes.Buffer
	cmd.Stdout = &logBuf
	cmd.Stderr = &logBuf

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start tanpopo serve: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		waitCh := make(chan error, 1)
		go func() { waitCh <- cmd.Wait() }()
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-waitCh
		}
	}()

	baseURL := "http://" + addr
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Poll /readyz until 200 OK.
	if err := waitReadyz(ctx, client, baseURL+"/readyz"); err != nil {
		return 0, fmt.Errorf("%w\nlogs:\n%s", err, logBuf.String())
	}

	// 2. Read generated bootstrap token and complete initial ORG_OWNER bootstrap.
	bootstrapTok, err := waitReadBootstrapToken(ctx, tokenFile)
	if err != nil {
		return 0, fmt.Errorf("read bootstrap token: %w\nlogs:\n%s", err, logBuf.String())
	}
	bootResp, body, err := doJSON(ctx, client, http.MethodPost, baseURL+"/admin/v1/bootstrap/complete", "", "", nil, map[string]any{
		"bootstrap_token":                bootstrapTok,
		"owner_email":                    "quickstart@tanpopo.local",
		"owner_password":                 "Quickstart-Correct-Horse-2026!",
		"master_kek_backup_acknowledged": true,
	})
	if err != nil || bootResp.StatusCode != http.StatusCreated && bootResp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bootstrap/complete failed: err=%v status=%d body=%s", err, statusOf(bootResp), body)
	}

	// 3. Login to admin Control Plane & attach Secure session cookies explicitly for HTTP loopback.
	loginResp, loginBody, err := doJSON(ctx, client, http.MethodPost, baseURL+"/admin/v1/auth/login", "", "", nil, map[string]any{
		"email":    "quickstart@tanpopo.local",
		"password": "Quickstart-Correct-Horse-2026!",
	})
	if err != nil || loginResp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("login failed: err=%v status=%d body=%s", err, statusOf(loginResp), loginBody)
	}
	adminCookies := loginResp.Cookies()
	var loginDTO struct {
		CSRFToken string `json:"csrf_token"`
		OrgID     string `json:"org_id"`
	}
	_ = json.Unmarshal([]byte(loginBody), &loginDTO)
	if loginDTO.CSRFToken == "" {
		loginDTO.CSRFToken = loginResp.Header.Get("X-CSRF-Token")
	}

	if loginDTO.OrgID == "" {
		_, orgsBody, orgErr := doJSON(ctx, client, http.MethodGet, baseURL+"/admin/v1/organizations", loginDTO.CSRFToken, "", adminCookies, nil)
		if orgErr == nil {
			loginDTO.OrgID = extractFirstItemID(orgsBody)
		}
	}

	projectsResp, projectsBody, err := doJSON(ctx, client, http.MethodGet, baseURL+"/admin/v1/projects", loginDTO.CSRFToken, loginDTO.OrgID, adminCookies, nil)
	if err != nil {
		return 0, fmt.Errorf("list projects: %w", err)
	}
	projectID := extractFirstItemID(projectsBody)
	envID := ""
	if projectID != "" {
		envsResp, envsBody, envErr := doJSON(ctx, client, http.MethodGet, baseURL+"/admin/v1/projects/"+projectID+"/environments", loginDTO.CSRFToken, loginDTO.OrgID, adminCookies, nil)
		_ = envsResp
		if envErr == nil {
			envID = extractFirstItemID(envsBody)
		}
	}
	if projectID == "" || envID == "" {
		return 0, fmt.Errorf("unable to discover bootstrap project/env from API (status=%d projects=%s)", statusOf(projectsResp), projectsBody)
	}

	vkResp, vkBody, err := doJSON(ctx, client, http.MethodPost, baseURL+"/admin/v1/virtual-keys", loginDTO.CSRFToken, loginDTO.OrgID, adminCookies, map[string]any{
		"project_id":             projectID,
		"env_id":                 envID,
		"name":                   "quickstart-smoke",
		"allowed_models":         []string{"*"},
		"max_concurrent_streams": 5,
	})
	if err != nil || vkResp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("create virtual key failed: err=%v status=%d body=%s", err, statusOf(vkResp), vkBody)
	}
	var vkDTO struct {
		SecretKey string `json:"secret_key"`
	}
	_ = json.Unmarshal([]byte(vkBody), &vkDTO)
	if !strings.HasPrefix(vkDTO.SecretKey, "vk_tp_") {
		return 0, fmt.Errorf("unexpected virtual key secret format in %s", vkBody)
	}

	// 4. Verify Data Plane GET /v1/models authenticates with the newly created Virtual Key.
	dpClient := &http.Client{Timeout: 5 * time.Second}
	modelsReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Bearer "+vkDTO.SecretKey)
	modelsResp, err := waitHTTPStatus(ctx, dpClient, modelsReq, http.StatusOK)
	if err != nil {
		return 0, fmt.Errorf("GET /v1/models with Virtual Key: %w", err)
	}
	_ = modelsResp.Body.Close()

	// 5. Verify plane separation (Virtual Key rejected on Control Plane /admin/v1).
	cpRejectReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/admin/v1/overview/summary", nil)
	cpRejectReq.Header.Set("Authorization", "Bearer "+vkDTO.SecretKey)
	cpRejectResp, err := dpClient.Do(cpRejectReq)
	if err != nil {
		return 0, fmt.Errorf("plane separation probe: %w", err)
	}
	_ = cpRejectResp.Body.Close()
	if cpRejectResp.StatusCode != http.StatusUnauthorized {
		return 0, fmt.Errorf("expected 401 on /admin/v1 with Virtual Key, got %d", cpRejectResp.StatusCode)
	}

	// 6. Verify POST /v1/chat/completions responds with structured OpenAI JSON envelope.
	chatPayload := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`
	chatReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(chatPayload))
	chatReq.Header.Set("Authorization", "Bearer "+vkDTO.SecretKey)
	chatReq.Header.Set("Content-Type", "application/json")
	chatResp, err := dpClient.Do(chatReq)
	if err != nil {
		return 0, fmt.Errorf("POST /v1/chat/completions: %w", err)
	}
	chatRaw, _ := io.ReadAll(chatResp.Body)
	_ = chatResp.Body.Close()
	if chatResp.StatusCode != http.StatusOK &&
		!(chatResp.StatusCode == http.StatusServiceUnavailable && strings.Contains(string(chatRaw), "no_compliant_deployment_available")) {
		return 0, fmt.Errorf("POST /v1/chat/completions unexpected status %d: %s", chatResp.StatusCode, string(chatRaw))
	}

	return time.Since(started), nil
}

func waitReadyz(ctx context.Context, client *http.Client, url string) error {
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s: %w", url, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitReadBootstrapToken(ctx context.Context, path string) (string, error) {
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			tok := strings.TrimSpace(string(raw))
			if tok != "" {
				return tok, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func waitHTTPStatus(ctx context.Context, client *http.Client, req *http.Request, want int) (*http.Response, error) {
	var lastStatus int
	for {
		cloned := req.Clone(ctx)
		resp, err := client.Do(cloned)
		if err == nil {
			lastStatus = resp.StatusCode
			if resp.StatusCode == want {
				return resp, nil
			}
			_ = resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("last_status=%d: %w", lastStatus, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func doJSON(ctx context.Context, client *http.Client, method, url, csrf, orgID string, cookies []*http.Cookie, payload any) (*http.Response, string, error) {
	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, "", err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if orgID != "" {
		req.Header.Set("X-Tanpopo-Org-Id", orgID)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(raw), nil
}

func extractFirstItemID(rawJSON string) string {
	var envelope struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &envelope); err == nil {
		if len(envelope.Items) > 0 && envelope.Items[0].ID != "" {
			return envelope.Items[0].ID
		}
		if len(envelope.Data) > 0 && envelope.Data[0].ID != "" {
			return envelope.Data[0].ID
		}
	}
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &list); err == nil && len(list) > 0 {
		return list[0].ID
	}
	return ""
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
