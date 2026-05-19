package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, commit hash, and build date of this application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s %s (commit: %s, built: %s)\n", appName, version, commit, date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
