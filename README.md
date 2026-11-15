# me-to-markdown

A unified CLI tool for fetching, caching, and exporting personal data exhaust from multiple sources (Mastodon, Linkding, GitHub, Spotify, YouTube, and Pocket Casts).

Designed for periodic personal-data summaries — weeknotes, journals, "what did I do this week" reports, and local LLM ingestion pipelines.

## Architecture

In late 2026, `me-to-markdown` was rewritten from an orchestrator of separate subprocess CLIs into a **single, self-contained Go binary with a local SQLite exhaust cache**:

```
               ┌────────────────────────────────────────────────────────┐
               │                        Sources                         │
               │  [mastodon]  [linkding]  [github]  [spotify]  [youtube]│
               │                      [pocketcasts]                     │
               └───────────────────────────┬────────────────────────────┘
                                           │
                                  sync (parallel API)
                                           ▼
                               ┌───────────────────────┐
                               │  Local SQLite Cache   │
                               │     (exhaust.db)      │
                               │  - items + raw JSON   │
                               │  - sync cursors       │
                               │  - OAuth tokens       │
                               └───────────┬───────────┘
                                           │
                                  export (0ms network)
                                           ▼
                     ┌─────────────────────┼─────────────────────┐
                     ▼                     ▼                     ▼
            [Markdown Formatter]     [Structured JSON]     [JSON Lines]
             (Weeknotes drafts)      (Agent Pipelines)     (Unix pipes)
```

### Key Benefits

1. **Single Binary**: All 6 sources are built directly into Go packages (`internal/sources/`). No package manager, no subprocess management, no release asset downloading.
2. **Decoupled Fetching & Output**:
   - `me-to-markdown sync` hits the APIs and incrementally updates the local SQLite cache.
   - `me-to-markdown export` queries the local SQLite cache in milliseconds. You can run export dozens of times while iterating on templates, prompts, or post drafts with zero network calls.
3. **Structured & Raw Output**:
   - `--format markdown` (default): Pre-formatted Jekyll/Hugo weeknotes sections.
   - `--format json`: Clean structured JSON list with normalized fields (`tags`, `media[]`, `url`, `title`, `summary`).
   - `--format jsonl`: Line-delimited JSON for unix pipes, `jq`, or LLM map-reduce tasks.
   - Preserves original `raw_payload` JSON for every item, so new fields or metadata can always be extracted later without re-fetching.

## Install

Requires Go 1.25+.

```sh
git clone https://github.com/lmorchard/me-to-markdown.git
cd me-to-markdown
make build
```

Binary lands at `./bin/me-to-markdown`. Copy it to `~/.local/bin/` or anywhere on your `$PATH`.

## Quick Start

```sh
# 1. Check source authentication status
me-to-markdown list

# 2. Sync recent activity into the local cache (e.g. last 7 days)
me-to-markdown sync --since 7d

# 3. Inspect cached data volume
me-to-markdown status

# 4. Export Markdown for weeknotes
me-to-markdown export --since 7d -o weeknotes.md

# 5. Or export structured JSON for local models and agents
me-to-markdown export --since 7d --format json -o weeknotes.json
```

## Commands

| Command | What it does |
|---|---|
| `sync` | Fetch recent data from upstream APIs and update local SQLite cache. |
| `export` | Query local SQLite cache and output Markdown, JSON, or JSONL. |
| `status` | Display item counts, time ranges, and sync health per source. |
| `list` | Show registered sources and their authentication status. |
| `auth` | Walk interactive credential setup for any missing/expired sources. |
| `init` | Scaffold `me-to-markdown.yaml` and sample `~/.config/me-to-markdown/env`. |
| `version`| Print version information. |

### `sync` Flags

| Flag | Description | Default |
|---|---|---|
| `--since` | Backfill time window (`YYYY-MM-DD`, `7d`, `168h`) | last synced cursor |
| `--include` | Comma-separated source slugs to sync | all sources |
| `--exclude` | Comma-separated source slugs to skip | none |
| `--full-reindex` | Ignore cached sync cursors and re-query full window | false |

### `export` Flags

| Flag | Description | Default |
|---|---|---|
| `--since` | Start of time window (`YYYY-MM-DD`, `7d`, `168h`) | required |
| `--until` | End of time window (`YYYY-MM-DD`, end-of-day inclusive) | now |
| `-o`, `--output` | Combined output file | stdout |
| `-d`, `--output-dir` | Write one `{slug}.md` (or `{slug}.json`) file per source into this directory | unset |
| `--format` | Output format: `markdown`, `json`, `jsonl` | `markdown` |
| `--sync` | Trigger an automatic sync before querying | false |
| `--include` | Comma-separated source slugs to export | all sources |
| `--exclude` | Comma-separated source slugs to skip | none |

Source slugs: `mastodon`, `linkding`, `github`, `spotify`, `youtube`, `pocketcasts`.

## Credentials & Environment

`me-to-markdown` loads credentials from `$XDG_CONFIG_HOME/me-to-markdown/env` (default `~/.config/me-to-markdown/env`) or ambient environment variables:

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

OAuth tokens for Spotify and YouTube are automatically persisted and refreshed in the local SQLite database (`~/.local/share/me-to-markdown/exhaust.db`). Existing tokens from legacy `state.db` files are automatically discovered on first run.

## License

MIT — see `LICENSE`.
