// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Command check runs Tanpopo's cross-platform CI and Floor Guard quality gates
// (`CONSTRAINTS.md`, `TP-IMP-0005`) with zero shell/bash dependencies.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const (
	canonicalModule = "github.com/nguywnben/tanpopo"
	spikesModule    = "github.com/nguywnben/tanpopo-spikes"
)

// Violation describes a single deterministic quality gate failure.
type Violation struct {
	Check   string
	Rule    string
	File    string
	Line    int
	Message string
}

func (v Violation) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("[%s:%s] %s:%d: %s", v.Check, v.Rule, v.File, v.Line, v.Message)
	}
	if v.File != "" {
		return fmt.Sprintf("[%s:%s] %s: %s", v.Check, v.Rule, v.File, v.Message)
	}
	return fmt.Sprintf("[%s:%s] %s", v.Check, v.Rule, v.Message)
}

type checkOptions struct {
	rootDir     string
	runAll      bool
	pureGoBuild bool
	dag         bool
	floorGuard  bool
	license     bool
	lockfiles   bool
	canary      bool
	uiLayout    bool
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	var opts checkOptions
	fsFlags := flag.NewFlagSet("check", flag.ContinueOnError)
	fsFlags.SetOutput(stderr)
	fsFlags.StringVar(&opts.rootDir, "root", ".", "repository root directory")
	fsFlags.BoolVar(&opts.runAll, "all", false, "run all checks")
	fsFlags.BoolVar(&opts.pureGoBuild, "pure-go-build", false, "verify CGO_ENABLED=0 pure-Go build of ./cmd/tanpopo")
	fsFlags.BoolVar(&opts.dag, "dag", false, "verify one-way Go package DAG and banned imports")
	fsFlags.BoolVar(&opts.floorGuard, "floor-guard", false, "verify CONSTRAINTS.md floor rules")
	fsFlags.BoolVar(&opts.license, "license", false, "verify Apache-2.0 metadata and 0 copyleft deps")
	fsFlags.BoolVar(&opts.lockfiles, "lockfiles", false, "verify single-lockfile discipline across npm and Go")
	fsFlags.BoolVar(&opts.canary, "canary", false, "verify zero leaked secrets or canary tokens")
	fsFlags.BoolVar(&opts.uiLayout, "ui-layout", false, "verify Astryx Web Console layout, English strings, and theme integrity")

	if err := fsFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if opts.runAll || (!opts.pureGoBuild && !opts.dag && !opts.floorGuard && !opts.license && !opts.lockfiles && !opts.canary && !opts.uiLayout) {
		opts.pureGoBuild = true
		opts.dag = true
		opts.floorGuard = true
		opts.license = true
		opts.lockfiles = true
		opts.canary = true
		opts.uiLayout = true
	}

	absRoot, err := filepath.Abs(opts.rootDir)
	if err != nil {
		fmt.Fprintf(stderr, "check: resolve root: %v\n", err)
		return 2
	}
	opts.rootDir = absRoot

	violations, execErr := runChecks(opts)
	if execErr != nil {
		fmt.Fprintf(stderr, "check: execution error: %v\n", execErr)
		return 2
	}
	if len(violations) > 0 {
		sort.Slice(violations, func(i, j int) bool {
			if violations[i].File != violations[j].File {
				return violations[i].File < violations[j].File
			}
			if violations[i].Line != violations[j].Line {
				return violations[i].Line < violations[j].Line
			}
			return violations[i].Rule < violations[j].Rule
		})
		for _, v := range violations {
			fmt.Fprintln(stderr, "FAIL:", v.String())
		}
		fmt.Fprintf(stderr, "check: %d violation(s) found\n", len(violations))
		return 1
	}

	fmt.Fprintln(stdout, "PASS: all selected Tanpopo quality gates succeeded")
	return 0
}

