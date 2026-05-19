# Spec: `--env-file`, `install --from-source`, and `auth` for the orchestrator

## Why

The Phase 1 PR (orchestrator MVP, #1) shipped registry / runner / export / install / init / list, but the per-tool auth and config story stayed pure delegation: each tool owns its own config file and auth flow, the orchestrator just shells out.

A real end-to-end run surfaces three rough edges:

1. **Credentials are scattered.** With env-var normalization landed across the five tools (each now exposes a `<TOOL>_<KEY>` env-var convention), a single secrets file would let the orchestrator carry every tool's credentials in one place without centralizing config.
2. **`install` only works against published releases.** During iteration on the family, that means push → wait for CI → download. We have all five sibling repos checked out locally, so a "build from source" install mode is a natural dev-experience improvement.
3. **`auth` is fragmented across tools.** Spotify needs `spotify-to-markdown auth` (browser OAuth). Pocket Casts needs `pocketcasts-to-markdown login` (password). The token-paste tools (mastodon / linkding / github) need user-supplied secrets in env or yaml. Today the user has to remember each flow per-tool; the orchestrator should be the single front door.

## Goals

- **Single secrets file.** `me-to-markdown export` (and `init`, `auth`) loads a `KEY=VALUE` file and merges its entries into each subprocess's environment, so `MASTODON_ACCESS_TOKEN=…` in one file reaches `mastodon-to-markdown export`.
- **`install --from-source`** that builds each registered tool from a sibling repo on disk and copies the binary into the managed bin dir — bypassing GitHub releases entirely. Useful for local dev iteration on the family.
- **`me-to-markdown auth`** that smooths over the per-tool auth flows: shell into each tool's interactive flow where it exists (spotify, pocketcasts), and prompt-then-append-to-env-file for the paste-token tools (mastodon, linkding, github).

## Non-goals

- **Don't centralize config.** Each tool's yaml stays canonical for its non-secret settings. The env-file is for secrets / overrides only.
- **Don't reimplement the per-tool auth flows.** The orchestrator's `auth` shells out to existing tool subcommands where they exist; it doesn't speak OAuth or HTTP itself.
- **Don't gate `install` on the family layout.** `--from-source` is opt-in; the default GitHub-releases path stays.

## Shape

### `--env-file` (and config-level `env_file:`)

- Flag on root: `--env-file <path>` (and `env_file:` viper config). Default: `~/.config/me-to-markdown/env` if it exists, else nothing.
- File format: `KEY=VALUE` per line. `#` starts a comment. Blank lines OK. No shell interpolation, no `export`, no quoting magic — keep it dumb. Values are taken verbatim after the first `=`.
- Loaded once at command startup. Merged into the process env *after* the inherited environment (file entries override inherited entries — the file is a deliberate override for the orchestrator session).
- Applies to every subprocess the orchestrator spawns (`export`, `init`, `auth`).

### `install --from-source <path>`

- New flag `--from-source <dir>`: directory containing the sibling tool checkouts (named `mastodon-to-markdown/`, etc., one level deep).
- Default value when used with no argument: the parent dir of the running `me-to-markdown` binary's source repo — i.e. `../` from `me-to-markdown/`. This matches the actual layout.
- For each selected tool: `go build -o <managed bin dir>/<binary> .` inside the sibling dir. No release fetch, no checksum verification — we built it ourselves.
- Mutually exclusive with `--version`.

### `auth` subcommand

- Iterates registered tools in registry order.
- For tools with a known interactive auth subcommand:
  - **spotify**: `exec spotify-to-markdown auth` (browser OAuth flow). Pass through stdin/stdout/stderr so the user can complete the flow.
  - **pocketcasts**: prompt for email + password if not already set via env-file or env, then `pocketcasts-to-markdown login --email <…> --password-stdin`.
- For paste-token tools (mastodon, linkding, github):
  - Print where to generate the token (with URL).
  - If `--env-file` is writable, prompt for the value and append `<TOOL>_<KEY>=<value>` to the env file.
  - Otherwise print the env-var line for the user to paste themselves.
- Skip tools that already have working credentials (heuristic: their `export --since 1h --dry-run` succeeds, or we read a token from env / env-file). Don't block on every tool every time.

## Acceptance

- `me-to-markdown --env-file ./secrets.env export --since 168h` runs all five tools end-to-end with secrets sourced from the file. No environment leakage between tools (each subprocess gets only the prefix-matched secrets it needs — though we can take a simpler "load all, let each tool ignore non-matching" approach if scope balloons).
- `me-to-markdown install --from-source` against the sibling-repo layout populates the managed bin dir from local builds and `list` shows each as `found (managed)`.
- `me-to-markdown auth` walks the user through filling out the env file end-to-end, calling each tool's existing flow where present.

## Out of scope (future)

- Encryption at rest for the env-file. Plain text is fine for the user's own machine; rotate via `chmod 600`.
- A `--secrets-from <command>` integration (1Password CLI, pass, etc.). Useful, but a separate feature on top of this.
- Cross-tool config sharing (e.g. shared output directory). Spec calls this out as non-goal but worth re-evaluating once the family has more shared semantics.
