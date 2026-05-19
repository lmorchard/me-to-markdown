package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/envfile"
	"github.com/lmorchard/me-to-markdown/internal/registry"
	"github.com/lmorchard/me-to-markdown/internal/runner"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Walk through per-tool authorization, populating the shared env file",
	Long: `Run each registered tool's authorization flow in turn, capturing
secrets into the shared env file (see --env-file).

Per-tool behavior:
  mastodon / linkding / github   prompt for an access token (or other
                                 required credentials) and write the
                                 matching <TOOL>_<KEY>= entries to the
                                 env file.
  spotify                        exec the tool's own ` + "`auth`" + `
                                 subcommand (browser OAuth/PKCE flow).
                                 Requires SPOTIFY_CLIENT_ID set first.
  pocketcasts                    prompt for POCKETCASTS_EMAIL /
                                 POCKETCASTS_PASSWORD if not already
                                 present, then exec the tool's own
                                 ` + "`login`" + ` subcommand to cache a
                                 bearer token.

Use --tool <slug> to authorize a single tool. Without --tool, walks
every registered tool in registry order. Skip prompts for credentials
that are already present in the env / env-file.`,
	RunE: runAuth,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.Flags().String("tool", "", "authorize a single tool by slug (default: walk all)")
	authCmd.Flags().Bool("force", false, "re-prompt even for credentials that are already set")
}

func runAuth(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	toolSlug, _ := cmd.Flags().GetString("tool")
	force, _ := cmd.Flags().GetBool("force")

	tools, err := authTargets(toolSlug)
	if err != nil {
		return err
	}

	envFilePath := effectiveEnvFilePath()
	if envFilePath == "" {
		return errors.New("could not determine env file path; pass --env-file explicitly")
	}
	log.Infof("writing secrets to %s", envFilePath)

	in := bufio.NewReader(os.Stdin)
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	for _, t := range tools {
		fmt.Printf("\n=== %s ===\n", t.Label)
		handler, ok := authHandlers[t.Slug]
		if !ok {
			fmt.Printf("(no auth handler registered for %s; skipping)\n", t.Slug)
			continue
		}
		if err := handler(ctx, t, envFilePath, in, force); err != nil {
			log.Errorf("%s: %v", t.Slug, err)
			// Continue with the other tools rather than aborting the whole walk.
			continue
		}
	}
	fmt.Println()
	return nil
}

// effectiveEnvFilePath returns the path the auth command should write to.
// Priority: --env-file flag > env_file: config > default location.
func effectiveEnvFilePath() string {
	c := GetConfig()
	if c.EnvFile != "" {
		return c.EnvFile
	}
	return envfile.DefaultPath()
}

// authTargets resolves the requested tool slug(s).
func authTargets(toolSlug string) ([]registry.Tool, error) {
	if toolSlug == "" {
		return registry.Tools, nil
	}
	t, ok := registry.BySlug(toolSlug)
	if !ok {
		return nil, fmt.Errorf("unknown --tool slug %q (run `me-to-markdown list` for valid slugs)", toolSlug)
	}
	return []registry.Tool{t}, nil
}

// authHandler runs the per-tool auth flow. envFilePath is where any
// new secrets should be persisted. in is a buffered reader on stdin
// (so handlers don't fight over the same stream). force=true means
// re-prompt even when a value is already present.
type authHandler func(ctx context.Context, t registry.Tool, envFilePath string, in *bufio.Reader, force bool) error

var authHandlers = map[string]authHandler{
	"mastodon":    authMastodon,
	"linkding":    authLinkding,
	"github":      authGitHub,
	"spotify":     authSpotify,
	"pocketcasts": authPocketcasts,
}

// authMastodon prompts for the server URL + access token.
func authMastodon(_ context.Context, _ registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	fmt.Println("Generate a Mastodon access token under Settings → Development → New Application.")
	fmt.Println("Required scope: read:statuses")

	updates := map[string]string{}
	if v, err := promptKey(in, "MASTODON_SERVER", "Mastodon instance URL (e.g. https://mastodon.social): ", force, false); err != nil {
		return err
	} else if v != "" {
		updates["MASTODON_SERVER"] = v
	}
	if v, err := promptKey(in, "MASTODON_ACCESS_TOKEN", "Mastodon access token: ", force, true); err != nil {
		return err
	} else if v != "" {
		updates["MASTODON_ACCESS_TOKEN"] = v
	}
	return envfile.Upsert(envFilePath, updates)
}

// authLinkding prompts for the instance URL + API token.
func authLinkding(_ context.Context, _ registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	fmt.Println("Generate a Linkding API token under Settings → Integrations → REST API.")

	updates := map[string]string{}
	if v, err := promptKey(in, "LINKDING_URL", "Linkding instance URL (e.g. https://bookmarks.example.com): ", force, false); err != nil {
		return err
	} else if v != "" {
		updates["LINKDING_URL"] = v
	}
	if v, err := promptKey(in, "LINKDING_TOKEN", "Linkding API token: ", force, true); err != nil {
		return err
	} else if v != "" {
		updates["LINKDING_TOKEN"] = v
	}
	return envfile.Upsert(envFilePath, updates)
}

