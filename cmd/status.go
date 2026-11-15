package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status, cache counts, and sync history for all sources",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	st, err := GetStore()
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	stats, err := st.GetStats(cmd.Context())
	if err != nil {
		return fmt.Errorf("get store stats: %w", err)
	}

	fmt.Println("Personal Data Exhaust Cache Status:")
	fmt.Printf("%-14s %-8s %-20s %-20s %-10s\n", "Source", "Items", "Latest Item", "Last Synced", "Status")
	fmt.Println("--------------------------------------------------------------------------------")

	totalItems := 0
	for _, ss := range stats {
		totalItems += ss.ItemCount
		latestStr := "never"
		if ss.LatestItem != nil {
			latestStr = ss.LatestItem.Local().Format("2006-01-02 15:04")
		}
		syncedStr := "never"
		if ss.LastSyncedAt != nil {
			syncedStr = ss.LastSyncedAt.Local().Format("2006-01-02 15:04")
		}
		statusStr := ss.Status
		if statusStr == "" {
			statusStr = "uninitialized"
		}

		fmt.Printf("%-14s %-8d %-20s %-20s %-10s\n", ss.Source, ss.ItemCount, latestStr, syncedStr, statusStr)
		if ss.LastError != "" {
			fmt.Printf("   ↳ error: %s\n", ss.LastError)
		}
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("Total cached items: %d\n", totalItems)
	return nil
}
