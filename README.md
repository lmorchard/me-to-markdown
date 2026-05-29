# me-to-markdown

Run the family of `*-to-markdown` tools in parallel over a single time window
and concatenate the output into one combined Markdown document. Designed for
periodic personal-data summaries — weeknotes, journals, "what did I do this
week" reports — pulled from multiple sources at once.

## What it does

`me-to-markdown export --since 168h -o weeknotes.md` runs each registered
tool's `export` subcommand in parallel:

- [`mastodon-to-markdown`](https://github.com/lmorchard/mastodon-to-markdown) — Mastodon posts, boosts, favorites
- [`linkding-to-markdown`](https://github.com/lmorchard/linkding-to-markdown) — Linkding bookmarks
- [`github-to-markdown`](https://github.com/lmorchard/github-to-markdown) — GitHub activity
- [`spotify-to-markdown`](https://github.com/lmorchard/spotify-to-markdown) — Spotify listening history
- [`youtube-to-markdown`](https://github.com/lmorchard/youtube-to-markdown) — YouTube liked videos
- [`pocketcasts-to-markdown`](https://github.com/lmorchard/pocketcasts-to-markdown) — Pocket Casts episodes

...then concatenates each tool's output into a single Markdown document with
`## {Tool Label}` section headers, in registry order. Tools that fail render
an error section in place of their output (unless `--omit-errors` is set);
the orchestrator's exit code is non-zero if any tool failed.

Pass `--output-dir <dir>` instead of `-o` to write one Markdown file per
source (`mastodon.md`, `github.md`, …), each with a `# {Label}` heading,
rather than one concatenated document. The directory is created if needed;
existing unrelated files are left in place.

The orchestrator deliberately stays thin: each underlying tool keeps its own
config, state, and authentication. `me-to-markdown` is a coordinator, not an
abstraction layer.

## Install

### Build from source

Requires Go 1.21+.

```sh
git clone https://github.com/lmorchard/me-to-markdown.git
cd me-to-markdown
make build
```

Binary lands at `./me-to-markdown`. Copy it somewhere on `$PATH` if you like.

### Pre-built binaries

Tagged releases publish binaries for linux/{amd64,arm64},
darwin/{amd64,arm64}, and windows/amd64. See the
[Releases page](https://github.com/lmorchard/me-to-markdown/releases).

### Installing the tools

The orchestrator needs the per-tool binaries to actually do anything. Tool
resolution checks `$PATH` first, then `$XDG_DATA_HOME/me-to-markdown/bin`.
You can install however you like; the convenience command:

```sh
me-to-markdown install
```

downloads each tool's latest tagged release into the managed directory
(`~/.local/share/me-to-markdown/bin/` by default) and verifies SHA-256
checksums. To install one at a time or pin a specific version:

```sh
me-to-markdown install mastodon
me-to-markdown install github --version v0.2.0
```

For dev iteration on the family, build directly from local sibling-repo
checkouts instead of fetching releases:

```sh
me-to-markdown install --from-source            # auto-detect siblings
me-to-markdown install --from-source=/path/to   # explicit source root
```

The auto-detect form walks up from the running binary looking for a
directory whose children include at least one tool's repo
(`mastodon-to-markdown/`, `linkding-to-markdown/`, etc.).

To check what the orchestrator sees:

```sh
me-to-markdown list
```

## Quick start

```sh
# 1. Install the underlying tools (skip if they're already on your $PATH).
me-to-markdown install

# 2. Run the orchestrated auth flow. Walks every tool, prompts for
#    tokens / credentials, writes them to the shared env file, and
#    shells into the interactive auth flows for Spotify (browser OAuth)
#    and Pocket Casts (password login).
me-to-markdown auth

# 3. Run the orchestrator.
me-to-markdown export --since 168h -o weeknotes.md
```

`me-to-markdown auth` writes secrets to `$XDG_CONFIG_HOME/me-to-markdown/env`
by default (override with `--env-file`). Subsequent runs of `export`
pick the file up automatically and merge its entries into every
subprocess's environment, so each tool sees its `<TOOL>_<KEY>`
secrets without per-tool config plumbing.

If you'd rather edit yaml configs by hand, `me-to-markdown init` still
scaffolds one per tool in the current directory.

## Commands

| Command   | What it does |
| --------- | ------------ |
| `export`  | Run every selected tool's `export` in parallel; concatenate the output. Main user-facing command. |
| `install` | Download release binaries into the managed bin dir, verify SHA-256 (or `--from-source` to build from local sibling repos). |
| `init`    | Run each tool's `init` to scaffold config files; print next-step instructions for interactive auth flows. |
| `auth`    | Walk the per-tool authorization flows; persist secrets to the shared env file. |
| `list`    | Show registered tools, where each binary resolves (`$PATH` / managed / missing), and its reported version. |
| `version` | Print version, commit, and build date. |

Run any command with `--help` for full usage details.

### `export` flags

| Flag | Description | Default |
|---|---|---|
| `--since` | Start of window (`YYYY-MM-DD` or Go duration like `168h`) | required (or set in config) |
| `--until` | End of window (`YYYY-MM-DD`, end-of-day inclusive) | now |
| `-o`, `--output` | Combined output file | stdout |
| `-d`, `--output-dir` | Write one `{slug}.md` file per tool into this directory instead of concatenating. Mutually exclusive with `-o`. | unset |
| `--include` | Comma-separated tool slugs to run (mutually exclusive with `--exclude`) | all tools |
| `--exclude` | Comma-separated tool slugs to skip (mutually exclusive with `--include`) | none |
| `--omit-errors` | Suppress per-tool error sections in the combined output | false |

Tool slugs match the `SLUG` column in `me-to-markdown list` (e.g. `mastodon`,
`linkding`, `github`, `spotify`, `youtube`, `pocketcasts`).

## Configuration

`me-to-markdown` looks for `me-to-markdown.yaml` in the current directory by
default; pass `--config /path/to/file.yaml` to override.

```yaml
# Default --since value for `export` when no flag is given.
# since: "168h"

# Pre-select which tools to include or exclude. Mutually exclusive; flags
# override config values when provided.
# include:
#   - mastodon
#   - github
# exclude:
#   - spotify

# Suppress per-tool error sections in the combined output (errors still go
# to stderr and the exit code still reflects failures).
# omit_errors: false

# Standard logging flags
verbose: false
debug: false
log_json: false
```

All keys can also be overridden via environment variables prefixed with
`ME_TO_MARKDOWN_` (Viper's standard precedence: flag > env > file > default).

### Shared env file

The orchestrator can load a single `KEY=VALUE` file once at startup and
merge its entries into every subprocess's environment. Useful for
keeping all six tools' secrets in one place without per-tool config
edits.

```sh
me-to-markdown --env-file ~/.config/me-to-markdown/env export --since 168h
```

The default path is `$XDG_CONFIG_HOME/me-to-markdown/env` (fallback
`~/.config/me-to-markdown/env`); if that file exists it's loaded
automatically. Override with `--env-file <path>` or `env_file:` in the
yaml config.

File format is intentionally simple: one `KEY=VALUE` per line, `#`
comments and blank lines ignored, value taken verbatim after the first
`=`. No shell interpolation, no `export` prefix, no quoting magic.

```text
MASTODON_SERVER=https://mastodon.social
MASTODON_ACCESS_TOKEN=...
LINKDING_URL=https://bookmarks.example.com
LINKDING_TOKEN=...
GITHUB_TOKEN=ghp_...
SPOTIFY_CLIENT_ID=...
YOUTUBE_CLIENT_ID=...
YOUTUBE_CLIENT_SECRET=...
POCKETCASTS_EMAIL=you@example.com
POCKETCASTS_PASSWORD=...
```

`me-to-markdown auth` walks the user through filling this out.

## How tool binaries are located

When `export` (or `init`, or `list`) needs to invoke a tool, it resolves the
binary in this order:

1. **`$PATH`**: anything found by `exec.LookPath` wins. This means a local
   dev build of, say, `mastodon-to-markdown` on your `$PATH` shadows any
   installed version.
2. **Managed directory**: `$XDG_DATA_HOME/me-to-markdown/bin/{binary}`,
   which `me-to-markdown install` populates.
3. **Missing**: error with a pointer to `me-to-markdown install`.

## Adding a new tool to the registry

Any Go CLI that implements the canonical
`<tool> export --since <date|duration> [--until <date>] [-o <file>]` contract
can be added. Edit `internal/registry/registry.go`:

```go
{Binary: "yourthing-to-markdown", Slug: "yourthing", Label: "Your Thing",
 Repo: "lmorchard/yourthing-to-markdown"},
```

Then rebuild. The
[`go-cli-builder`](https://github.com/lmorchard/lmorchard-agent-skills/tree/main/go-cli-builder)
skill documents the contract for new fetch-and-export tools.

## Development

```sh
make setup     # install gofumpt + golangci-lint
make build     # build ./me-to-markdown
make format    # go fmt + gofumpt
make lint      # golangci-lint
make test      # go test ./...
make clean     # remove binary
```

### Project layout

```
cmd/                  Cobra subcommands (root, export, install, init, list, version)
internal/config/      Orchestrator's typed config struct
internal/registry/    Static list of registered tools
internal/runner/      Binary resolution ($PATH + managed dir)
docs/dev-sessions/    Session-by-session spec/plan/notes
```

## License

MIT — see `LICENSE`.
