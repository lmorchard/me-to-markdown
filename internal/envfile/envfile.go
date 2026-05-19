// Package envfile parses a simple KEY=VALUE file into entries suitable
// for appending to exec.Cmd.Env. Format intentionally dumb: one entry
// per line, blank lines and `#` comments ignored, value is everything
// after the first `=` verbatim (no quoting, no shell interpolation, no
// `export` prefix support).
package envfile

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Load reads path and returns its parsed entries as a []string of
// KEY=VALUE pairs. An empty path returns nil with no error (the env
// file is optional). A missing path also returns nil — the file's
// non-existence is not an error since the default path is opportunistic.
// Any other error (permission, malformed line) is returned.
func Load(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var entries []string
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE, got %q", path, lineNo, line)
		}
		key := strings.TrimSpace(line[:eq])
		value := line[eq+1:]
		if key == "" {
			return nil, fmt.Errorf("%s:%d: empty key in %q", path, lineNo, line)
		}
		entries = append(entries, key+"="+value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return entries, nil
}

// DefaultPath returns the conventional location for the orchestrator's
// shared env file: $XDG_CONFIG_HOME/me-to-markdown/env (fallback
// ~/.config/me-to-markdown/env). The path is returned whether or not
// the file exists; the caller decides what to do if it's missing.
func DefaultPath() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "me-to-markdown", "env")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "me-to-markdown", "env")
}

// Upsert merges updates into the env file at path, rewriting the file
// in place. Existing entries whose key matches an update are replaced
// (in their original position); new keys are appended. Comments and
// blank lines in the source are preserved. The file (and parent dir)
// is created with 0600 / 0700 perms if it doesn't exist.
func Upsert(path string, updates map[string]string) error {
	if path == "" {
		return errors.New("envfile.Upsert: empty path")
	}
	if len(updates) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}

	var original []byte
	if existing, err := os.ReadFile(path); err == nil {
		original = existing
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	applied := make(map[string]bool, len(updates))
	var out strings.Builder

	for _, line := range strings.SplitAfter(string(original), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out.WriteString(line)
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			out.WriteString(line)
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if newValue, ok := updates[key]; ok && !applied[key] {
			out.WriteString(key + "=" + newValue + "\n")
			applied[key] = true
		} else {
			out.WriteString(line)
		}
	}

	// Ensure trailing newline before appending new keys.
	current := out.String()
	if len(current) > 0 && !strings.HasSuffix(current, "\n") {
		out.WriteString("\n")
	}

	// Append keys that weren't already in the file (in stable key order).
	keys := make([]string, 0, len(updates))
	for k := range updates {
		if !applied[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.WriteString(k + "=" + updates[k] + "\n")
	}

	if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
