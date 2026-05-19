package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/registry"
	"github.com/lmorchard/me-to-markdown/internal/runner"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Show registered tools and where each binary resolves",
	Long: `Iterate the registered *-to-markdown tools and report, for each:
  - slug used in --include / --exclude
  - whether the binary was found on $PATH or in the managed directory
    ($XDG_DATA_HOME/me-to-markdown/bin), or is missing
  - the binary's reported version (when found)`,
	RunE: runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SLUG\tSTATUS\tVERSION\tPATH")
	for _, t := range registry.Tools {
		path, source, err := runner.Resolve(t.Binary)
		status := "missing"
		version := "—"
		shownPath := "—"
		switch {
		case err != nil:
			// keep defaults
		case source == runner.SourcePath:
			status = "found ($PATH)"
			version = probeVersion(cmd.Context(), path)
			shownPath = path
		case source == runner.SourceManaged:
			status = "found (managed)"
			version = probeVersion(cmd.Context(), path)
			shownPath = path
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Slug, status, version, shownPath)
	}
	return w.Flush()
}

// probeVersion runs `<bin> version` with a short timeout and returns the
// first non-empty line of stdout. Returns "?" on failure rather than
// surfacing the error — `list` shouldn't fail because one tool's version
// check timed out.
func probeVersion(parent context.Context, binPath string) string {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "version").Output()
	if err != nil {
		return "?"
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "?"
}
