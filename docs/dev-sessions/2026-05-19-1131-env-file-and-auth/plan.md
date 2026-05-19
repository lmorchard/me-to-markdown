# Plan: orchestrator env-file + install --from-source + auth

## Phase 1 — `--env-file` plumbing

**Goal:** every subprocess the orchestrator spawns can receive secrets from a single `KEY=VALUE` file.

1. `internal/envfile/envfile.go`: tiny parser. `Load(path string) ([]string, error)` returns `[]string` of `KEY=VALUE` entries suitable for appending to `exec.Cmd.Env`. Skip blank lines, lines beginning `#`. No quoting magic. Empty path → no file, no error.
2. `internal/config/config.go`: add `EnvFile string` field. `cmd/root.go`: bind `--env-file` flag and `env_file:` viper key.
3. `cmd/root.go` PersistentPreRun: resolve the effective env-file path (flag → viper → default `~/.config/me-to-markdown/env` if it exists). Load it once into a package-level `[]string` (`runner.ExtraEnv` or similar) for later use.
4. `cmd/export.go runTool`: when assembling the `exec.Cmd`, set `cmd.Env = append(os.Environ(), extraEnv...)`. This makes the env-file's values override anything in the inherited environment for the subprocess.
5. `cmd/init.go runInit`: same treatment so per-tool `init` sees the env file.
6. README: document the env-file format + the default path.

**Verification:** run `export` with a temp env-file setting `MASTODON_SERVER=https://example.invalid` — the mastodon section should fail with an error referencing example.invalid (proving the value reached the subprocess), not with "server URL is required".

## Phase 2 — `install --from-source`

**Goal:** build each registered tool from a local sibling-repo checkout and copy the binary into the managed bin dir.

1. `cmd/install.go`: add `--from-source <dir>` flag. Default value: empty (i.e., go through the existing GitHub-releases path). `--from-source` with no value defaults to the parent of the me-to-markdown checkout (auto-detected via `os.Executable()` + `..`); `--from-source <dir>` uses the supplied path.
2. New helper `installFromSource(tool registry.Tool, sourceRoot, binDir string) error`:
   - `sourceDir := filepath.Join(sourceRoot, tool.Binary)` (e.g. `../mastodon-to-markdown`).
   - Verify it exists and contains `go.mod`. Error if not.
   - `exec.Command("go", "build", "-o", filepath.Join(binDir, tool.Binary), ".")` with `cmd.Dir = sourceDir`. Stream output to the logger.
3. Branch in `runInstall` on `--from-source`: skip the GitHub API call and checksum verification; call `installFromSource` instead.
4. Make `--from-source` and `--version` mutually exclusive.
5. README: document the new mode + when to use it (dev iteration vs. user install).

**Verification:** `me-to-markdown install --from-source` against the actual local layout should populate `$XDG_DATA_HOME/me-to-markdown/bin/` with all five binaries, and `me-to-markdown list` should report each as `found (managed)`.

## Phase 3 — `me-to-markdown auth`

**Goal:** one front door for completing every tool's authorization, populating the env file where appropriate.

1. `cmd/auth.go`: new subcommand. For each registered tool, dispatch to a per-tool handler. Per-tool branching is small and explicit (only 5 cases; abstraction overhead would exceed the benefit).
2. **Spotify handler:** `exec.Command("spotify-to-markdown", "auth")` with stdin/stdout/stderr inherited. The PKCE flow opens a browser and waits for the callback — let it run interactively. Pre-check: `SPOTIFY_CLIENT_ID` must be set (in env or env-file) — if not, prompt and append.
3. **Pocket Casts handler:** look for `POCKETCASTS_EMAIL` / `POCKETCASTS_PASSWORD`; if missing, prompt (password via stdin, no echo). Then `pocketcasts-to-markdown login --email <e> --password-stdin`.
4. **Paste-token handlers (mastodon / linkding / github):** print "Generate a token at <URL>", prompt for the value, append `<TOOL>_<KEY>=<value>` to the env-file. If no env-file is configured, print the export-line for the user.
5. Add a `--tool <slug>` flag so the user can run `me-to-markdown auth --tool spotify` to re-do just one. Default = walk all in registry order.
6. README: document the auth flow.

**Verification:** `me-to-markdown auth --tool mastodon` should produce a working `MASTODON_SERVER` + `MASTODON_ACCESS_TOKEN` pair in the env-file, and a subsequent `me-to-markdown export --include mastodon` should succeed.

## Phase 4 — End-to-end smoke run

1. `me-to-markdown install --from-source` to populate managed binaries.
2. `me-to-markdown auth` to fill out the env-file (real credentials).
3. `me-to-markdown --env-file ~/.config/me-to-markdown/env export --since 168h -o weeknotes.md` — confirm five real sections + zero error sections.
4. Capture combined output (lightly redacted) as a session artifact in `notes.md`.

## Risk / open questions

- **Env-file load ordering vs. tool-specific config loading.** Each tool reads viper at its own `initConfig` time; env vars are applied via `viper.AutomaticEnv`. As long as the orchestrator passes the env vars *into the subprocess env*, the tool's existing config flow handles the rest. No tool-side changes needed.
- **`go build` from `install --from-source` runs in the user's $GOPATH.** That's fine for dev iteration but produces binaries with whatever local Go version is installed. Print the Go version in the install log for traceability.
- **Auth flow ergonomics.** The first interactive flow (spotify) opens a browser; if the user is on a headless host, they'll need to set `SPOTIFY_CLIENT_ID` and run the spotify-to-markdown auth flow themselves. The orchestrator should detect headless and skip with a clear "run this on your laptop first" message.
- **Secret prompting UX.** Password fields should suppress echo. Use `golang.org/x/term` (already a stdlib-adjacent dep). Don't reinvent.

## Order of operations

Phase 1 first (other phases depend on the env-file plumbing). Each phase ends with `make build && make test && go vet ./...` and a commit. Branch lands as one PR when all four phases are clean.
