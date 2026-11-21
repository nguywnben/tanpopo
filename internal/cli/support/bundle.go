// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package support implements secret-scrubbed local diagnostics archives.
package support

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/security/canary"
	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
	"gopkg.in/yaml.v3"
)

var masterKeyAssignment = regexp.MustCompile(`(?i)(TANPOPO_MASTER_KEY(?:_FILE)?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)

const (
	maxInputBytes     = 1 << 20
	maxBundleBytes    = 16 << 20
	maxLogFiles       = 500
	maxLogScanEntries = 10000
)

// Options configures local diagnostics collection.
type Options struct {
	RootDir      string
	Output       string
	OutputFormat *string
	Stdout       io.Writer
	Locale       i18n.Locale
}

func resolveLocale(loc i18n.Locale) i18n.Locale {
	if loc == "" {
		return i18n.LocaleEn
	}
	return loc
}

// NewCommand creates `tanpopo support-bundle -o <archive>`.
func NewCommand(opts Options) *cobra.Command {
	loc := resolveLocale(opts.Locale)
	cmd := &cobra.Command{
		Use:   "support-bundle",
		Short: i18n.T(loc, "cli.support.bundle.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format := "table"
			if opts.OutputFormat != nil {
				format = strings.ToLower(strings.TrimSpace(*opts.OutputFormat))
			}
			if format != "json" && format != "yaml" && format != "table" {
				return errors.New(i18n.T(loc, "cli.err.output_format"))
			}
			if strings.TrimSpace(opts.RootDir) == "" {
				opts.RootDir = "."
			}
			if strings.TrimSpace(opts.Output) == "" {
				opts.Output = "tanpopo-support.tar.gz"
			}
			if err := CreateBundle(cmd.Context(), opts.RootDir, opts.Output); err != nil {
				return fmt.Errorf("%s: %w", i18n.T(loc, "cli.support.bundle.err.create"), err)
			}
			if opts.Stdout != nil {
				switch format {
				case "json":
					if err := json.NewEncoder(opts.Stdout).Encode(map[string]string{"archive": opts.Output}); err != nil {
						return fmt.Errorf("write support bundle result: %w", err)
					}
				case "yaml":
					if err := yaml.NewEncoder(opts.Stdout).Encode(map[string]string{"archive": opts.Output}); err != nil {
						return fmt.Errorf("write support bundle result: %w", err)
					}
				case "table", "":
					if _, err := fmt.Fprintln(opts.Stdout, opts.Output); err != nil {
						return fmt.Errorf("write support bundle result: %w", err)
					}
				default:
					return errors.New(i18n.T(loc, "cli.err.output_format"))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.Output, "archive", "o", opts.Output, i18n.T(loc, "cli.support.bundle.flag.archive"))
	cmd.Flags().StringVar(&opts.RootDir, "root", opts.RootDir, i18n.T(loc, "cli.support.bundle.flag.root"))
	return cmd
}

// CreateBundle captures a bounded allowlist of local config, lock, log, and
// metrics files. Every member is redacted and canary-scanned before archiving.
func CreateBundle(ctx context.Context, root, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if root == destination {
		return errors.New("output path cannot be the data root")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("output path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output path: %w", err)
	}
	redactor := privacy.NewDefaultRedactor()
	scanner := canary.NewScanner()
	doctorData, err := doctorReport(ctx, root)
	if err != nil {
		return fmt.Errorf("collect local doctor report: %w", err)
	}
	files := []inputFile{{name: "tanpopo.yaml"}, {name: "tanpopo.lock"}, {name: "metrics.prom"}}
	var logRoots []*os.Root
	defer func() {
		for _, r := range logRoots {
			_ = r.Close()
		}
	}()
	for _, dir := range []string{"logs", "log"} {
		logDir := filepath.Join(root, dir)
		dirInfo, dirErr := os.Lstat(logDir)
		if errors.Is(dirErr, os.ErrNotExist) {
			continue
		}
		if dirErr != nil {
			return fmt.Errorf("inspect %s directory: %w", dir, dirErr)
		}
		if !dirInfo.IsDir() {
			continue
		}
		dirRoot, openErr := os.OpenRoot(logDir)
		if openErr != nil {
			return fmt.Errorf("open %s directory: %w", dir, openErr)
		}
		logRoots = append(logRoots, dirRoot)
		openedDirInfo, statErr := dirRoot.Stat(".")
		pathDirInfo, pathErr := os.Lstat(logDir)
		if statErr != nil || pathErr != nil || !openedDirInfo.IsDir() || !os.SameFile(dirInfo, openedDirInfo) || !os.SameFile(openedDirInfo, pathDirInfo) {
			if statErr != nil {
				return fmt.Errorf("inspect opened %s directory: %w", dir, statErr)
			}
			if pathErr != nil {
				return fmt.Errorf("recheck %s directory: %w", dir, pathErr)
			}
			return fmt.Errorf("%s directory changed while opening; refusing to collect it", dir)
		}
		recent, collectErr := collectRecentLogs(dirRoot, dir)
		if collectErr != nil {
			return collectErr
		}
		files = append(files, recent...)
	}
	for _, file := range files {
		if destination == filepath.Join(root, file.name) {
			return errors.New("output path cannot replace a support input")
		}
	}
	var members []bundleMember
	var totalBytes int64
	if err := scanner.Check("doctor.json", doctorData); err != nil {
		return err
	}
	members = append(members, bundleMember{name: "doctor.json", data: doctorData})
	totalBytes += int64(len(doctorData))
	for _, input := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := input.name
		full := filepath.Join(root, name)
		var info fs.FileInfo
		var statErr error
		if input.root != nil {
			info, statErr = input.root.Lstat(input.relative)
		} else {
			info, statErr = os.Lstat(full)
		}
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return fmt.Errorf("inspect %s: %w", name, statErr)
		}
		if !info.Mode().IsRegular() || info.Size() > maxInputBytes {
			continue
		}
		if input.expected != nil && !os.SameFile(info, input.expected) {
			return fmt.Errorf("%s changed since log directory enumeration; refusing to collect it", name)
		}
		var f *os.File
		var readErr error
		if input.root != nil {
			f, readErr = openRootedInput(input.root, input.relative, input.expected)
		} else {
			f, readErr = os.Open(full)
		}
		if readErr != nil {
			return fmt.Errorf("read %s: %w", name, readErr)
		}
		openedInfo, statErr := f.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || (input.expected != nil && !os.SameFile(input.expected, openedInfo)) {
			_ = f.Close()
			if statErr != nil {
				return fmt.Errorf("inspect opened %s: %w", name, statErr)
			}
			return fmt.Errorf("%s changed while opening; refusing to collect it", name)
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
		closeErr := f.Close()
		if readErr != nil {
			return fmt.Errorf("read %s: %w", name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", name, closeErr)
		}
		if len(raw) > maxInputBytes {
			clear(raw)
			continue
		}
		clean := sanitize(raw, name, redactor)
		clear(raw)
		totalBytes += int64(len(clean))
		if totalBytes > maxBundleBytes {
			return errors.New("redacted diagnostic inputs exceed the support-bundle size limit")
		}
		if err := scanner.Check(name, clean); err != nil {
			return err
		}
		members = append(members, bundleMember{name: filepath.ToSlash(name), data: clean})
	}
	return writeArchive(destination, members, scanner)
}

func sanitize(raw []byte, name string, redactor *privacy.Redactor) []byte {
	if strings.HasSuffix(name, ".jsonl") {
		lines := bytes.Split(raw, []byte("\n"))
		var result bytes.Buffer
		for i, line := range lines {
			if len(line) == 0 && i == len(lines)-1 {
				continue
			}
			result.Write(sanitizeJSON(line, redactor))
			result.WriteByte('\n')
		}
		return result.Bytes()
	}
	var value any
	var err error
	switch {
	case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
		err = yaml.Unmarshal(raw, &value)
	default:
		err = json.Unmarshal(raw, &value)
	}
	if err == nil {
		clean, marshalErr := json.Marshal(redactValue(value, redactor))
		if marshalErr == nil {
			return clean
		}
	}
	return sanitizeText(string(raw), redactor)
}

func sanitizeJSON(raw []byte, redactor *privacy.Redactor) []byte {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return sanitizeText(string(raw), redactor)
	}
	clean, err := json.Marshal(redactValue(value, redactor))
	if err != nil {
		return sanitizeText(string(raw), redactor)
	}
	return clean
}

func sanitizeText(raw string, redactor *privacy.Redactor) []byte {
	clean := redactor.SanitizeString(raw)
	clean = masterKeyAssignment.ReplaceAllString(clean, `${1}[REDACTED]`)
	return []byte(clean)
}

func redactValue(value any, redactor *privacy.Redactor) any {
	switch item := value.(type) {
	case map[string]any:
		return redactor.SanitizeMap(item)
	case []any:
		out := make([]any, len(item))
		for i := range item {
			out[i] = redactValue(item[i], redactor)
		}
		return out
	default:
		clean, _ := redactor.SanitizeAttribute("value", item)
		return clean
	}
}

type bundleMember struct {
	name string
	data []byte
}

type inputFile struct {
	name     string
	expected fs.FileInfo
	root     *os.Root
	relative string
}

func collectRecentLogs(root *os.Root, directory string) ([]inputFile, error) {
	listing, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open rooted log listing: %w", err)
	}
	defer listing.Close()
	var candidates []inputFile
	scanned := 0
	for scanned < maxLogScanEntries {
		batchSize := 128
		if remain := maxLogScanEntries - scanned; batchSize > remain {
			batchSize = remain
		}
		entries, readErr := listing.ReadDir(batchSize)
		scanned += len(entries)
		for _, entry := range entries {
			if entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".log") || strings.HasSuffix(entry.Name(), ".jsonl")) {
				continue
			}
			info, statErr := root.Lstat(entry.Name())
			if statErr != nil {
				return nil, fmt.Errorf("inspect %s/%s: %w", directory, entry.Name(), statErr)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			candidates = append(candidates, inputFile{name: filepath.Join(directory, entry.Name()), expected: info, root: root, relative: entry.Name()})
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read %s directory: %w", directory, readErr)
		}
		if len(entries) == 0 {
			break
		}
	}
	if scanned == maxLogScanEntries {
		extra, probeErr := listing.ReadDir(1)
		if probeErr != nil && !errors.Is(probeErr, io.EOF) {
			return nil, fmt.Errorf("check %s directory scan limit: %w", directory, probeErr)
		}
		if len(extra) > 0 {
			return nil, fmt.Errorf("%s directory exceeds the bounded log scan limit", directory)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		ti, tj := candidates[i].expected.ModTime(), candidates[j].expected.ModTime()
		if ti.Equal(tj) {
			return candidates[i].name < candidates[j].name
		}
		return ti.After(tj)
	})
	if len(candidates) > maxLogFiles {
		candidates = candidates[:maxLogFiles]
	}
	return candidates, nil
}

func openRootedInput(root *os.Root, name string, expected fs.FileInfo) (*os.File, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || (expected != nil && !os.SameFile(expected, info)) {
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("rooted input changed; refusing to collect it")
	}
	return f, nil
}

func writeArchive(destination string, members []bundleMember, scanner *canary.Scanner) (err error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tmp, err := createArchiveTemp(filepath.Dir(destination))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = secureArchiveFile(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	gz := gzip.NewWriter(tmp)
	tarw := tar.NewWriter(gz)
	for _, member := range members {
		if err = scanner.Check(member.name, member.data); err != nil {
			break
		}
		hdr := &tar.Header{Name: member.name, Mode: 0o600, Size: int64(len(member.data)), Typeflag: tar.TypeReg}
		if err = tarw.WriteHeader(hdr); err != nil {
			break
		}
		if _, err = tarw.Write(member.data); err != nil {
			break
		}
	}
	closeTarErr := tarw.Close()
	closeGzipErr := gz.Close()
	if err == nil {
		err = closeTarErr
	}
	if err == nil {
		err = closeGzipErr
	}
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = verifyArchive(tmpName, scanner); err != nil {
		return err
	}
	if err := os.Link(tmpName, destination); err != nil {
		return fmt.Errorf("publish archive without replacing an existing path: %w", err)
	}
	return os.Remove(tmpName)
}

func verifyArchive(filename string, scanner *canary.Scanner) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	for {
		hdr, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		if hdr.Size < 0 || hdr.Size > maxInputBytes {
			return fmt.Errorf("archive member %q exceeds scan limit", hdr.Name)
		}
		data, readErr := io.ReadAll(io.LimitReader(tarReader, maxInputBytes+1))
		if readErr != nil {
			return readErr
		}
		if len(data) > maxInputBytes {
			return fmt.Errorf("archive member %q exceeds scan limit", hdr.Name)
		}
		if err := scanner.Check(hdr.Name, data); err != nil {
			return err
		}
		clear(data)
	}
}
