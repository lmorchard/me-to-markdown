package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/registry"
	"github.com/lmorchard/me-to-markdown/internal/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// defaultToolTimeout caps how long any one tool subprocess may run. Generous
// by default; configurable later if the need arises.
const defaultToolTimeout = 5 * time.Minute

// toolResult holds one tool subprocess's captured output and run error.
type toolResult struct {
	stdout []byte
	stderr []byte
	err    error
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Run every registered *-to-markdown tool in parallel and concatenate the output",
	Long: `Invoke every selected *-to-markdown tool's ` + "`export`" + ` subcommand in parallel
over a single time window, then concatenate the output into one Markdown
document with section headers per tool.

Tools that fail render an error section in place of their output (unless
--omit-errors is set). The process exit code reflects whether any tool
failed.

Example usage:
  me-to-markdown export --since 168h -o weeknotes.md
  me-to-markdown export --since 2026-05-11 --until 2026-05-18 --include mastodon,github`,
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().String("since", "", "Start of time window (YYYY-MM-DD or Go duration like 168h) — required unless set in config")
	exportCmd.Flags().String("until", "", "End of time window (YYYY-MM-DD, defaults to now)")
	exportCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	exportCmd.Flags().StringSlice("include", nil, "Run only these tools (comma-separated slugs). Mutually exclusive with --exclude.")
	exportCmd.Flags().StringSlice("exclude", nil, "Skip these tools (comma-separated slugs). Mutually exclusive with --include.")
	exportCmd.Flags().Bool("omit-errors", false, "Suppress per-tool error sections in the combined output")

	_ = viper.BindPFlag("since", exportCmd.Flags().Lookup("since"))
	_ = viper.BindPFlag("include", exportCmd.Flags().Lookup("include"))
	_ = viper.BindPFlag("exclude", exportCmd.Flags().Lookup("exclude"))
	_ = viper.BindPFlag("omit_errors", exportCmd.Flags().Lookup("omit-errors"))
}

func runExport(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	c := GetConfig()

	since := viper.GetString("since")
	until, _ := cmd.Flags().GetString("until")
	output, _ := cmd.Flags().GetString("output")

	if since == "" {
		return errors.New("--since is required (or set `since:` in config)")
	}

	// Resolve include/exclude with mutual-exclusion check.
	flagsIncludeSet := cmd.Flags().Changed("include")
	flagsExcludeSet := cmd.Flags().Changed("exclude")
	if flagsIncludeSet && flagsExcludeSet {
		return errors.New("--include and --exclude are mutually exclusive")
	}
	include := c.Include
	exclude := c.Exclude
	if flagsIncludeSet {
		include, _ = cmd.Flags().GetStringSlice("include")
		exclude = nil
	} else if flagsExcludeSet {
		exclude, _ = cmd.Flags().GetStringSlice("exclude")
		include = nil
	}
	if len(include) > 0 && len(exclude) > 0 {
		return errors.New("config has both `include:` and `exclude:` — pick one")
	}

	selected, err := selectTools(include, exclude)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return errors.New("no tools selected")
	}
	log.Infof("Window: --since %s%s", since, formatUntil(until))
	log.Infof("Selected %d tool(s): %s", len(selected), toolNames(selected))

	// Each tool's section ends up at the same index as its entry in selected.
	results := make([]toolResult, len(selected))
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	overallStart := time.Now()
	for i, t := range selected {
		wg.Add(1)
		go func(i int, t registry.Tool) {
			defer wg.Done()
			results[i] = runTool(ctx, log, t, since, until)
		}(i, t)
	}
	wg.Wait()
	log.Infof("All %d tool(s) finished in %s", len(selected), time.Since(overallStart).Round(time.Millisecond))

	// Build the combined output. Each section is prefixed with `## {Label}`
	// — if the tool itself emits H1/H2 headers, they nest underneath ours,
	// which is the desired shape for a combined weeknotes document.
	var combined bytes.Buffer
	anyFailed := false
	omitErrors := c.OmitErrors

	for i, t := range selected {
		r := results[i]
		if r.err != nil {
			anyFailed = true
			log.Errorf("%s export failed: %v", t.Binary, r.err)
		}
		section, skip := renderToolSection(t, r, "##", omitErrors)
		if skip {
			continue
		}
		combined.Write(section)
	}

	if err := writeOutput(output, combined.Bytes()); err != nil {
		return err
	}
	if output != "" && output != "-" {
		log.Infof("Wrote %s combined output to %s", humanBytes(combined.Len()), output)
	} else {
		log.Infof("Wrote %s combined output to stdout", humanBytes(combined.Len()))
	}

	if anyFailed {
		return fmt.Errorf("one or more tools failed (see error sections above and stderr)")
	}
	return nil
}

