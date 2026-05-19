// Package runner locates and executes the *-to-markdown tool binaries the
// orchestrator coordinates.
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Source describes where a resolved binary was found.
type Source string

const (
	// SourcePath means the binary was found on the user's $PATH.
	SourcePath Source = "path"
	// SourceManaged means the binary was found in the orchestrator's
	// managed directory ($XDG_DATA_HOME/me-to-markdown/bin).
	SourceManaged Source = "managed"
)

// ManagedBinDir returns $XDG_DATA_HOME/me-to-markdown/bin, falling back
// to ~/.local/share/me-to-markdown/bin if XDG_DATA_HOME is unset.
func ManagedBinDir() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "me-to-markdown", "bin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "share", "me-to-markdown", "bin")
	}
	return filepath.Join(home, ".local", "share", "me-to-markdown", "bin")
}

// Resolve locates the binary by name. $PATH is checked first; failing that,
// the managed directory is consulted. Returns the absolute path and a Source
// tag identifying which lookup succeeded. NotFound is returned as a regular
// error with a message pointing the user at `me-to-markdown install`.
func Resolve(binary string) (path string, source Source, err error) {
	if p, lookErr := exec.LookPath(binary); lookErr == nil {
		abs, _ := filepath.Abs(p)
		return abs, SourcePath, nil
	}

	managed := filepath.Join(ManagedBinDir(), binary)
	if info, statErr := os.Stat(managed); statErr == nil && !info.IsDir() {
		return managed, SourceManaged, nil
	}

	return "", "", fmt.Errorf("%s not found on $PATH or in %s — run `me-to-markdown install` to download release binaries", binary, ManagedBinDir())
}