// authGitHub prompts for a personal access token.
func authGitHub(_ context.Context, _ registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	fmt.Println("Generate a GitHub fine-grained personal access token at:")
	fmt.Println("  https://github.com/settings/personal-access-tokens/new")
	fmt.Println("Required scopes: read:user (public activity is fine for the default flow)")

	updates := map[string]string{}
	if v, err := promptKey(in, "GITHUB_TOKEN", "GitHub access token: ", force, true); err != nil {
		return err
	} else if v != "" {
		updates["GITHUB_TOKEN"] = v
	}
	return envfile.Upsert(envFilePath, updates)
}

// authSpotify ensures SPOTIFY_CLIENT_ID is set, then shells into
// `spotify-to-markdown auth` for the browser OAuth flow.
func authSpotify(ctx context.Context, t registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	fmt.Println("Create (or open) a Spotify Developer app at:")
	fmt.Println("  https://developer.spotify.com/dashboard")
	fmt.Println("Note the Client ID. (No client secret needed — PKCE flow.)")
	fmt.Println("Register http://127.0.0.1:8888/callback as a Redirect URI.")

	if v, err := promptKey(in, "SPOTIFY_CLIENT_ID", "Spotify Client ID: ", force, false); err != nil {
		return err
	} else if v != "" {
		if err := envfile.Upsert(envFilePath, map[string]string{"SPOTIFY_CLIENT_ID": v}); err != nil {
			return err
		}
	}

	binPath, _, err := runner.Resolve(t.Binary)
	if err != nil {
		return err
	}

	fmt.Println("\nLaunching browser OAuth flow…")
	c := exec.CommandContext(ctx, binPath, "auth")
	c.Env = append(os.Environ(), reloadEnv(envFilePath)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// authPocketcasts prompts for email + password, then shells into
// `pocketcasts-to-markdown login --email <> --password-stdin`.
func authPocketcasts(ctx context.Context, t registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	email, err := promptKey(in, "POCKETCASTS_EMAIL", "Pocket Casts email: ", force, false)
	if err != nil {
		return err
	}
	if email == "" {
		// already set in env; reload current value
		email = os.Getenv("POCKETCASTS_EMAIL")
		if email == "" {
			for _, e := range reloadEnv(envFilePath) {
				if strings.HasPrefix(e, "POCKETCASTS_EMAIL=") {
					email = strings.TrimPrefix(e, "POCKETCASTS_EMAIL=")
					break
				}
			}
		}
	} else {
		if err := envfile.Upsert(envFilePath, map[string]string{"POCKETCASTS_EMAIL": email}); err != nil {
			return err
		}
	}

	password, err := promptKey(in, "POCKETCASTS_PASSWORD", "Pocket Casts password: ", force, true)
	if err != nil {
		return err
	}
	if password != "" {
		if err := envfile.Upsert(envFilePath, map[string]string{"POCKETCASTS_PASSWORD": password}); err != nil {
			return err
		}
	} else {
		// Already-set case: use the existing value just to feed login.
		for _, e := range reloadEnv(envFilePath) {
			if strings.HasPrefix(e, "POCKETCASTS_PASSWORD=") {
				password = strings.TrimPrefix(e, "POCKETCASTS_PASSWORD=")
				break
			}
		}
		if password == "" {
			password = os.Getenv("POCKETCASTS_PASSWORD")
		}
	}
	if email == "" || password == "" {
		return errors.New("missing POCKETCASTS_EMAIL or POCKETCASTS_PASSWORD")
	}

	binPath, _, err := runner.Resolve(t.Binary)
	if err != nil {
		return err
	}

	fmt.Println("Exchanging credentials for a Pocket Casts bearer token…")
	c := exec.CommandContext(ctx, binPath, "login", "--email", email, "--password-stdin")
	c.Env = append(os.Environ(), reloadEnv(envFilePath)...)
	c.Stdin = strings.NewReader(password + "\n")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// promptKey asks for the given env-var value. If the variable is
// already present in the process env or in the env file, returns
// "" (skip) unless force=true. secret=true reads with echo off.
func promptKey(in *bufio.Reader, key, prompt string, force, secret bool) (string, error) {
	if !force {
		if v := os.Getenv(key); v != "" {
			fmt.Printf("%s is already set in the environment; skipping. (Use --force to re-prompt.)\n", key)
			return "", nil
		}
		// Also check env-file value (loaded at startup).
		for _, e := range envFileExtra {
			if strings.HasPrefix(e, key+"=") && len(e) > len(key)+1 {
				fmt.Printf("%s is already set in the env file; skipping. (Use --force to re-prompt.)\n", key)
				return "", nil
			}
		}
	}
	if secret && term.IsTerminal(int(os.Stdin.Fd())) {
		return readSecret(prompt)
	}
	// Non-TTY stdin (scripted input): fall back to plain line-read. The
	// secret will be visible in the terminal output, but this only
	// applies when input is piped — interactive runs still get no-echo.
	return readLine(in, prompt)
}

func readLine(in *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := in.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func readSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// reloadEnv re-reads the env file from disk and returns its entries.
// Used by handlers that need the live state of the file (after their
// own writes) when launching a subprocess.
func reloadEnv(path string) []string {
	entries, err := envfile.Load(path)
	if err != nil {
		return envFileExtra
	}
	return entries
}
