# Spec: `youtube-to-markdown` — liked-videos digest tool for the family

## Why

The `*-to-markdown` family already covers social posts (mastodon), bookmarks (linkding), code activity (github), music (spotify), and podcasts (pocketcasts). YouTube is the obvious gap on the video side.

Real watch-history access via the YouTube Data API has been deprecated for years (the `HL` playlist endpoint was removed circa 2016-17). The accessible-and-useful alternative is the **Liked Videos playlist (`LL`)**, which doubles as an even better signal for weeknotes: a like is a deliberate "this is worth remembering" action, not a passive view.

The tool fetches liked videos over a time window and renders them as Markdown, slotting into the orchestrator alongside the other five sources.

## Goals

- A new sibling repo `youtube-to-markdown` matching the family's conventions (Cobra/Viper, SQLite cached state, OAuth2 PKCE auth, `env-prefix` env-var pattern, family-wide subcommand contracts).
- Day-grouped Markdown output: title / channel / URL / duration / view count / description excerpt per video.
- Stateful sync: incremental fetch of new likes, cached in local SQLite, so renders work over any historical window (within YouTube's API recency limits captured at sync time).
- Orchestrator integration via one registry entry — `me-to-markdown export --include youtube --since 168h` works once the tool is installed.

## Non-goals

- Watch history. Inaccessible via the public API; not pursuing workarounds (Takeout, scraping).
- Comments, subscriptions, recommendations. These are different signals and out of scope for "what did I find worth remembering."
- Cross-account aggregation. Single-user tool, mirroring the rest of the family.

## Shape

### Data model

YouTube Data API v3, OAuth2 with PKCE flow. Scope: `https://www.googleapis.com/auth/youtube.readonly`.

Two API calls per page during fetch:
1. `playlistItems.list?playlistId=LL&part=snippet,contentDetails&maxResults=50` — gives title, channel, video ID, **`snippet.publishedAt` = liked-at timestamp**, `contentDetails.videoPublishedAt` = original publish date.
2. `videos.list?id=<batch>&part=contentDetails,statistics` — enriches with `contentDetails.duration` (ISO 8601 duration) and `statistics.viewCount`.

Both API calls cost 1 unit each per request. YouTube's default quota is 10K units/day; a full backfill of a few thousand likes costs <100 units. No quota concern.

### SQLite schema

```sql
CREATE TABLE liked_videos (
  video_id            TEXT PRIMARY KEY,
  title               TEXT NOT NULL,
  channel_id          TEXT NOT NULL,
  channel_title       TEXT NOT NULL,
  description         TEXT,                  -- full description; render truncates
  thumbnail_url       TEXT,                  -- medium-resolution thumbnail
  duration_seconds    INTEGER,               -- parsed from ISO 8601 contentDetails.duration
  view_count          INTEGER,               -- from statistics.viewCount at fetch time
  video_published_at  TEXT NOT NULL,         -- RFC3339; when the video was published
  liked_at            TEXT NOT NULL,         -- RFC3339; playlistItem.snippet.publishedAt
  fetched_at          TEXT NOT NULL,         -- when this row was synced
  raw_json            TEXT                   -- full playlistItem payload for forward-compat
);

CREATE INDEX idx_liked_videos_liked_at ON liked_videos (liked_at);

CREATE TABLE kv (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE schema_version (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT    NOT NULL
);
```

`kv` holds OAuth access/refresh tokens (matching the spotify-to-markdown pattern), plus `sync.last_run` and `sync.last_video_id` for incremental sync bookkeeping.

### Incremental sync semantics

- **First run:** walk the entire LL playlist (paginated), insert all rows. The playlist is server-sorted by liked-at DESC, so the first page is the newest.
- **Subsequent runs:** walk newest-first until we hit a `video_id` already present in `liked_videos`. Insert anything ahead of it. O(new likes) per run.
- **`raw_json`** stores the full `playlistItems` payload so future schema changes can reprocess without re-fetching from the API.

### Subcommand surface

| Subcommand | Purpose |
|---|---|
| `init` | Scaffold `youtube-to-markdown.yaml` + `youtube-to-markdown.md` template in CWD. Print OAuth-client-setup next steps. |
| `auth` | Browser OAuth2 PKCE flow; persists access + refresh tokens to state.db. Requires `YOUTUBE_CLIENT_ID` set first. `--force` re-runs even when a valid token exists. |
| `fetch` | Page through LL, enrich with `videos.list`, dedupe on `video_id`. Stops early when it hits a known ID. |
| `render` | Read cached rows over `--since`/`--until` (filtered by `liked_at`), emit day-grouped Markdown to `--output` (default stdout). API-free. |
| `run` | `fetch && render` — convenience combo for scheduled use. |
| `export` | Orchestrator contract: `fetch && render` to stdout (or `-o`). What `me-to-markdown export` invokes per-tool. |
| `validate-auth` | Hits `channels.list?part=snippet&mine=true` as a cheap auth probe. Single-line `validate-auth: ok (<channel name>)` on success, non-zero exit on bad/expired token. Required by the family-wide validate-auth contract. |
| `version` | Standard version print. Should support `--json` per family contract issue ([me-to-markdown#3](https://github.com/lmorchard/me-to-markdown/issues/3)) once that lands across the family. |

### Render template

Embedded `default.md` (Go text/template), customizable copy written by `init`. Day-grouped, items sorted newest-first within each day:

```markdown
# Liked videos from {{ .StartDate }} to {{ .EndDate }}

## 2026-05-13

- [Title](video_url) — Channel · 14m · 23K views
  > First sentence of the description, truncated...

## 2026-05-14

- [Another Title](video_url) — OtherChannel · 32m · 41K views
  > Another excerpt...
```

**Formatting rules:**
- Duration: `Hh Mm` or `Mm` (e.g. `1h 14m`, `32m`)
- View count: abbreviated (`23K`, `1.2M`, `4.5B`)
- Description excerpt: first sentence or first 240 chars (whichever shorter)
- Empty window: `_No liked videos in this window._`

### Configuration

Yaml + env-var, prefix `YOUTUBE_`. Standard family pattern.

```yaml
# Required: from Google Cloud Console OAuth 2.0 client. No client_secret
# needed (PKCE flow).
client_id: ""

# Local OAuth callback port. Must match the redirect URI on the GCP OAuth
# client (http://127.0.0.1:<port>/callback).
redirect_port: 8888

# Path to the SQLite state file.
# database: "/custom/path/state.db"   # default: $XDG_STATE_HOME/youtube-to-markdown/state.db

# Logging
verbose: false
debug: false
log_json: false
```

Env-var equivalents: `YOUTUBE_CLIENT_ID`, `YOUTUBE_REDIRECT_PORT`, `YOUTUBE_DATABASE`, etc. Cross-cutting `YOUTUBE_VERBOSE` / `YOUTUBE_DEBUG` / `YOUTUBE_LOG_JSON`.

### Orchestrator integration

One new entry in `me-to-markdown/internal/registry/registry.go`, slotted after `spotify` and before `pocketcasts`:

```go
{Binary: "youtube-to-markdown", Slug: "youtube", Label: "YouTube", Repo: "lmorchard/youtube-to-markdown"},
```

That's the entire orchestrator change. Once the new tool implements `validate-auth` per the family contract, `me-to-markdown auth` automatically walks it; `me-to-markdown export` invokes its `export` subcommand alongside the rest.

## Acceptance

- `youtube-to-markdown init` scaffolds the config + template
- `youtube-to-markdown auth` walks the user through Google Cloud setup and completes the OAuth flow against real credentials
- `youtube-to-markdown validate-auth` exits 0 with `validate-auth: ok (<channel name>)`
- `youtube-to-markdown fetch` performs an initial backfill, then incremental sync on re-run
- `youtube-to-markdown export --since 168h` emits day-grouped Markdown for the past week's likes
- After registry update + `me-to-markdown install --from-source`, `me-to-markdown export --include youtube --since 168h` produces a `## YouTube` section in the combined output
- `me-to-markdown auth` (idempotent walk) recognizes YouTube as `Already authorized. ✓` after the per-tool `auth` runs

## Out of scope (future)

- `whoami` subcommand (richer identity output, mirroring mastodon's). Not blocking; can be added once we settle on a family-wide whoami convention.
- Description full-text indexing or search across the cached likes.
- Pulling watch-later (WL) playlist alongside likes. Different signal; might add later.
- A `doctor` subcommand (env + config + cached state diagnostic). Pending family-wide contract ([me-to-markdown#2](https://github.com/lmorchard/me-to-markdown/issues/2)).