func runChecks(opts checkOptions) ([]Violation, error) {
	var all []Violation

	if opts.dag {
		v, err := checkDAG(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}
	if opts.floorGuard {
		v, err := checkFloorGuard(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}
	if opts.license {
		v, err := checkLicense(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}
	if opts.lockfiles {
		v, err := checkLockfiles(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}
	if opts.canary {
		v, err := checkCanaryAndSecrets(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}
	if opts.uiLayout {
		v, err := checkUILayout(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}
	if opts.pureGoBuild {
		v, err := checkPureGoBuild(opts.rootDir)
		if err != nil {
			return nil, err
		}
		all = append(all, v...)
	}

	return all, nil
}

func checkPureGoBuild(rootDir string) ([]Violation, error) {
	tmpDir, err := os.MkdirTemp("", "tanpopo-ci-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	binName := "tanpopo"
	if runtime.GOOS == "windows" {
		binName = "tanpopo.exe"
	}
	outBin := filepath.Join(tmpDir, binName)
	cmd := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-o", outBin, "./cmd/tanpopo")
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		return []Violation{{
			Check:   "pure-go-build",
			Rule:    "cgo-disabled-build",
			File:    "cmd/tanpopo/main.go",
			Message: fmt.Sprintf("CGO_ENABLED=0 go build failed: %v\n%s", err, strings.TrimSpace(string(output))),
		}}, nil
	}
	return nil, nil
}

// packageTier maps canonical internal/cmd package paths into monotonic DAG layers.
// Returns (tier, recognized). Tier -1 marks internal/testkit.
func packageTier(relPkg string) (int, bool) {
	p := filepath.ToSlash(strings.TrimSuffix(relPkg, "/"))
	switch {
	case p == "internal/pkg" || strings.HasPrefix(p, "internal/pkg/"):
		return 0, true
	case p == "internal/storage/port" || strings.HasPrefix(p, "internal/storage/port/"):
		return 1, true
	case p == "internal/storage" || strings.HasPrefix(p, "internal/storage/"):
		return 2, true
	case p == "internal/security" || strings.HasPrefix(p, "internal/security/"):
		return 3, true
	case p == "internal/config" || strings.HasPrefix(p, "internal/config/"):
		return 4, true
	case p == "internal/testkit" || strings.HasPrefix(p, "internal/testkit/"):
		return -1, true
	case hasAnyPrefix(p,
		"internal/iam", "internal/extension", "internal/provider", "internal/policy",
		"internal/ratelimit", "internal/routing", "internal/finops", "internal/telemetry", "internal/webhook"):
		return 5, true
	case hasAnyPrefix(p, "internal/ingress", "internal/admin", "internal/webui", "internal/ops"):
		return 6, true
	case p == "internal/cli" || strings.HasPrefix(p, "internal/cli/"):
		return 7, true
	case p == "cmd/tanpopo" || strings.HasPrefix(p, "cmd/tanpopo/"):
		return 8, true
	default:
		return 0, false
	}
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if s == p || strings.HasPrefix(s, p+"/") {
			return true
		}
	}
	return false
}

var canonicalModuleLineRe = regexp.MustCompile(`(?m)^module\s+` + regexp.QuoteMeta(canonicalModule) + `\s*$`)

func checkDAG(rootDir string) ([]Violation, error) {
	var violations []Violation

	goModPath := filepath.Join(rootDir, "go.mod")
	modRaw, err := os.ReadFile(goModPath)
	if err != nil {
		violations = append(violations, Violation{
			Check:   "dag",
			Rule:    "go-mod-missing",
			File:    "go.mod",
			Message: fmt.Sprintf("unable to read root go.mod: %v", err),
		})
		return violations, nil
	}
	modText := strings.ReplaceAll(string(modRaw), "\r\n", "\n")
	if !canonicalModuleLineRe.MatchString(modText) {
		violations = append(violations, Violation{
			Check:   "dag",
			Rule:    "canonical-module-path",
			File:    "go.mod",
			Message: fmt.Sprintf("root go.mod must declare exact line `module %s`", canonicalModule),
		})
	}
	if strings.Contains(modText, "tools/spikes") || strings.Contains(modText, spikesModule) {
		violations = append(violations, Violation{
			Check:   "dag",
			Rule:    "ban-tools-spikes-import",
			File:    "go.mod",
			Message: "root go.mod must never require or replace tools/spikes or tanpopo-spikes",
		})
	}

	graph := make(map[string]map[string]bool)
	fset := token.NewFileSet()

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".worktrees" || name == "testdata" {
				return filepath.SkipDir
			}
			relDir, relErr := filepath.Rel(rootDir, path)
			if relErr != nil {
				return relErr
			}
			relDir = filepath.ToSlash(relDir)
			if relDir == "tools" || relDir == "scripts" || relDir == "web" || strings.HasPrefix(relDir, "tools/") || strings.HasPrefix(relDir, "scripts/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		relFile, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return relErr
		}
		relFile = filepath.ToSlash(relFile)
		relPkg := filepath.ToSlash(filepath.Dir(relFile))
		isTestFile := strings.HasSuffix(d.Name(), "_test.go")

		fileAST, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			violations = append(violations, Violation{
				Check:   "dag",
				Rule:    "go-parse",
				File:    relFile,
				Message: fmt.Sprintf("failed parsing Go imports: %v", parseErr),
			})
			return nil
		}

		srcTier, srcKnown := packageTier(relPkg)
		if strings.HasPrefix(relPkg, "internal/") && !srcKnown {
			violations = append(violations, Violation{
				Check:   "dag",
				Rule:    "unregistered-internal-package",
				File:    relFile,
				Message: fmt.Sprintf("unregistered internal package %q must be assigned a canonical DAG tier in scripts/ci/check.go", relPkg),
			})
		}

		if !isTestFile && graph[relPkg] == nil {
			graph[relPkg] = make(map[string]bool)
		}

		for _, imp := range fileAST.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			line := fset.Position(imp.Pos()).Line

			if impPath == canonicalModule+"/tools/spikes" ||
				strings.HasPrefix(impPath, canonicalModule+"/tools/spikes/") ||
				impPath == spikesModule ||
				strings.HasPrefix(impPath, spikesModule+"/") {
				violations = append(violations, Violation{
					Check:   "dag",
					Rule:    "ban-tools-spikes-import",
					File:    relFile,
					Line:    line,
					Message: "importing tools/spikes or tanpopo-spikes from root module code is strictly forbidden",
				})
				continue
			}

			if !strings.HasPrefix(impPath, canonicalModule+"/") {
				continue
			}
			dstRel := strings.TrimPrefix(impPath, canonicalModule+"/")
			dstTier, dstKnown := packageTier(dstRel)

			if dstKnown && dstTier == -1 && !isTestFile && !(srcKnown && srcTier == -1) {
				violations = append(violations, Violation{
					Check:   "dag",
					Rule:    "ban-prod-testkit-import",
					File:    relFile,
					Line:    line,
					Message: "production Go files must never import internal/testkit",
				})
				continue
			}

			if !isTestFile {
				if relPkg != dstRel {
					graph[relPkg][dstRel] = true
				}
				if srcKnown && dstKnown && srcTier >= 0 && dstTier >= 0 && srcTier < dstTier {
					violations = append(violations, Violation{
						Check:   "dag",
						Rule:    "dag-layer-violation",
						File:    relFile,
						Line:    line,
						Message: fmt.Sprintf("package %s (tier %d) cannot import higher-layer package %s (tier %d)", relPkg, srcTier, dstRel, dstTier),
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Cycle detection using 3-color DFS across all non-test imports in root module.
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(graph))
	nodes := make([]string, 0, len(graph))
	for k := range graph {
		nodes = append(nodes, k)
	}
	sort.Strings(nodes)

	var dfs func(node string, trail []string)
	dfs = func(node string, trail []string) {
		state[node] = visiting
		edges := make([]string, 0, len(graph[node]))
		for dst := range graph[node] {
			edges = append(edges, dst)
		}
		sort.Strings(edges)
		for _, dst := range edges {
			switch state[dst] {
			case visiting:
				violations = append(violations, Violation{
					Check:   "dag",
					Rule:    "import-cycle",
					File:    node,
					Message: fmt.Sprintf("import cycle detected: %s -> %s", strings.Join(trail, " -> "), dst),
				})
			case unvisited:
				nextTrail := make([]string, len(trail)+1)
				copy(nextTrail, trail)
				nextTrail[len(trail)] = dst
				dfs(dst, nextTrail)
			}
		}
		state[node] = done
	}
	for _, n := range nodes {
		if state[n] == unvisited {
			dfs(n, []string{n})
		}
	}

	return violations, nil
}

type floorRule struct {
	name    string
	pattern *regexp.Regexp
	message string
}

// floorRules constructs banned patterns dynamically so check.go never trips its own scanner.
var (
	floorRules = []floorRule{
		{
			name:    "no-ts-ignore",
			pattern: regexp.MustCompile(`@ts-` + `ignore|@ts-` + `expect-error|@ts-` + `nocheck`),
			message: "TypeScript suppression directive is forbidden by CONSTRAINTS.md",
		},
		{
			name:    "no-eslint-biome-disable",
			pattern: regexp.MustCompile(`eslint-` + `disable|biome-` + `ignore`),
			message: "JS/TS linter suppression directive is forbidden by CONSTRAINTS.md",
		},
		{
			name:    "no-gosec-nolint",
			pattern: regexp.MustCompile(`(?://|/\*)\s*` + `nolint(?:\s*(?:\n|$|\*/|:[^\n*]*\b(?:gosec|all)\b))`),
			message: "Go nolint directive (bare, all, or gosec) is forbidden by CONSTRAINTS.md",
		},
		{
			name:    "no-sec-allow",
			pattern: regexp.MustCompile(`gitleaks:` + `allow|\bnosemgrep\b|Stryker ` + `disable|istanbul ` + `ignore`),
			message: "Security/coverage scanner suppression directive is forbidden by CONSTRAINTS.md",
		},
		{
			name:    "no-test-skip",
			pattern: regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.` + `Skip(?:Now|f)?\s*\(|\.(?:skip|only)(?:\.each)?\s*\(|\b(?:xit|xdescribe|xtest|test\.fixme)\s*\(|@pytest\.mark\.` + `skip\b`),
			message: "Unticketed test skip/isolation is forbidden by CONSTRAINTS.md",
		},
		{
			name:    "no-stubs-or-empty-catch",
			pattern: regexp.MustCompile(`throw\s+new\s+Error\(\s*["']not implemented["']\s*\)|panic\(\s*["']not implemented["']\s*\)|catch\s*(?:\([^)]*\))?\s*\{\s*\}`),
			message: "Unimplemented stub or empty catch block is forbidden by CONSTRAINTS.md",
		},
	}

	rawHTMLTagRe = regexp.MustCompile(`<\s*(?:div|span)\b`)
)

func lineNumberAt(text string, offset int) int {
	if offset <= 0 {
		return 1
	}
	if offset > len(text) {
		offset = len(text)
	}
	return 1 + strings.Count(text[:offset], "\n")
}

func checkFloorGuard(rootDir string) ([]Violation, error) {
	var violations []Violation

	codeRoots := []string{"cmd", "internal", "scripts", "tests", filepath.Join("web", "console", "src")}
	for _, relRoot := range codeRoots {
		fullRoot := filepath.Join(rootDir, relRoot)
		if _, err := os.Stat(fullRoot); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(fullRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".git" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}

			ext := strings.ToLower(filepath.Ext(d.Name()))
			switch ext {
			case ".go", ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".yml", ".yaml":
			default:
				return nil
			}

			relFile, relErr := filepath.Rel(rootDir, path)
			if relErr != nil {
				return relErr
			}
			relFile = filepath.ToSlash(relFile)

			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			norm := strings.ReplaceAll(string(raw), "\r\n", "\n")

			for _, rule := range floorRules {
				for _, loc := range rule.pattern.FindAllStringIndex(norm, -1) {
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    rule.name,
						File:    relFile,
						Line:    lineNumberAt(norm, loc[0]),
						Message: rule.message,
					})
				}
			}

			if strings.HasPrefix(relFile, "web/console/src/") && ext != ".css" {
				for _, loc := range rawHTMLTagRe.FindAllStringIndex(norm, -1) {
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "no-raw-div-span",
						File:    relFile,
						Line:    lineNumberAt(norm, loc[0]),
						Message: "raw HTML div/span elements are forbidden in web/console/src (use Astryx layout primitives)",
					})
				}
			}

			if ext == ".go" && (strings.HasPrefix(relFile, "cmd/") || strings.HasPrefix(relFile, "internal/") || strings.HasPrefix(relFile, "tests/")) {
				v, astErr := checkBannedGoStdlibHTTP(relFile, raw)
				if astErr != nil {
					return astErr
				}
				violations = append(violations, v...)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return violations, nil
}

func checkBannedGoStdlibHTTP(relFile string, src []byte) ([]Violation, error) {
	fset := token.NewFileSet()
	fileAST, err := parser.ParseFile(fset, relFile, src, 0)
	if err != nil {
		return []Violation{{
			Check:   "floor-guard",
			Rule:    "go-parse",
			File:    relFile,
			Message: fmt.Sprintf("invalid Go syntax: %v", err),
		}}, nil
	}

	isNetguardImpl := relFile == "internal/testkit/netguard/netguard.go"         // CONSTRAINTS.md E3
	isNetguardDefTests := relFile == "internal/testkit/netguard/netguard_default_test.go" // CONSTRAINTS.md E4

	var violations []Violation
	var netHTTPLocalNames []string
	var netLocalNames []string
	var tlsLocalNames []string
	for _, imp := range fileAST.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		switch impPath {
		case "net/http":
			if imp.Name != nil && imp.Name.Name == "." {
				violations = append(violations, Violation{
					Check:   "floor-guard",
					Rule:    "ban-http-dot-import",
					File:    relFile,
					Line:    fset.Position(imp.Pos()).Line,
					Message: "dot-import of net/http is forbidden in cmd/, internal/, and tests/",
				})
				continue
			}
			name := "http"
			if imp.Name != nil && imp.Name.Name != "_" {
				name = imp.Name.Name
			}
			if imp.Name == nil || imp.Name.Name != "_" {
				netHTTPLocalNames = append(netHTTPLocalNames, name)
			}
		case "net":
			if imp.Name != nil && imp.Name.Name == "." {
				violations = append(violations, Violation{
					Check:   "floor-guard",
					Rule:    "ban-net-dot-import",
					File:    relFile,
					Line:    fset.Position(imp.Pos()).Line,
					Message: "dot-import of net is forbidden in cmd/, internal/, and tests/",
				})
				continue
			}
			name := "net"
			if imp.Name != nil && imp.Name.Name != "_" {
				name = imp.Name.Name
			}
			if imp.Name == nil || imp.Name.Name != "_" {
				netLocalNames = append(netLocalNames, name)
			}
		case "crypto/tls":
			if imp.Name != nil && imp.Name.Name == "." {
				violations = append(violations, Violation{
					Check:   "floor-guard",
					Rule:    "ban-tls-dot-import",
					File:    relFile,
					Line:    fset.Position(imp.Pos()).Line,
					Message: "dot-import of crypto/tls is forbidden in cmd/, internal/, and tests/",
				})
				continue
			}
			name := "tls"
			if imp.Name != nil && imp.Name.Name != "_" {
				name = imp.Name.Name
			}
			if imp.Name == nil || imp.Name.Name != "_" {
				tlsLocalNames = append(tlsLocalNames, name)
			}
		}
	}
	if len(netHTTPLocalNames) == 0 && len(netLocalNames) == 0 && len(tlsLocalNames) == 0 {
		return violations, nil
	}

	containsName := func(list []string, name string) bool {
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}

	isNilExpr := func(expr ast.Expr) bool {
		if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
		// Catch typed nil conversions such as (*http.Transport)(nil).
		if call, ok := expr.(*ast.CallExpr); ok && len(call.Args) == 1 {
			if argID, ok := call.Args[0].(*ast.Ident); ok && argID.Name == "nil" {
				return true
			}
		}
		return false
	}

	hasCompositeKey := func(lit *ast.CompositeLit, keyName string) bool {
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if keyIdent, ok := kv.Key.(*ast.Ident); ok && keyIdent.Name == keyName {
				return !isNilExpr(kv.Value)
			}
		}
		return false
	}

	unwrapSelector := func(expr ast.Expr) (pkgName, symName string, ok bool) {
		for {
			if se, isStar := expr.(*ast.StarExpr); isStar {
				expr = se.X
				continue
			}
			break
		}
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return "", "", false
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return "", "", false
		}
		return ident.Name, sel.Sel.Name, true
	}

	checkTypedCompositeLit := func(typeExpr ast.Expr, lit *ast.CompositeLit) {
		pkg, sym, ok := unwrapSelector(typeExpr)
		if !ok {
			return
		}
		line := fset.Position(lit.Pos()).Line
		if containsName(netHTTPLocalNames, pkg) {
			switch sym {
			case "Client":
				if !isNetguardDefTests && !hasCompositeKey(lit, "Transport") {
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-http-client-nil-transport",
						File:    relFile,
						Line:    line,
						Message: "http.Client composite literals must set a non-nil SSRF-guarded or netguard Transport",
					})
				}
			case "Transport":
				if !hasCompositeKey(lit, "DialContext") {
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-http-transport-nil-dial",
						File:    relFile,
						Line:    line,
						Message: "http.Transport composite literals must set a non-nil DialContext (SSRFGuard / netguard)",
					})
				}
			}
		}
		if containsName(netLocalNames, pkg) && sym == "Dialer" {
			if !hasCompositeKey(lit, "Control") {
				violations = append(violations, Violation{
					Check:   "floor-guard",
					Rule:    "ban-net-dialer-nil-control",
					File:    relFile,
					Line:    line,
					Message: "net.Dialer composite literals must set a non-nil Control hook (SSRFGuard / netguard.Control)",
				})
			}
		}
		if containsName(tlsLocalNames, pkg) && sym == "Dialer" {
			if !hasCompositeKey(lit, "NetDialer") {
				violations = append(violations, Violation{
					Check:   "floor-guard",
					Rule:    "ban-tls-dialer-nil-netdialer",
					File:    relFile,
					Line:    line,
					Message: "tls.Dialer composite literals must set a non-nil NetDialer (SSRFGuard / netguard.NewDialer)",
				})
			}
		}
	}

	isGuardedValueType := func(expr ast.Expr) (string, bool) {
		// Pointer variable declaration (`var tr *http.Transport`) initializes to nil until assigned;
		// value struct declarations (`var d net.Dialer`) allocate unguarded zero-value instances.
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		if containsName(netHTTPLocalNames, ident.Name) && (sel.Sel.Name == "Client" || sel.Sel.Name == "Transport") {
			return "net/http." + sel.Sel.Name, true
		}
		if containsName(netLocalNames, ident.Name) && sel.Sel.Name == "Dialer" {
			return "net.Dialer", true
		}
		if containsName(tlsLocalNames, ident.Name) && sel.Sel.Name == "Dialer" {
			return "crypto/tls.Dialer", true
		}
		return "", false
	}

	ast.Inspect(fileAST, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			ident, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			if containsName(netHTTPLocalNames, ident.Name) {
				switch node.Sel.Name {
				case "DefaultClient", "DefaultTransport":
					if isNetguardImpl || isNetguardDefTests {
						return true
					}
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-http-default-client",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: fmt.Sprintf("net/http.%s is forbidden outside scoped netguard E3/E4 allowlist (use injected SSRFGuard HTTP client)", node.Sel.Name),
					})
				case "Get", "Post", "PostForm", "Head":
					if isNetguardDefTests {
						return true
					}
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-http-default-client",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: fmt.Sprintf("net/http.%s is forbidden in cmd/, internal/, and tests/ outside E4 negative test (use injected SSRFGuard HTTP client)", node.Sel.Name),
					})
				}
			}
			if containsName(netLocalNames, ident.Name) {
				switch node.Sel.Name {
				case "Dial", "DialTimeout", "DialTCP", "DialUDP", "DialIP":
					if isNetguardDefTests {
						return true
					}
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-unguarded-net-dial",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: fmt.Sprintf("net.%s is forbidden outside scoped netguard E4 negative test (use net.Dialer with Control / SSRFGuard)", node.Sel.Name),
					})
				case "DefaultResolver":
					if isNetguardImpl {
						return true
					}
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-net-default-resolver",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: "net.DefaultResolver mutation/bypass is forbidden outside internal/testkit/netguard/netguard.go (E3)",
					})
				}
			}
			if containsName(tlsLocalNames, ident.Name) {
				switch node.Sel.Name {
				case "Dial", "DialWithDialer":
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-tls-unguarded-dial",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: fmt.Sprintf("crypto/tls.%s is forbidden (use tls.Dialer with SSRFGuard / netguard NetDialer)", node.Sel.Name),
					})
				}
			}
		case *ast.ValueSpec:
			if len(node.Values) == 0 && node.Type != nil {
				if desc, bad := isGuardedValueType(node.Type); bad {
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-unguarded-var-decl",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: fmt.Sprintf("zero-value var declaration of %s bypasses Transport/Control/DialContext floor guard", desc),
					})
				}
			}
		case *ast.CallExpr:
			if fnIdent, ok := node.Fun.(*ast.Ident); ok && fnIdent.Name == "new" && len(node.Args) == 1 {
				if desc, bad := isGuardedValueType(node.Args[0]); bad {
					violations = append(violations, Violation{
						Check:   "floor-guard",
						Rule:    "ban-unguarded-new-alloc",
						File:    relFile,
						Line:    fset.Position(node.Pos()).Line,
						Message: fmt.Sprintf("new(%s) bypasses Transport/Control/DialContext floor guard", desc),
					})
				}
			}
		case *ast.CompositeLit:
			if node.Type != nil {
				checkTypedCompositeLit(node.Type, node)
				var elemType ast.Expr
				switch t := node.Type.(type) {
				case *ast.ArrayType:
					elemType = t.Elt
				case *ast.MapType:
					elemType = t.Value
				}
				if elemType != nil {
					for _, elt := range node.Elts {
						val := elt
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							val = kv.Value
						}
						if innerLit, ok := val.(*ast.CompositeLit); ok && innerLit.Type == nil {
							checkTypedCompositeLit(elemType, innerLit)
						}
					}
				}
			}
		}
		return true
	})

	return violations, nil
}

