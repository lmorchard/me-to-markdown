package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/me-to-markdown/internal/envfile"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold configuration and environment file",
	Long: `Initialize configuration file (me-to-markdown.yaml) and environment file
(~/.config/me-to-markdown/env) with sample credentials.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Overwrite existing config/env files")
}

const defaultEnvTemplate = `# Credentials for personal data exhaust sources

# Mastodon
MASTODON_SERVER=https://mastodon.social
MASTODON_ACCESS_TOKEN=

# Linkding
LINKDING_URL=https://bookmarks.example.com
LINKDING_TOKEN=

# GitHub
GITHUB_TOKEN=

# Spotify
SPOTIFY_CLIENT_ID=

# YouTube
YOUTUBE_CLIENT_ID=
YOUTUBE_CLIENT_SECRET=

# Pocket Casts
POCKETCASTS_EMAIL=
POCKETCASTS_PASSWORD=
`

const defaultConfigTemplate = `# me-to-markdown configuration

# Default time window for export/sync when not specified
# since: "168h"

# Pre-select sources to include or exclude
# include:
#   - mastodon
#   - linkding
#   - github
#   - spotify
#   - youtube
#   - pocketcasts

# Path to SQLite database
# db: "~/.local/share/me-to-markdown/exhaust.db"
`

func runInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	// 1. Config file
	cfgPath := "me-to-markdown.yaml"
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) || force {
		if err := os.WriteFile(cfgPath, []byte(defaultConfigTemplate), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", cfgPath, err)
		}
		fmt.Printf("✓ Created %s\n", cfgPath)
	} else {
		fmt.Printf("• %s already exists (use --force to overwrite)\n", cfgPath)
	}

	// 2. Env file
	envPath := envfile.DefaultPath()
	if _, err := os.Stat(envPath); os.IsNotExist(err) || force {
		_ = os.MkdirAll(filepath.Dir(envPath), 0o755)
		if err := os.WriteFile(envPath, []byte(defaultEnvTemplate), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", envPath, err)
		}
		fmt.Printf("✓ Created %s\n", envPath)
	} else {
		fmt.Printf("• %s already exists\n", envPath)
	}

	fmt.Println("\nNext steps:")
	fmt.Printf("1. Edit %s to fill in your API tokens\n", envPath)
	fmt.Println("2. Run `me-to-markdown list` to verify source authentication")
	fmt.Println("3. Run `me-to-markdown sync` to populate your local database")
	return nil
}
