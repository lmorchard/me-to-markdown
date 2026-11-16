package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/lmorchard/me-to-markdown/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// SourceExportData represents the fetched items for a source ready for formatting.
type SourceExportData struct {
	Source sources.Source
	Items  []model.Item
	Err    error
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export personal data exhaust items from local SQLite storage",
	Long: `Export personal data exhaust items covering a time window. Output can be formatted
as Markdown (default, perfect for weeknotes), JSON, or JSONL.

Output can be written to a single combined file/stdout (-o) or split into per-source
files in a directory (-d).`,
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().String("since", "", "Start of time window (YYYY-MM-DD or Go duration like 168h) — required")
	exportCmd.Flags().String("until", "", "End of time window (YYYY-MM-DD, defaults to now)")
	exportCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	exportCmd.Flags().StringP("output-dir", "d", "", "Write one file per source into this directory instead of concatenating")
	exportCmd.Flags().String("format", "markdown", "Output format: markdown, json, jsonl")
	exportCmd.Flags().Bool("sync", false, "Trigger sync before exporting")
	exportCmd.Flags().StringSlice("include", nil, "Export only these sources (comma-separated slugs)")
	exportCmd.Flags().StringSlice("exclude", nil, "Skip these sources (comma-separated slugs)")
	exportCmd.Flags().Bool("omit-errors", false, "Suppress per-source error sections")

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
	outputDir, _ := cmd.Flags().GetString("output-dir")
	format, _ := cmd.Flags().GetString("format")
	shouldSync, _ := cmd.Flags().GetBool("sync")

	if output != "" && outputDir != "" {
		return errors.New("--output and --output-dir are mutually exclusive")
	}
	if since == "" {
		return errors.New("--since is required (or set `since:` in config)")
	}

	tw, err := model.ParseTimeWindow(since, until, time.Now())
	if err != nil {
		return fmt.Errorf("invalid time window: %w", err)
	}

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

	selected := filterSources(include, exclude)
	if len(selected) == 0 {
		return errors.New("no sources selected")
	}

	st, err := GetStore()
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	if shouldSync {
		log.Infof("Running pre-export sync for %d source(s)...", len(selected))
		env := GetEnvMap()
		opts := sources.SyncOptions{Since: tw.Since}
		var wg sync.WaitGroup
		for _, s := range selected {
			wg.Add(1)
			go func(src sources.Source) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
				defer cancel()
				if _, sErr := src.Sync(ctx, st, env, opts); sErr != nil {
					log.Warnf("sync %s: %v", src.Name(), sErr)
				}
			}(s)
		}
		wg.Wait()
	}

	data := make([]SourceExportData, len(selected))
	for i, src := range selected {
		items, qErr := st.GetItems(cmd.Context(), store.ItemFilter{
			Sources: []string{src.Name()},
			Since:   tw.Since,
			Until:   tw.Until,
		})
		data[i] = SourceExportData{Source: src, Items: items, Err: qErr}
	}

	// Format output
	switch strings.ToLower(format) {
	case "json":
		return exportJSON(output, outputDir, data)
	case "jsonl":
		return exportJSONL(output, outputDir, data)
	case "markdown", "md":
		return exportMarkdown(output, outputDir, data, c.OmitErrors)
	default:
		return fmt.Errorf("unknown format %q (supported: markdown, json, jsonl)", format)
	}
}

func exportMarkdown(output, outputDir string, data []SourceExportData, omitErrors bool) error {
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		for _, d := range data {
			filePath := filepath.Join(outputDir, d.Source.Name()+".md")
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "# %s\n\n", d.Source.Label())
			if d.Err != nil && !omitErrors {
				fmt.Fprintf(&buf, "Error querying items: %v\n\n", d.Err)
			} else {
				_ = d.Source.RenderMarkdown(&buf, d.Items)
			}
			if err := os.WriteFile(filePath, buf.Bytes(), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", filePath, err)
			}
		}
		return nil
	}

	var combined bytes.Buffer
	for _, d := range data {
		fmt.Fprintf(&combined, "## %s\n\n", d.Source.Label())
		if d.Err != nil {
			if !omitErrors {
				fmt.Fprintf(&combined, "Error querying items: %v\n\n", d.Err)
			}
		} else if len(d.Items) == 0 {
			fmt.Fprintf(&combined, "_No items recorded for this time window._\n\n")
		} else {
			_ = d.Source.RenderMarkdown(&combined, d.Items)
		}
	}

	return writeOutput(output, combined.Bytes())
}

func exportJSON(output, outputDir string, data []SourceExportData) error {
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		for _, d := range data {
			filePath := filepath.Join(outputDir, d.Source.Name()+".json")
			b, err := json.MarshalIndent(d.Items, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(filePath, b, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", filePath, err)
			}
		}
		return nil
	}

	all := make(map[string][]model.Item)
	for _, d := range data {
		all[d.Source.Name()] = d.Items
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeOutput(output, b)
}

func exportJSONL(output, outputDir string, data []SourceExportData) error {
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		for _, d := range data {
			filePath := filepath.Join(outputDir, d.Source.Name()+".jsonl")
			f, err := os.Create(filePath)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(f)
			for _, it := range d.Items {
				_ = enc.Encode(it)
			}
			_ = f.Close()
		}
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, d := range data {
		for _, it := range d.Items {
			_ = enc.Encode(it)
		}
	}
	return writeOutput(output, buf.Bytes())
}

func writeOutput(output string, content []byte) error {
	if output == "" || output == "-" {
		_, err := os.Stdout.Write(content)
		return err
	}
	dir := filepath.Dir(output)
	if dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return os.WriteFile(output, content, 0o644)
}
