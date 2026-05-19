package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

// maxAuthAttempts caps how many times runAuthForTool will loop on a
// failing validation before giving up and moving on.
const maxAuthAttempts = 3

// validateTimeout caps how long the orchestrator waits for a per-tool
// credential check (`<tool> export --since 1m -o /dev/null`).
const validateTimeout = 60 * time.Second

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Validate credentials per tool; prompt only for what's missing or broken",
	Long: `Walk each registered tool and ensure its credentials are present and
working. Idempotent: re-running on an already-configured family is a
fast no-op.

For each tool, the orchestrator first runs a lightweight credential
check (` + "`<tool> export --since 1m -o /dev/null`" + `). On success the
tool is skipped with a ✓ note. On failure the tool's auth flow runs:

  mastodon / linkding / github   prompt for the required tokens (or
                                 instance URL) and write the matching
                                 <TOOL>_<KEY>= entries to the env file.
  spotify                        prompt for SPOTIFY_CLIENT_ID if needed,
                                 then shell into ` + "`spotify-to-markdown auth`" + `
                                 (PKCE browser flow).
  pocketcasts                    prompt for POCKETCASTS_EMAIL /
                                 POCKETCASTS_PASSWORD, then shell into
                                 ` + "`pocketcasts-to-markdown login`" + ` to
                                 cache a bearer token.

After each flow runs, the credential check repeats. Up to three
attempts per tool before giving up and moving on.

Flags:
  --tool <slug>    authorize a single tool (default: walk all)
  --force          re-prompt even if validation would pass (useful for
                   rotating an already-working credential)`,
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
	log.Debugf("env file: %s", envFilePath)

	in := bufio.NewReader(os.Stdin)
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
	defer cancel()

	var anyFailed bool
	for _, t := range tools {
		fmt.Printf("\n=== %s ===\n", t.Label)
		if err := runAuthForTool(ctx, t, envFilePath, in, force); err != nil {
			log.Errorf("%s: %v", t.Slug, err)
			anyFailed = true
		}
	}
	fmt.Println()
	if anyFailed {
		return errors.New("one or more tools could not be authorized")
	}
	return nil
}

// runAuthForTool ensures tool t has working credentials. The flow:
//
//  1. If !force, run a credential check (`<tool> export --since 1m`).
//     On success, print ✓ and return.
//  2. Otherwise (or if validation failed), run the interactive auth
//     handler. Re-validate. On failure, ask whether to retry. Loops
//     up to maxAuthAttempts times.
//
// Returns an error if the tool ends up un-authorized after all
// attempts; the outer walk continues with the next tool either way.
func runAuthForTool(ctx context.Context, t registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	if !force {
		if err := validateAuth(ctx, t, envFilePath); err == nil {
			fmt.Printf("Already authorized. ✓\n")
			return nil
		} else {
			fmt.Printf("Needs auth (%v).\n", err)
		}
	}

	handler, ok := authHandlers[t.Slug]
	if !ok {
		return fmt.Errorf("no auth handler registered for %s", t.Slug)
	}

	for attempt := 1; attempt <= maxAuthAttempts; attempt++ {
		// Always force=true inside the loop: by construction we've decided
		// the tool needs (re-)auth, so don't skip-on-set.
		err := handler(ctx, t, envFilePath, in, true)
		if err != nil {
			fmt.Printf("Auth flow failed: %v\n", err)
		} else {
			if vErr := validateAuth(ctx, t, envFilePath); vErr == nil {
				fmt.Printf("✓ Authorized.\n")
				return nil
			} else {
				fmt.Printf("Credentials persisted but validation failed: %v\n", vErr)
			}
		}

		if attempt >= maxAuthAttempts {
			break
		}
		ans, _ := readLine(in, fmt.Sprintf("Retry %s auth? [y/N]: ", t.Label))
		if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
			return errors.New("aborted by user")
		}
	}
	return fmt.Errorf("gave up after %d attempt(s)", maxAuthAttempts)
}