// renderToolSection renders one tool's contribution to the output: a header
// at the given level (e.g. "#" or "##") followed by the tool's stdout, or an
// error block if the run failed. The second return value reports whether the
// section should be skipped entirely (a failed tool while omitErrors is set).
func renderToolSection(t registry.Tool, r toolResult, header string, omitErrors bool) ([]byte, bool) {
	var buf bytes.Buffer
	if r.err != nil {
		if omitErrors {
			return nil, true
		}
		fmt.Fprintf(&buf, "%s %s\n\n> Error: %s export failed: %s\n\n",
			header, t.Label, t.Binary, errSummary(r.err, r.stderr))
		return buf.Bytes(), false
	}
	fmt.Fprintf(&buf, "%s %s\n\n", header, t.Label)
	if len(r.stdout) > 0 {
		buf.Write(r.stdout)
		if !bytes.HasSuffix(r.stdout, []byte("\n")) {
			buf.WriteByte('\n')
		}
	}
	buf.WriteByte('\n')
	return buf.Bytes(), false
}

// formatUntil formats the `--until` flag for the window log line; returns
// empty string if no until was provided.
func formatUntil(until string) string {
	if until == "" {
		return ""
	}
	return " --until " + until
}

// humanBytes renders byte counts as a short string (e.g. "12.3KB").
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

// runTool invokes a single tool's `export` subcommand with the supplied
// time window flags, capturing stdout and stderr. Per-tool timeout is
// enforced via context.
func runTool(parentCtx context.Context, log loggerLike, t registry.Tool, since, until string) (out toolResult) {
	binPath, source, err := runner.Resolve(t.Binary)
	if err != nil {
		out.err = err
		log.Infof("%s: skipped (%v)", t.Slug, err)
		return
	}
	log.Infof("%s: starting (%s via %s)", t.Slug, binPath, source)

	ctx, cancel := context.WithTimeout(parentCtx, defaultToolTimeout)
	defer cancel()

	args := []string{"export", "--since", since}
	if until != "" {
		args = append(args, "--until", until)
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = append(os.Environ(), envFileExtra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond)
	out.stdout = stdout.Bytes()
	out.stderr = stderr.Bytes()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out.err = fmt.Errorf("timed out after %s", defaultToolTimeout)
		} else {
			out.err = err
		}
		log.Infof("%s: failed in %s", t.Slug, elapsed)
		return
	}
	log.Infof("%s: completed in %s (%s)", t.Slug, elapsed, humanBytes(len(out.stdout)))
	return
}

// selectTools returns the registered tools filtered by include/exclude.
// If include is non-empty, only those slugs are kept (in registry order).
// Otherwise all tools are kept except those in exclude.
// Unknown slugs return an error.
func selectTools(include, exclude []string) ([]registry.Tool, error) {
	include = normalizeSlugs(include)
	exclude = normalizeSlugs(exclude)

	known := func(slug string) bool {
		_, ok := registry.BySlug(slug)
		return ok
	}
	for _, s := range include {
		if !known(s) {
			return nil, fmt.Errorf("unknown --include slug %q (run `me-to-markdown list` for valid slugs)", s)
		}
	}
	for _, s := range exclude {
		if !known(s) {
			return nil, fmt.Errorf("unknown --exclude slug %q", s)
		}
	}

	if len(include) > 0 {
		set := stringSet(include)
		out := make([]registry.Tool, 0, len(include))
		for _, t := range registry.Tools {
			if set[t.Slug] {
				out = append(out, t)
			}
		}
		return out, nil
	}

	set := stringSet(exclude)
	out := make([]registry.Tool, 0, len(registry.Tools))
	for _, t := range registry.Tools {
		if !set[t.Slug] {
			out = append(out, t)
		}
	}
	return out, nil
}

func normalizeSlugs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stringSet(in []string) map[string]bool {
	m := make(map[string]bool, len(in))
	for _, s := range in {
		m[s] = true
	}
	return m
}

func toolNames(tools []registry.Tool) string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Slug
	}
	return strings.Join(names, ", ")
}

func writeOutput(path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// errSummary returns a short single-line description for inclusion in the
// per-tool error section. The first non-empty line of stderr is preferred
// over the raw exec error, since it tends to be more informative.
func errSummary(err error, stderr []byte) string {
	for _, line := range bytes.Split(stderr, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			return string(trimmed)
		}
	}
	return err.Error()
}

// loggerLike is the subset of *logrus.Logger we use. Keeps test mocks
// trivial if we ever want them.
type loggerLike interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}
