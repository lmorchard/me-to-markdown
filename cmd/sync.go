package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch and cache data from personal data exhaust sources into local SQLite storage",
	Long: `Sync fetches new items from configured sources (Mastodon, Linkding, GitHub,
Spotify, YouTube, Pocket Casts) and saves them to the local SQLite database.

Sync is incremental by default, resuming from the last known cursor or timestamp.`,
	RunE: runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringSlice("include", nil, "Sync only these sources (comma-separated slugs)")
	syncCmd.Flags().StringSlice("exclude", nil, "Skip these sources (comma-separated slugs)")
	syncCmd.Flags().String("since", "", "Backfill since this time (YYYY-MM-DD or Go duration like 168h)")
	syncCmd.Flags().Bool("full-reindex", false, "Ignore cached cursor and re-fetch entire window")
}

func runSync(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	st, err := GetStore()
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	include, _ := cmd.Flags().GetStringSlice("include")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")
	sinceStr, _ := cmd.Flags().GetString("since")
	fullReindex, _ := cmd.Flags().GetBool("full-reindex")

	var sinceTime time.Time
	if sinceStr != "" {
		parsed, err := model.ParseTime(sinceStr, time.Now(), false)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		sinceTime = parsed
	}

	targets := filterSources(include, exclude)
	if len(targets) == 0 {
		return fmt.Errorf("no sources selected for sync")
	}

	env := GetEnvMap()
	opts := sources.SyncOptions{
		Since:       sinceTime,
		FullReindex: fullReindex,
	}

	log.Infof("Starting sync for %d source(s)...", len(targets))

	var wg sync.WaitGroup
	type resHolder struct {
		src sources.Source
		res *sources.SyncResult
		err error
	}
	results := make([]resHolder, len(targets))

	start := time.Now()
	for i, src := range targets {
		wg.Add(1)
		go func(i int, s sources.Source) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()

			res, err := s.Sync(ctx, st, env, opts)
			results[i] = resHolder{src: s, res: res, err: err}
		}(i, src)
	}
	wg.Wait()

	fmt.Printf("\nSync completed in %s:\n", time.Since(start).Round(time.Millisecond))
	var anyFailed bool
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("  ❌ %-12s failed: %v\n", r.src.Label(), r.err)
			anyFailed = true
		} else {
			fmt.Printf("  ✓  %-12s fetched: %-4d upserted: %-4d\n", r.src.Label(), r.res.Fetched, r.res.Upserted)
		}
	}

	if anyFailed {
		return fmt.Errorf("one or more sources failed to sync")
	}
	return nil
}

func filterSources(include, exclude []string) []sources.Source {
	all := sources.All()
	if len(include) == 0 && len(exclude) == 0 {
		return all
	}

	incMap := make(map[string]bool)
	for _, s := range include {
		incMap[s] = true
	}
	excMap := make(map[string]bool)
	for _, s := range exclude {
		excMap[s] = true
	}

	var filtered []sources.Source
	for _, s := range all {
		if len(incMap) > 0 && !incMap[s.Name()] {
			continue
		}
		if excMap[s.Name()] {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}