var copyleftLicenseRe = regexp.MustCompile(`(?i)\b(A?GPL(?:-[0-9.]+)?|SSPL(?:-[0-9.]+)?)\b`)

func checkLicense(rootDir string) ([]Violation, error) {
	var violations []Violation

	licRaw, err := os.ReadFile(filepath.Join(rootDir, "LICENSE"))
	if err != nil {
		violations = append(violations, Violation{
			Check:   "license",
			Rule:    "root-license-missing",
			File:    "LICENSE",
			Message: fmt.Sprintf("missing root LICENSE file: %v", err),
		})
	} else if !strings.Contains(string(licRaw), "Apache License") || !strings.Contains(string(licRaw), "Version 2.0") {
		violations = append(violations, Violation{
			Check:   "license",
			Rule:    "root-license-apache2",
			File:    "LICENSE",
			Message: "root LICENSE must be Apache License, Version 2.0",
		})
	}

	pkgPath := filepath.Join(rootDir, "web", "console", "package.json")
	pkgRaw, err := os.ReadFile(pkgPath)
	if err != nil {
		violations = append(violations, Violation{
			Check:   "license",
			Rule:    "console-package-json-missing",
			File:    "web/console/package.json",
			Message: fmt.Sprintf("missing web/console/package.json: %v", err),
		})
	} else {
		var pkg struct {
			License         string            `json:"license"`
			Type            string            `json:"type"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if err := json.Unmarshal(pkgRaw, &pkg); err != nil {
			violations = append(violations, Violation{
				Check:   "license",
				Rule:    "console-package-json-parse",
				File:    "web/console/package.json",
				Message: fmt.Sprintf("invalid JSON in web/console/package.json: %v", err),
			})
		} else {
			if pkg.License != "Apache-2.0" {
				violations = append(violations, Violation{
					Check:   "license",
					Rule:    "console-license-apache2",
					File:    "web/console/package.json",
					Message: fmt.Sprintf("expected license \"Apache-2.0\", got %q (ISS-002)", pkg.License),
				})
			}
			if pkg.Type != "module" {
				violations = append(violations, Violation{
					Check:   "license",
					Rule:    "console-type-module",
					File:    "web/console/package.json",
					Message: fmt.Sprintf("expected type \"module\", got %q (ISS-004)", pkg.Type),
				})
			}
			if pkg.Dependencies["react"] == "" || pkg.Dependencies["react-dom"] == "" {
				violations = append(violations, Violation{
					Check:   "license",
					Rule:    "console-explicit-react-deps",
					File:    "web/console/package.json",
					Message: "dependencies must explicitly declare both \"react\" and \"react-dom\" (ISS-003)",
				})
			}
			if pkg.Dependencies["@astryxdesign/cli"] != "" || pkg.DevDependencies["@astryxdesign/cli"] == "" {
				violations = append(violations, Violation{
					Check:   "license",
					Rule:    "console-cli-devdep",
					File:    "web/console/package.json",
					Message: "\"@astryxdesign/cli\" must be declared under devDependencies rather than dependencies (ISS-004)",
				})
			}
		}
	}

	lockPath := filepath.Join(rootDir, "web", "console", "package-lock.json")
	lockRaw, lockErr := os.ReadFile(lockPath)
	if lockErr != nil {
		violations = append(violations, Violation{
			Check:   "license",
			Rule:    "missing-console-lockfile",
			File:    "web/console/package-lock.json",
			Message: fmt.Sprintf("missing web/console/package-lock.json: %v", lockErr),
		})
	} else {
		var lockDoc struct {
			Packages map[string]struct {
				License string `json:"license"`
			} `json:"packages"`
		}
		if jsonErr := json.Unmarshal(lockRaw, &lockDoc); jsonErr != nil {
			violations = append(violations, Violation{
				Check:   "license",
				Rule:    "package-lock-parse",
				File:    "web/console/package-lock.json",
				Message: fmt.Sprintf("invalid JSON: %v", jsonErr),
			})
		} else {
			for pkgName, meta := range lockDoc.Packages {
				if pkgName == "" && meta.License != "" && meta.License != "Apache-2.0" {
					violations = append(violations, Violation{
						Check:   "license",
						Rule:    "package-lock-root-license",
						File:    "web/console/package-lock.json",
						Message: fmt.Sprintf("root package-lock entry license must be Apache-2.0, got %q", meta.License),
					})
				}
				if copyleftLicenseRe.MatchString(meta.License) {
					violations = append(violations, Violation{
						Check:   "license",
						Rule:    "no-copyleft-dependency",
						File:    "web/console/package-lock.json",
						Message: fmt.Sprintf("forbidden copyleft license %q on %s", meta.License, pkgName),
					})
				}
			}
		}
	}

	// Explicitly validate .agents/plugin.json as documented allowlist (upstream MIT).
	agentPlugin := filepath.Join(rootDir, ".agents", "plugin.json")
	if raw, err := os.ReadFile(agentPlugin); err == nil {
		var meta struct {
			License string `json:"license"`
		}
		if jsonErr := json.Unmarshal(raw, &meta); jsonErr == nil && copyleftLicenseRe.MatchString(meta.License) {
			violations = append(violations, Violation{
				Check:   "license",
				Rule:    "no-copyleft-tooling",
				File:    ".agents/plugin.json",
				Message: fmt.Sprintf("forbidden copyleft license %q in .agents/plugin.json", meta.License),
			})
		}
	}

	return violations, nil
}

func checkLockfiles(rootDir string) ([]Violation, error) {
	var violations []Violation

	for _, rootLegacy := range []string{"package.json", "package-lock.json", "astryx-themes.js"} {
		if _, err := os.Stat(filepath.Join(rootDir, rootLegacy)); err == nil {
			violations = append(violations, Violation{
				Check:   "lockfiles",
				Rule:    "relocated-root-ui-cleanup",
				File:    rootLegacy,
				Message: fmt.Sprintf("legacy root file %s must be relocated under web/console/", rootLegacy),
			})
		}
	}

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == ".worktrees" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		switch d.Name() {
		case "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "npm-shrinkwrap.json":
			violations = append(violations, Violation{
				Check:   "lockfiles",
				Rule:    "single-npm-lockfile",
				File:    relSlash,
				Message: "competing JS package-manager lockfile is forbidden (use web/console/package-lock.json only)",
			})
		case "package-lock.json":
			if relSlash != "web/console/package-lock.json" {
				violations = append(violations, Violation{
					Check:   "lockfiles",
					Rule:    "single-npm-lockfile",
					File:    relSlash,
					Message: "secondary package-lock.json outside web/console/package-lock.json is forbidden",
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(rootDir, "web", "console", "package-lock.json")); err != nil {
		violations = append(violations, Violation{
			Check:   "lockfiles",
			Rule:    "missing-console-lockfile",
			File:    "web/console/package-lock.json",
			Message: "web/console/package-lock.json is required for reproducible npm ci --ignore-scripts",
		})
	}

	return violations, nil
}

var (
	canaryForbiddenToken = "CANARY_FORBIDDEN_" + "CI_KEY"
	pemPrivateKeyRe      = regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |ENCRYPTED |OPENSSH )?PRIVATE KEY-----`)
	liveOrCanaryKeyRe    = regexp.MustCompile(`sk-tp-` + `(?:live|prod|canary)-[A-Za-z0-9_-]{4,}`)
)

func checkCanaryAndSecrets(rootDir string) ([]Violation, error) {
	var violations []Violation

	for _, relRoot := range []string{"cmd", "internal", "scripts", filepath.Join("web", "console")} {
		fullRoot := filepath.Join(rootDir, relRoot)
		if _, err := os.Stat(fullRoot); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(fullRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == "package-lock.json" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relFile, relErr := filepath.Rel(rootDir, path)
			if relErr != nil {
				return relErr
			}
			relFile = filepath.ToSlash(relFile)

			lines := strings.Split(string(raw), "\n")
			for i, line := range lines {
				if strings.Contains(line, canaryForbiddenToken) {
					violations = append(violations, Violation{
						Check:   "canary",
						Rule:    "canary-token-leak",
						File:    relFile,
						Line:    i + 1,
						Message: "forbidden CI canary key literal detected (value redacted)",
					})
				}
				if pemPrivateKeyRe.MatchString(line) || liveOrCanaryKeyRe.MatchString(line) {
					violations = append(violations, Violation{
						Check:   "canary",
						Rule:    "secret-material-detected",
						File:    relFile,
						Line:    i + 1,
						Message: "hardcoded private key, canary secret, or live API key detected (value redacted)",
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return violations, nil
}

var vnDiacriticRe = regexp.MustCompile("[\u00e0-\u00e3\u00e8-\u00ea\u00ec-\u00ed\u00f2-\u00f5\u00f9-\u00fa\u00fd\u0103\u0111\u0129\u0169\u01a1\u01b0\u1ea0-\u1ef9\u00c0-\u00c3\u00c8-\u00ca\u00cc-\u00cd\u00d2-\u00d5\u00d9-\u00da\u00dd\u0102\u0110\u0128\u0168\u01a0\u01af]")

const expectedThemeRecord = `@astryx-theme-record {"themes":{"neutral":"@astryxdesign/theme-neutral"},"defaultThemeSlug":"neutral"}`

func checkUILayout(rootDir string) ([]Violation, error) {
	var violations []Violation

	themesPath := filepath.Join(rootDir, "web", "console", "astryx-themes.js")
	themesRaw, err := os.ReadFile(themesPath)
	if err != nil {
		violations = append(violations, Violation{
			Check:   "ui-layout",
			Rule:    "astryx-themes-missing",
			File:    "web/console/astryx-themes.js",
			Message: fmt.Sprintf("missing web/console/astryx-themes.js: %v", err),
		})
	} else {
		s := string(themesRaw)
		if !strings.Contains(s, "@generated by `astryx theme`") ||
			!strings.Contains(s, expectedThemeRecord) ||
			!strings.Contains(s, `defaultThemeSlug = "neutral"`) {
			violations = append(violations, Violation{
				Check:   "ui-layout",
				Rule:    "astryx-theme-unmodified-neutral",
				File:    "web/console/astryx-themes.js",
				Message: "web/console/astryx-themes.js must preserve its @generated marker, @astryx-theme-record, and defaultThemeSlug = \"neutral\"",
			})
		}
		if strings.Contains(s, ":root") {
			violations = append(violations, Violation{
				Check:   "ui-layout",
				Rule:    "no-root-css-override",
				File:    "web/console/astryx-themes.js",
				Message: ":root CSS overrides are forbidden by AGENTS.md Astryx rules",
			})
		}
		if strings.Contains(s, "\r\n") {
			violations = append(violations, Violation{
				Check:   "ui-layout",
				Rule:    "theme-module-must-be-lf",
				File:    "web/console/astryx-themes.js",
				Message: "web/console/astryx-themes.js must use LF line endings (required for Astryx doctor theme-module check on Windows; enforce via .gitattributes)",
			})
		}
	}

	srcDir := filepath.Join(rootDir, "web", "console", "src")
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		violations = append(violations, Violation{
			Check:   "ui-layout",
			Rule:    "console-src-missing",
			File:    "web/console/src",
			Message: "web/console/src directory must exist",
		})
		return violations, nil
	}

	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(strings.ToLower(d.Name()), ".css") {
			violations = append(violations, Violation{
				Check:   "ui-layout",
				Rule:    "no-custom-css-in-src",
				File:    rel,
				Message: "custom CSS files in web/console/src are forbidden (use Astryx components & tokens)",
			})
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if vnDiacriticRe.MatchString(line) {
				violations = append(violations, Violation{
					Check:   "ui-layout",
					Rule:    "english-ui-copy",
					File:    rel,
					Line:    i + 1,
					Message: "non-English Vietnamese text found in web/console/src (ISS-008)",
				})
			}
			if strings.Contains(line, "fonts.googleapis.com") {
				violations = append(violations, Violation{
					Check:   "ui-layout",
					Rule:    "air-gap-no-google-fonts",
					File:    rel,
					Line:    i + 1,
					Message: "external Google Fonts URLs are forbidden in web/console/src (CR-10 / NEG-2)",
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return violations, nil
}
