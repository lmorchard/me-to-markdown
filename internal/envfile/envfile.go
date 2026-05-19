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