// validateAuth runs a lightweight credential check for tool t by
// invoking `<tool> validate-auth`. Every registered tool implements
// this as a family-wide contract: a fast authenticated probe that
// exits 0 on success, non-zero on bad credentials / no token /
// network failure.
//
// Network failures, timeouts, and tool bugs all produce non-nil
// errors here — validateAuth doesn't try to distinguish "bad creds"
// from "bad network." The user re-running auth will see the same
// error and can act on it.
func validateAuth(ctx context.Context, t registry.Tool, envFilePath string) error {
	binPath, _, err := runner.Resolve(t.Binary)
	if err != nil {
		return err
	}

	vctx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()

	c := exec.CommandContext(vctx, binPath, "validate-auth")
	c.Env = append(os.Environ(), reloadEnv(envFilePath)...)
	c.Stdout = io.Discard
	var stderr bytes.Buffer
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		summary := firstNonEmptyLine(stderr.Bytes())
		if summary == "" {
			summary = err.Error()
		}
		return errors.New(summary)
	}
	return nil
}

func firstNonEmptyLine(b []byte) string {
	for _, raw := range bytes.Split(b, []byte("\n")) {
		s := strings.TrimSpace(string(raw))
		if s == "" {
			continue
		}
		// Skip logrus timestamp prefixes for tidier output.
		if i := strings.Index(s, "msg="); i >= 0 {
			s = strings.Trim(s[i+4:], `"`)
		}
		return s
	}
	return ""
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
// `pocketcasts-to-markdown login --email <> --password-stdin`. The
// password is kept in memory through the login attempt and only
// persisted to the env file if the login succeeds — this avoids
// polluting the file with a known-bad password if the credentials
// turn out to be wrong. POCKETCASTS_PASSWORD is stripped from the
// subprocess env because the tool's login refuses --password-stdin
// when env-var password is also set.
func authPocketcasts(ctx context.Context, t registry.Tool, envFilePath string, in *bufio.Reader, force bool) error {
	email, err := promptOrReuse(in, envFilePath, "POCKETCASTS_EMAIL", "Pocket Casts email: ", force, false)
	if err != nil {
		return err
	}
	password, err := promptOrReuse(in, envFilePath, "POCKETCASTS_PASSWORD", "Pocket Casts password: ", force, true)
	if err != nil {
		return err
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
	// Strip POCKETCASTS_PASSWORD from the inherited env: the tool's
	// `login` refuses --password-stdin when env-var password is set.
	// The password reaches login via stdin instead.
	c.Env = append(stripEnvKeys(os.Environ(), "POCKETCASTS_PASSWORD"),
		stripEnvKeys(reloadEnv(envFilePath), "POCKETCASTS_PASSWORD")...)
	c.Stdin = strings.NewReader(password + "\n")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return err
	}

	// Login succeeded — persist credentials so future runs (and token
	// refresh on expiry) can re-auth without prompting.
	return envfile.Upsert(envFilePath, map[string]string{
		"POCKETCASTS_EMAIL":    email,
		"POCKETCASTS_PASSWORD": password,
	})
}

// promptOrReuse returns either a freshly prompted value or the
// existing value (process env, then env file) for the given key,
// matching the same skip-if-set semantics as promptKey.
func promptOrReuse(in *bufio.Reader, envFilePath, key, prompt string, force, secret bool) (string, error) {
	v, err := promptKey(in, key, prompt, force, secret)
	if err != nil {
		return "", err
	}
	if v != "" {
		return v, nil
	}
	// Skipped — fall back to whatever is already configured.
	if existing := os.Getenv(key); existing != "" {
		return existing, nil
	}
	for _, e := range reloadEnv(envFilePath) {
		if strings.HasPrefix(e, key+"=") {
			return strings.TrimPrefix(e, key+"="), nil
		}
	}
	return "", nil
}

// stripEnvKeys returns env with any KEY=VALUE entries whose KEY is in
// keys removed. Useful for hand-off to a subprocess that conflicts on
// an inherited env var.
func stripEnvKeys(env []string, keys ...string) []string {
	if len(keys) == 0 {
		return env
	}
	skip := make(map[string]bool, len(keys))
	for _, k := range keys {
		skip[k] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		eq := strings.IndexByte(e, '=')
		if eq < 0 {
			out = append(out, e)
			continue
		}
		if skip[e[:eq]] {
			continue
		}
		out = append(out, e)
	}
	return out
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
