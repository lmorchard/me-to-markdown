package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Legacy command (no longer needed — all sources are built into this binary)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("All 6 sources (Mastodon, Linkding, GitHub, Spotify, YouTube, Pocket Casts) are now built directly into this binary.")
		fmt.Println("No external tool installation is necessary. Run `me-to-markdown list` or `me-to-markdown sync`.")
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
