package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/registry"
	"github.com/lmorchard/me-to-markdown/internal/runner"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold per-tool config files by delegating to each tool's init",
	Long: `Run each registered tool's `+"`init`"+` subcommand in the current
directory, scaffolding a config (and any starter template) for each.

Then print the manual next steps for tools that need interactive setup
(Spotify OAuth, Pocket Casts password login, and adding tokens to the
config files for Mastodon / Linkding / GitHub).

This subcommand does not attempt to automate the interactive auth flows —
they need a human at a browser or password prompt.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "pass --force to each tool's init (overwrite existing files)")
}

func runInit(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	force, _ := cmd.Flags().GetBool("force")

	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()

	for _, t := range registry.Tools {
		path, _, err := runner.Resolve(t.Binary)
		if err != nil {
			log.Warnf("%s: %v (skipping; run `me-to-markdown install` to fetch binaries)", t.Slug, err)
			continue
		}
		cmdArgs := []string{"init"}
		if force {
			cmdArgs = append(cmdArgs, "--force")
		}
		c := exec.CommandContext(ctx, path, cmdArgs...)
		c.Env = append(os.Environ(), envFileExtra...)
		var stdout, stderr bytes.Buffer
		c.Stdout = &stdout
		c.Stderr = &stderr
		if err := c.Run(); err != nil {
			log.Errorf("%s init failed: %v", t.Slug, errSummary(err, stderr.Bytes()))
			continue
		}
		log.Infof("%s: init complete", t.Slug)
		// Forward any "next steps" output from the tool itself.
		if out := strings.TrimSpace(stdout.String()); out != "" {
			fmt.Println(out)
		}
	}

	fmt.Fprint(os.Stdout, "\n"+nextStepsBlock())
	return nil
}

// nextStepsBlock summarizes the manual setup each tool needs after `init`
// has scaffolded its config file. Hard-coded here rather than computed
// from the registry because the per-tool auth shape varies (OAuth flow,
// password prompt, API token paste).
func nextStepsBlock() string {
	return `Next steps for interactive setup:

  Mastodon / Linkding / GitHub
    Edit each *.yaml config in this directory and paste in your API token.
    See each tool's README for token creation instructions.

  Spotify
    Run:  spotify-to-markdown auth
    This opens a browser for the OAuth/PKCE flow once. Refresh tokens
    persist; no further interactive auth is needed.

  Pocket Casts
    Run:  echo "$YOUR_PASSWORD" | pocketcasts-to-markdown login --email you@example.com --password-stdin
    Caches a bearer token; re-run only when the token expires.

After that, validate the setup with:
    me-to-markdown list
    me-to-markdown export --since 168h
`
}
