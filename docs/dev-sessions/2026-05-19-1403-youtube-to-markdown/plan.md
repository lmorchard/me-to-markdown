# youtube-to-markdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a new `youtube-to-markdown` CLI in the `*-to-markdown` family that fetches the user's YouTube Liked Videos, caches them in SQLite, and renders a day-grouped Markdown digest. Add a registry entry to `me-to-markdown` so the orchestrator can drive it alongside the other five tools.

**Architecture:** Scaffold via the `go-cli-builder` skill (Cobra/Viper/SQLite stack, family-standard CI + release workflows), then layer on a YouTube Data API client, OAuth2 PKCE auth (mirroring spotify-to-markdown), incremental sync of the LL playlist, and a day-grouped render template. Family-standard subcommands: `auth`, `fetch`, `render`, `run`, `export`, `validate-auth`, `init`, `version`.

**Tech Stack:** Go 1.25, Cobra/Viper, mattn/go-sqlite3, golang.org/x/oauth2 (or hand-rolled PKCE matching spotify's pattern), YouTube Data API v3.

---

## Pre-flight

**Spec:** `/Users/lorchard/devel/x-to-markdown/me-to-markdown/docs/dev-sessions/2026-05-19-1403-youtube-to-markdown/spec.md`

**Reference code** to mirror (all in `/Users/lorchard/devel/x-to-markdown/`):
- `spotify-to-markdown/` — closest sibling; same OAuth2 PKCE + SQLite shape
- `pocketcasts-to-markdown/` — SQLite cache + bearer token via `kv` table
- `mastodon-to-markdown/` — render template structure (day-grouped)
- `me-to-markdown/internal/registry/registry.go` — receives the final new-tool entry

## File structure

New repo at `/Users/lorchard/devel/x-to-markdown/youtube-to-markdown/`:

```
cmd/
  root.go              # cobra root + initConfig with YOUTUBE_ prefix
  version.go           # standard version subcommand
  init.go              # scaffold yaml + template + next-steps block
  auth.go              # interactive browser OAuth flow
  fetch.go             # paginate LL + enrich, dedupe on video_id
  render.go            # query SQLite, emit day-grouped Markdown
  run.go               # fetch && render combo
  export.go            # orchestrator contract (fetch && render to stdout/-o)
  validate_auth.go     # cheap auth probe; family-wide contract
  constants.go         # binary name, env prefix, etc.

internal/
  config/config.go     # Config struct (Database, Verbose, Debug, LogJSON, Spotify-like sub-struct)
  database/
    database.go        # DB wrapper, connection, KV helpers, Close
    schema.go          # InitSchema (CREATE TABLE statements)
    liked_videos.go    # Upsert + QueryByLikedAt + KnownID helpers
  youtube/
    client.go          # YouTube Data API client (CurrentUser, LikedVideosPage, EnrichVideos)
    types.go           # API response shapes
    duration.go        # ISO 8601 duration parser
  youtubeauth/
    auth.go            # Authenticator (OAuth2 PKCE)
    pkce.go            # verifier + challenge helpers
    store.go           # token load/save via kv table
  render/
    markdown.go        # Render() entrypoint
    format.go          # duration formatter, view-count abbreviation, description excerpt
  templates/
    default.md         # embedded default Go text/template
    templates.go       # loader (embed.FS + file fallback)
    types.go           # template data structs
  timewindow/
    parse.go           # copied from family timewindow pkg (same as mastodon/linkding/etc.)

main.go
go.mod / go.sum
Makefile
README.md
LICENSE
.gitignore
.github/workflows/{ci,release,rolling-release}.yml
```

Modified in `me-to-markdown/`:
- `internal/registry/registry.go` — one new entry between spotify and pocketcasts

---

### Task 1: Scaffold the project via go-cli-builder

**Files:** Everything under `/Users/lorchard/devel/x-to-markdown/youtube-to-markdown/` (new repo).

- [ ] **Step 1: Invoke the go-cli-builder skill**

Use the `Skill` tool with `skill: "go-cli-builder"` and args that name the new project. The skill will prompt for the project details; respond with:
- Binary / module name: `youtube-to-markdown`
- GitHub repo: `lmorchard/youtube-to-markdown`
- Module path: `github.com/lmorchard/youtube-to-markdown`
- Project location: `/Users/lorchard/devel/x-to-markdown/youtube-to-markdown`
- Short description: "Fetch your YouTube liked-videos history and render it as Markdown"
- Long description: copy the "Why" + "Goals" sections from `spec.md`
- Go version: `1.25` (matching the rest of the family post-alignment)
- Include SQLite: yes
- Include rolling-release workflow: yes

If the skill defaults differ from family conventions (e.g. older Go version or `golangci-lint-action@v6`), accept the defaults — Task 14 audits and aligns them.

- [ ] **Step 2: Verify scaffold built clean**

```bash
cd /Users/lorchard/devel/x-to-markdown/youtube-to-markdown
make build && make test
```

Expected: builds, prints "Built: youtube-to-markdown", test output `ok` for any scaffolded tests.

- [ ] **Step 3: Initialize git + first commit**

If `go-cli-builder` didn't already `git init`:

```bash
cd /Users/lorchard/devel/x-to-markdown/youtube-to-markdown
git init -b main
gh repo create lmorchard/youtube-to-markdown --private --source=. --remote=origin
git add -A
git commit -m "Initial scaffold from go-cli-builder"
git push -u origin main
```

If `gh repo create` says the repo exists already, just `git remote add origin git@github.com:lmorchard/youtube-to-markdown.git && git push -u origin main`.

- [ ] **Step 4: Bootstrap a `cmd/constants.go`**

Some go-cli-builder versions don't write this; mastodon/spotify both have one. Create the file if missing:

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/constants.go
package cmd

const (
	appName    = "youtube-to-markdown"
	configName = "youtube-to-markdown"
	envPrefix  = "YOUTUBE"
)
```

- [ ] **Step 5: Commit the constants if added**

```bash
git add cmd/constants.go
git commit -m "Add cmd/constants.go with app + env prefix constants"
git push origin main
```

---

### Task 2: Family-standard root.go + Config struct

**Files:**
- Modify: `cmd/root.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Ensure root.go matches the family pattern**

Open `cmd/root.go`. Confirm it:
- Imports `strings`
- Calls `viper.SetEnvPrefix(envPrefix)` (so env vars use `YOUTUBE_<KEY>`)
- Calls `viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))`
- Calls `viper.AutomaticEnv()`

If any of those is missing, add it. Pattern to mirror — `spotify-to-markdown/cmd/root.go` lines ~85-90.

Add a persistent `--database` flag if not present:

```go
rootCmd.PersistentFlags().String("database", "", "database file path (default: $XDG_STATE_HOME/youtube-to-markdown/state.db)")
_ = viper.BindPFlag("database", rootCmd.PersistentFlags().Lookup("database"))
viper.SetDefault("database", defaultDatabasePath())
```

Where `defaultDatabasePath()` is the same XDG helper spotify uses (copy from `spotify-to-markdown/cmd/root.go`).

- [ ] **Step 2: Define the Config struct**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/config/config.go
package config

// Config holds application configuration.
type Config struct {
	Database string

	Verbose bool
	Debug   bool
	LogJSON bool

	// YouTube OAuth client + scope settings.
	YouTube YouTubeConfig
}

// YouTubeConfig holds the OAuth client + scope settings.
type YouTubeConfig struct {
	ClientID     string
	RedirectPort int
	Scopes       []string
}
```

- [ ] **Step 3: Populate Config in GetConfig()**

In `cmd/root.go` `GetConfig()`, populate the new fields:

```go
cfg = &config.Config{
	Database: viper.GetString("database"),
	Verbose:  viper.GetBool("verbose"),
	Debug:    viper.GetBool("debug"),
	LogJSON:  viper.GetBool("log_json"),
	YouTube: config.YouTubeConfig{
		ClientID:     viper.GetString("client_id"),
		RedirectPort: viper.GetInt("redirect_port"),
		Scopes:       viper.GetStringSlice("scopes"),
	},
}
```

And in `initConfig()` add the defaults:

```go
viper.SetDefault("redirect_port", 8888)
viper.SetDefault("scopes", []string{"https://www.googleapis.com/auth/youtube.readonly"})
```

- [ ] **Step 4: Build and verify**

```bash
make build && go vet ./...
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/root.go internal/config/config.go
git commit -m "Add YOUTUBE_ env prefix, --database flag, Config struct"
git push origin main
```

---

### Task 3: SQLite schema + database wrapper

**Files:**
- Create: `internal/database/database.go`
- Create: `internal/database/schema.go`
- Test: `internal/database/database_test.go`

- [ ] **Step 1: Write the failing KV roundtrip test**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/database/database_test.go
package database

import (
	"path/filepath"
	"testing"
)

func TestKVRoundtrip(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.SetKV("foo", "bar"); err != nil {
		t.Fatalf("SetKV: %v", err)
	}
	v, ok, err := db.GetKV("foo")
	if err != nil {
		t.Fatalf("GetKV: %v", err)
	}
	if !ok || v != "bar" {
		t.Fatalf("GetKV: got (%q, %v), want (bar, true)", v, ok)
	}

	_, ok, err = db.GetKV("missing")
	if err != nil {
		t.Fatalf("GetKV missing: %v", err)
	}
	if ok {
		t.Fatalf("GetKV missing: ok=true, want false")
	}
}
```

- [ ] **Step 2: Run test, watch it fail**

```bash
go test ./internal/database/... -v -run TestKVRoundtrip
```

Expected: FAIL (package doesn't exist yet).

- [ ] **Step 3: Write schema.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/database/schema.go
package database

const schemaSQL = `
CREATE TABLE IF NOT EXISTS liked_videos (
  video_id            TEXT PRIMARY KEY,
  title               TEXT NOT NULL,
  channel_id          TEXT NOT NULL,
  channel_title       TEXT NOT NULL,
  description         TEXT,
  thumbnail_url       TEXT,
  duration_seconds    INTEGER,
  view_count          INTEGER,
  video_published_at  TEXT NOT NULL,
  liked_at            TEXT NOT NULL,
  fetched_at          TEXT NOT NULL,
  raw_json            TEXT
);

CREATE INDEX IF NOT EXISTS idx_liked_videos_liked_at ON liked_videos (liked_at);

CREATE TABLE IF NOT EXISTS kv (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_version (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT    NOT NULL
);
`
```

- [ ] **Step 4: Write database.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/database/database.go
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a sqlite3 *sql.DB with helpers specific to youtube-to-markdown.
type DB struct {
	conn *sql.DB
}

// New opens (creating if necessary) the SQLite database at path, initializes
// the schema, and returns a *DB. Caller must Close.
func New(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.initSchema(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return db, nil
}

func (d *DB) initSchema() error {
	_, err := d.conn.Exec(schemaSQL)
	return err
}

// Conn exposes the underlying *sql.DB for callers that need it.
func (d *DB) Conn() *sql.DB { return d.conn }

// Close closes the underlying connection.
func (d *DB) Close() error { return d.conn.Close() }

// GetKV returns the value for key. ok is false if no row exists.
func (d *DB) GetKV(key string) (value string, ok bool, err error) {
	row := d.conn.QueryRow(`SELECT value FROM kv WHERE key = ?`, key)
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

// SetKV upserts key=value into the kv table.
func (d *DB) SetKV(key, value string) error {
	_, err := d.conn.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

// DeleteKV removes a row from the kv table. No error if the key didn't exist.
func (d *DB) DeleteKV(key string) error {
	_, err := d.conn.Exec(`DELETE FROM kv WHERE key = ?`, key)
	return err
}
```

- [ ] **Step 5: Run test, watch it pass**

```bash
go test ./internal/database/... -v -run TestKVRoundtrip
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/database/
git commit -m "Add SQLite schema + DB wrapper + KV helpers (with tests)"
git push origin main
```

---

### Task 4: ISO 8601 duration parser

**Files:**
- Create: `internal/youtube/duration.go`
- Test: `internal/youtube/duration_test.go`

YouTube returns video durations as ISO 8601 (`PT1H14M3S`, `PT32M`, `PT45S`). We need to convert to seconds for storage.

- [ ] **Step 1: Write the failing test**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtube/duration_test.go
package youtube

import "testing"

func TestParseDuration(t *testing.T) {
	cases := map[string]int{
		"PT0S":      0,
		"PT45S":     45,
		"PT32M":     32 * 60,
		"PT1H":      60 * 60,
		"PT1H14M3S": 60*60 + 14*60 + 3,
		"PT2H30M":   2*60*60 + 30*60,
		"":          0,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Errorf("ParseDuration(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseDurationRejectsGarbage(t *testing.T) {
	for _, in := range []string{"1H", "PT1", "PTabc", "foo"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) should error", in)
		}
	}
}
```

- [ ] **Step 2: Run test, watch it fail**

```bash
go test ./internal/youtube/... -v
```

Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Implement duration.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtube/duration.go
package youtube

import (
	"fmt"
	"regexp"
	"strconv"
)

// isoDurationPattern matches the youtube subset of ISO 8601: PT[<H>H][<M>M][<S>S].
// The seconds-only forms like PT45S, minutes-only PT32M, etc., all match.
var isoDurationPattern = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

// ParseDuration converts a YouTube ISO 8601 duration string (e.g. "PT1H14M3S")
// into a total number of seconds. An empty string returns 0 with no error
// (some videos report no duration). Any non-conforming input returns an error.
func ParseDuration(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	m := isoDurationPattern.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "") {
		return 0, fmt.Errorf("invalid ISO 8601 duration %q", s)
	}
	total := 0
	for i, mult := range []int{3600, 60, 1} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", s, err)
		}
		total += n * mult
	}
	return total, nil
}
```

- [ ] **Step 4: Run test, watch it pass**

```bash
go test ./internal/youtube/... -v
```

Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/youtube/
git commit -m "Add ISO 8601 duration parser for YouTube video durations"
git push origin main
```

---

### Task 5: YouTube API client types + low-level HTTP

**Files:**
- Create: `internal/youtube/types.go`
- Create: `internal/youtube/client.go`

- [ ] **Step 1: Define API response types**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtube/types.go
package youtube

// CurrentChannel is the subset of /channels?mine=true we use for validate-auth.
type CurrentChannel struct {
	ID    string
	Title string
}

// PlaylistItem is one entry in the LL (Liked Videos) playlist.
type PlaylistItem struct {
	VideoID         string
	Title           string
	Description     string
	ChannelID       string
	ChannelTitle    string
	Thumbnail       string // medium-resolution URL
	LikedAt         string // RFC3339; snippet.publishedAt for the LL playlist
	VideoPublishedAt string // RFC3339; contentDetails.videoPublishedAt
	RawJSON         []byte // full raw response item, for forward-compat storage
}

// VideoDetails are the per-video fields we fetch via videos.list.
type VideoDetails struct {
	VideoID         string
	DurationSeconds int
	ViewCount       int64
}

// PageToken is YouTube's opaque pagination cursor.
type PageToken string
```

- [ ] **Step 2: Implement the HTTP client**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtube/client.go
package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBaseURL = "https://www.googleapis.com/youtube/v3"

// TokenSource produces a current, valid OAuth access token. The Authenticator
// in internal/youtubeauth satisfies this interface.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// Client is a thin YouTube Data API v3 client. It does not retry on 401 —
// token freshness is the TokenSource's responsibility.
type Client struct {
	http        *http.Client
	tokenSource TokenSource
}

// New constructs a Client.
func New(tokenSource TokenSource, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{http: httpClient, tokenSource: tokenSource}
}

// CurrentChannel returns the channel info for the authenticated user.
// Used as a cheap auth probe.
func (c *Client) CurrentChannel(ctx context.Context) (CurrentChannel, error) {
	var resp struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := c.doJSON(ctx, "/channels?part=snippet&mine=true", &resp); err != nil {
		return CurrentChannel{}, err
	}
	if len(resp.Items) == 0 {
		return CurrentChannel{}, fmt.Errorf("no channel found for authenticated user")
	}
	return CurrentChannel{ID: resp.Items[0].ID, Title: resp.Items[0].Snippet.Title}, nil
}

// LikedVideosPage fetches one page of the LL (Liked Videos) playlist.
// Pass an empty pageToken for the first page. Returns the items plus the
// next pageToken (empty when no more pages remain).
func (c *Client) LikedVideosPage(ctx context.Context, pageToken PageToken) ([]PlaylistItem, PageToken, error) {
	q := url.Values{}
	q.Set("part", "snippet,contentDetails")
	q.Set("playlistId", "LL")
	q.Set("maxResults", "50")
	if pageToken != "" {
		q.Set("pageToken", string(pageToken))
	}

	var resp struct {
		NextPageToken string `json:"nextPageToken"`
		Items         []struct {
			Snippet struct {
				PublishedAt  string `json:"publishedAt"`
				Title        string `json:"title"`
				Description  string `json:"description"`
				ChannelID    string `json:"videoOwnerChannelId"`
				ChannelTitle string `json:"videoOwnerChannelTitle"`
				ResourceId   struct {
					VideoID string `json:"videoId"`
				} `json:"resourceId"`
				Thumbnails struct {
					Medium struct {
						URL string `json:"url"`
					} `json:"medium"`
				} `json:"thumbnails"`
			} `json:"snippet"`
			ContentDetails struct {
				VideoID          string `json:"videoId"`
				VideoPublishedAt string `json:"videoPublishedAt"`
			} `json:"contentDetails"`
		} `json:"items"`
	}

	// Capture raw response so we can keep raw_json per row.
	rawBody, err := c.doRaw(ctx, "/playlistItems?"+q.Encode())
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal(rawBody, &resp); err != nil {
		return nil, "", fmt.Errorf("decode playlistItems response: %w", err)
	}

	// Re-extract per-item raw payloads so each row stores its own JSON.
	var rawWrapper struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(rawBody, &rawWrapper)

	out := make([]PlaylistItem, 0, len(resp.Items))
	for i, it := range resp.Items {
		var raw []byte
		if i < len(rawWrapper.Items) {
			raw = []byte(rawWrapper.Items[i])
		}
		out = append(out, PlaylistItem{
			VideoID:          it.ContentDetails.VideoID,
			Title:            it.Snippet.Title,
			Description:      it.Snippet.Description,
			ChannelID:        it.Snippet.ChannelID,
			ChannelTitle:     it.Snippet.ChannelTitle,
			Thumbnail:        it.Snippet.Thumbnails.Medium.URL,
			LikedAt:          it.Snippet.PublishedAt,
			VideoPublishedAt: it.ContentDetails.VideoPublishedAt,
			RawJSON:          raw,
		})
	}
	return out, PageToken(resp.NextPageToken), nil
}

// EnrichVideos fetches duration + view count for a batch of video IDs
// (videos.list accepts up to 50 IDs per call).
func (c *Client) EnrichVideos(ctx context.Context, ids []string) (map[string]VideoDetails, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 50 {
		return nil, fmt.Errorf("EnrichVideos: max 50 IDs per call (got %d)", len(ids))
	}
	q := url.Values{}
	q.Set("part", "contentDetails,statistics")
	q.Set("id", strings.Join(ids, ","))

	var resp struct {
		Items []struct {
			ID             string `json:"id"`
			ContentDetails struct {
				Duration string `json:"duration"`
			} `json:"contentDetails"`
			Statistics struct {
				ViewCount string `json:"viewCount"`
			} `json:"statistics"`
		} `json:"items"`
	}
	if err := c.doJSON(ctx, "/videos?"+q.Encode(), &resp); err != nil {
		return nil, err
	}

	out := make(map[string]VideoDetails, len(resp.Items))
	for _, it := range resp.Items {
		dur, _ := ParseDuration(it.ContentDetails.Duration)
		var views int64
		if it.Statistics.ViewCount != "" {
			views, _ = strconv.ParseInt(it.Statistics.ViewCount, 10, 64)
		}
		out[it.ID] = VideoDetails{
			VideoID:         it.ID,
			DurationSeconds: dur,
			ViewCount:       views,
		}
	}
	return out, nil
}

// doRaw issues an authenticated GET and returns the body bytes.
func (c *Client) doRaw(ctx context.Context, path string) ([]byte, error) {
	token, err := c.tokenSource.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("youtube API GET %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) doJSON(ctx context.Context, path string, out interface{}) error {
	body, err := c.doRaw(ctx, path)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Build to verify clean**

```bash
make build && go vet ./...
```

Expected: clean (no tests yet for this layer; integration covered by `validate-auth` smoke later).

- [ ] **Step 4: Commit**

```bash
git add internal/youtube/
git commit -m "Add YouTube Data API client: CurrentChannel, LikedVideosPage, EnrichVideos"
git push origin main
```

---

### Task 6: OAuth2 PKCE flow (`internal/youtubeauth`)

**Files:**
- Create: `internal/youtubeauth/pkce.go`
- Create: `internal/youtubeauth/store.go`
- Create: `internal/youtubeauth/auth.go`

This mirrors `spotify-to-markdown/internal/spotifyauth/` almost line-for-line, with YouTube endpoints. **Open the spotify version and crib aggressively** — same code shape, different URLs.

- [ ] **Step 1: Copy `pkce.go` from spotify-to-markdown**

```bash
cp /Users/lorchard/devel/x-to-markdown/spotify-to-markdown/internal/spotifyauth/pkce.go \
   /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtubeauth/pkce.go
```

Then in the new file, change `package spotifyauth` → `package youtubeauth`. PKCE helpers are URL-agnostic; no other changes needed.

- [ ] **Step 2: Copy + adapt `store.go`**

```bash
cp /Users/lorchard/devel/x-to-markdown/spotify-to-markdown/internal/spotifyauth/store.go \
   /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtubeauth/store.go
```

Edit the new file:
- `package spotifyauth` → `package youtubeauth`
- Token storage uses the same `kv` table; the KV keys should be prefixed `youtube.` to avoid any potential collision (e.g. `youtube.access_token`, `youtube.refresh_token`, `youtube.expires_at`). Find/replace `spotify.` → `youtube.` in the constants.
- The store reads from a `*sql.DB` and uses the same SQL shape; should compile against the new `database` package's `Conn()` method.

- [ ] **Step 3: Copy + adapt `auth.go`**

```bash
cp /Users/lorchard/devel/x-to-markdown/spotify-to-markdown/internal/spotifyauth/auth.go \
   /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/youtubeauth/auth.go
```

Edit:
- `package spotifyauth` → `package youtubeauth`
- Auth endpoint constants:
  - `authURL = "https://accounts.spotify.com/authorize"` → `"https://accounts.google.com/o/oauth2/v2/auth"`
  - `tokenURL = "https://accounts.spotify.com/api/token"` → `"https://oauth2.googleapis.com/token"`
- `DefaultScopes` value:

```go
var DefaultScopes = []string{
	"https://www.googleapis.com/auth/youtube.readonly",
}
```

- Google's OAuth token endpoint accepts `access_type=offline` to issue refresh tokens. Add it to the authorize-URL query string in `BuildAuthorizeURL()`:

```go
q.Set("access_type", "offline")
q.Set("prompt", "consent")
```

(Without `prompt=consent`, Google may skip issuing a refresh token if the user has previously authorized. Belt-and-suspenders.)

- The token-exchange POST should send `grant_type=authorization_code` and the PKCE `code_verifier` — exactly the same form fields spotify uses, no client_secret. Verify it does and adjust if needed.
- The redirect URI path is the same: `http://127.0.0.1:<port>/callback`.

- [ ] **Step 4: Confirm the package builds**

```bash
cd /Users/lorchard/devel/x-to-markdown/youtube-to-markdown
go build ./internal/youtubeauth/
```

If any unresolved identifiers — typically `database.DB` or `sql.DB` — fix the imports to match this project's module path (`github.com/lmorchard/youtube-to-markdown/internal/database`).

- [ ] **Step 5: Build the whole project**

```bash
make build
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/youtubeauth/
git commit -m "Add OAuth2 PKCE auth for YouTube (mirrors spotify-to-markdown pattern)"
git push origin main
```

---

### Task 7: liked_videos database helpers

**Files:**
- Create: `internal/database/liked_videos.go`
- Modify: `internal/database/database_test.go` (add tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/database/database_test.go`:

```go
import (
	"time"
)

func TestLikedVideosUpsertAndQuery(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	v1 := LikedVideo{
		VideoID: "abc123", Title: "A", ChannelID: "c1", ChannelTitle: "C1",
		LikedAt: "2026-05-13T10:00:00Z", VideoPublishedAt: "2026-05-13T09:00:00Z",
	}
	v2 := LikedVideo{
		VideoID: "def456", Title: "B", ChannelID: "c1", ChannelTitle: "C1",
		LikedAt: "2026-05-14T10:00:00Z", VideoPublishedAt: "2026-05-14T09:00:00Z",
	}
	if err := db.UpsertLikedVideo(v1); err != nil {
		t.Fatalf("Upsert v1: %v", err)
	}
	if err := db.UpsertLikedVideo(v2); err != nil {
		t.Fatalf("Upsert v2: %v", err)
	}

	// Re-upsert v1 with a new title — should overwrite, not insert.
	v1upd := v1
	v1upd.Title = "A updated"
	if err := db.UpsertLikedVideo(v1upd); err != nil {
		t.Fatalf("Re-upsert v1: %v", err)
	}

	// Range query: only v2 (liked May 14 onward).
	since, _ := time.Parse(time.RFC3339, "2026-05-14T00:00:00Z")
	until, _ := time.Parse(time.RFC3339, "2026-05-15T00:00:00Z")
	got, err := db.QueryLikedVideosBetween(since, until)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].VideoID != "def456" {
		t.Fatalf("Query: got %#v, want one row with video_id def456", got)
	}

	// KnownVideoID
	known, err := db.KnownVideoID("abc123")
	if err != nil || !known {
		t.Fatalf("KnownVideoID abc123 = (%v, %v), want (true, nil)", known, err)
	}
	known, err = db.KnownVideoID("missing")
	if err != nil || known {
		t.Fatalf("KnownVideoID missing = (%v, %v), want (false, nil)", known, err)
	}
}
```

- [ ] **Step 2: Run, watch it fail**

```bash
go test ./internal/database/... -v -run TestLikedVideosUpsertAndQuery
```

Expected: FAIL (functions don't exist yet).

- [ ] **Step 3: Implement `internal/database/liked_videos.go`**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/database/liked_videos.go
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LikedVideo mirrors one row of the liked_videos table.
type LikedVideo struct {
	VideoID          string
	Title            string
	ChannelID        string
	ChannelTitle     string
	Description      string
	ThumbnailURL     string
	DurationSeconds  int
	ViewCount        int64
	VideoPublishedAt string // RFC3339
	LikedAt          string // RFC3339
	FetchedAt        string // RFC3339
	RawJSON          []byte
}

// UpsertLikedVideo inserts or replaces a single row keyed on video_id.
// FetchedAt defaults to now (UTC) when empty on the input.
func (d *DB) UpsertLikedVideo(v LikedVideo) error {
	if v.FetchedAt == "" {
		v.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.conn.Exec(`
		INSERT INTO liked_videos (
		  video_id, title, channel_id, channel_title, description, thumbnail_url,
		  duration_seconds, view_count, video_published_at, liked_at,
		  fetched_at, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(video_id) DO UPDATE SET
		  title              = excluded.title,
		  channel_id         = excluded.channel_id,
		  channel_title      = excluded.channel_title,
		  description        = excluded.description,
		  thumbnail_url      = excluded.thumbnail_url,
		  duration_seconds   = excluded.duration_seconds,
		  view_count         = excluded.view_count,
		  video_published_at = excluded.video_published_at,
		  liked_at           = excluded.liked_at,
		  fetched_at         = excluded.fetched_at,
		  raw_json           = excluded.raw_json
	`,
		v.VideoID, v.Title, v.ChannelID, v.ChannelTitle, v.Description, v.ThumbnailURL,
		v.DurationSeconds, v.ViewCount, v.VideoPublishedAt, v.LikedAt,
		v.FetchedAt, string(v.RawJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert liked_video %s: %w", v.VideoID, err)
	}
	return nil
}

// KnownVideoID reports whether the given video_id is already cached.
func (d *DB) KnownVideoID(id string) (bool, error) {
	row := d.conn.QueryRow(`SELECT 1 FROM liked_videos WHERE video_id = ?`, id)
	var n int
	if err := row.Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// QueryLikedVideosBetween returns rows where liked_at falls in [since, until].
// Sorted ascending by liked_at; render is responsible for any re-ordering.
func (d *DB) QueryLikedVideosBetween(since, until time.Time) ([]LikedVideo, error) {
	rows, err := d.conn.Query(`
		SELECT video_id, title, channel_id, channel_title, description, thumbnail_url,
		       duration_seconds, view_count, video_published_at, liked_at,
		       fetched_at, raw_json
		FROM liked_videos
		WHERE liked_at >= ? AND liked_at <= ?
		ORDER BY liked_at ASC
	`, since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []LikedVideo
	for rows.Next() {
		var v LikedVideo
		var raw string
		if err := rows.Scan(
			&v.VideoID, &v.Title, &v.ChannelID, &v.ChannelTitle, &v.Description, &v.ThumbnailURL,
			&v.DurationSeconds, &v.ViewCount, &v.VideoPublishedAt, &v.LikedAt,
			&v.FetchedAt, &raw,
		); err != nil {
			return nil, err
		}
		v.RawJSON = []byte(raw)
		out = append(out, v)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests, watch them pass**

```bash
go test ./internal/database/... -v
```

Expected: both `TestKVRoundtrip` and `TestLikedVideosUpsertAndQuery` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/database/
git commit -m "Add liked_videos Upsert / KnownVideoID / QueryBetween helpers"
git push origin main
```

---

### Task 8: validate-auth subcommand (smoke test for the API client)

**Files:**
- Create: `cmd/validate_auth.go`

This is the earliest end-to-end smoke surface; getting it green proves auth + the API client work.

- [ ] **Step 1: Implement cmd/validate_auth.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/validate_auth.go
package cmd

import (
	"fmt"

	"github.com/lmorchard/youtube-to-markdown/internal/database"
	"github.com/lmorchard/youtube-to-markdown/internal/youtube"
	"github.com/lmorchard/youtube-to-markdown/internal/youtubeauth"
	"github.com/spf13/cobra"
)

var validateAuthCmd = &cobra.Command{
	Use:   "validate-auth",
	Short: "Check whether the cached YouTube OAuth token is accepted",
	Long: `Use the cached OAuth tokens (acquired via ` + "`auth`" + `) to make a
minimal authenticated request (channels.list?mine=true) and exit 0 if the
token is accepted, non-zero otherwise.

If no token is cached, fails with a hint to run ` + "`auth`" + `.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := GetConfig()
		if c.YouTube.ClientID == "" {
			return fmt.Errorf("client_id is not set; add it to youtube-to-markdown.yaml or set YOUTUBE_CLIENT_ID")
		}

		db, err := database.New(c.Database)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		auth := youtubeauth.New(youtubeauth.Config{
			ClientID:     c.YouTube.ClientID,
			RedirectPort: c.YouTube.RedirectPort,
			Scopes:       c.YouTube.Scopes,
		}, db.Conn(), nil)

		client := youtube.New(auth, nil)
		channel, err := client.CurrentChannel(cmd.Context())
		if err != nil {
			return fmt.Errorf("youtube auth check: %w", err)
		}
		fmt.Printf("validate-auth: ok (authenticated as %s)\n", channel.Title)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateAuthCmd)
}
```

The `youtubeauth.New` signature should match what you wrote in Task 6 (cribbed from spotify). If the param shape differs, adjust the call site here to match.

- [ ] **Step 2: Build**

```bash
make build && go vet ./...
```

Expected: clean.

- [ ] **Step 3: Verify it errors cleanly with no token (no smoke against real API yet)**

```bash
./youtube-to-markdown validate-auth 2>&1 | head -3
```

Expected: an error mentioning `client_id is not set` (since no config / env var).

- [ ] **Step 4: Commit**

```bash
git add cmd/validate_auth.go
git commit -m "Add validate-auth subcommand: cheap auth probe for orchestrator contract"
git push origin main
```

---

### Task 9: `auth` subcommand (interactive browser OAuth)

**Files:**
- Create: `cmd/auth.go`

- [ ] **Step 1: Implement cmd/auth.go (mirror spotify's pattern)**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/auth.go
package cmd

import (
	"fmt"

	"github.com/lmorchard/youtube-to-markdown/internal/database"
	"github.com/lmorchard/youtube-to-markdown/internal/youtubeauth"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "One-time interactive OAuth flow for YouTube access",
	Long: `Runs the OAuth2 PKCE flow against Google's accounts API. Opens a browser
for user consent and exchanges the authorization code for access + refresh
tokens, which are persisted in the local state.db.

Requires client_id (from a Google Cloud OAuth 2.0 client) to be set via
YOUTUBE_CLIENT_ID or in the config file.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := GetLogger()
		c := GetConfig()
		if c.YouTube.ClientID == "" {
			return fmt.Errorf("client_id is not set; add it to youtube-to-markdown.yaml or set YOUTUBE_CLIENT_ID")
		}

		db, err := database.New(c.Database)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		auth := youtubeauth.New(youtubeauth.Config{
			ClientID:     c.YouTube.ClientID,
			RedirectPort: c.YouTube.RedirectPort,
			Scopes:       c.YouTube.Scopes,
		}, db.Conn(), nil)

		ctx := cmd.Context()
		if err := auth.RunInteractiveFlow(ctx); err != nil {
			return fmt.Errorf("auth flow failed: %w", err)
		}
		log.Info("Authorization complete; tokens persisted")
		fmt.Println("✅ Authorization complete; tokens persisted to", c.Database)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
}
```

The method name `RunInteractiveFlow` matches what spotify uses; verify your `internal/youtubeauth/auth.go` exposes the same name (Task 6 should have left it identical).

- [ ] **Step 2: Build**

```bash
make build && go vet ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add cmd/auth.go
git commit -m "Add auth subcommand: browser OAuth2 PKCE flow"
git push origin main
```

---

### Task 10: `fetch` subcommand (incremental sync of LL playlist)

**Files:**
- Create: `cmd/fetch.go`

- [ ] **Step 1: Implement cmd/fetch.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/fetch.go
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/lmorchard/youtube-to-markdown/internal/database"
	"github.com/lmorchard/youtube-to-markdown/internal/youtube"
	"github.com/lmorchard/youtube-to-markdown/internal/youtubeauth"
	"github.com/spf13/cobra"
)

const fetchCmdTimeout = 5 * time.Minute

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Sync new likes from YouTube into the local cache",
	Long: `Page through the LL (Liked Videos) playlist newest-first, enrich each
batch with duration + view count via videos.list, and upsert new rows into
the local liked_videos cache. Stops early when it hits a video_id that is
already known — so subsequent runs are O(new likes).

Requires a valid cached OAuth token (run ` + "`auth`" + ` first).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := GetLogger()
		c := GetConfig()
		if c.YouTube.ClientID == "" {
			return fmt.Errorf("client_id is not set; add it to youtube-to-markdown.yaml or set YOUTUBE_CLIENT_ID")
		}

		db, err := database.New(c.Database)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = db.Close() }()

		auth := youtubeauth.New(youtubeauth.Config{
			ClientID:     c.YouTube.ClientID,
			RedirectPort: c.YouTube.RedirectPort,
			Scopes:       c.YouTube.Scopes,
		}, db.Conn(), nil)
		client := youtube.New(auth, nil)

		ctx, cancel := context.WithTimeout(cmd.Context(), fetchCmdTimeout)
		defer cancel()

		var pageToken youtube.PageToken
		var totalNew, totalSeen int
		startedAt := time.Now()
		for {
			items, next, err := client.LikedVideosPage(ctx, pageToken)
			if err != nil {
				return fmt.Errorf("fetch LL page: %w", err)
			}
			if len(items) == 0 {
				break
			}

			// Find the boundary: how many items in this page are already known?
			var newItems []youtube.PlaylistItem
			hitKnown := false
			for _, it := range items {
				totalSeen++
				known, err := db.KnownVideoID(it.VideoID)
				if err != nil {
					return fmt.Errorf("check known %s: %w", it.VideoID, err)
				}
				if known {
					hitKnown = true
					break
				}
				newItems = append(newItems, it)
			}

			if len(newItems) > 0 {
				// Enrich the new items with duration + view count.
				ids := make([]string, len(newItems))
				for i, it := range newItems {
					ids[i] = it.VideoID
				}
				details, err := client.EnrichVideos(ctx, ids)
				if err != nil {
					return fmt.Errorf("enrich videos: %w", err)
				}

				for _, it := range newItems {
					d := details[it.VideoID] // zero value if missing — acceptable
					row := database.LikedVideo{
						VideoID:          it.VideoID,
						Title:            it.Title,
						ChannelID:        it.ChannelID,
						ChannelTitle:     it.ChannelTitle,
						Description:      it.Description,
						ThumbnailURL:     it.Thumbnail,
						DurationSeconds:  d.DurationSeconds,
						ViewCount:        d.ViewCount,
						VideoPublishedAt: it.VideoPublishedAt,
						LikedAt:          it.LikedAt,
						RawJSON:          it.RawJSON,
					}
					if err := db.UpsertLikedVideo(row); err != nil {
						return fmt.Errorf("upsert %s: %w", it.VideoID, err)
					}
					totalNew++
				}
			}

			if hitKnown || next == "" {
				break
			}
			pageToken = next
		}

		if err := db.SetKV("sync.last_run", time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("record last_run: %w", err)
		}

		log.Infof("fetch complete in %s: %d seen, %d new", time.Since(startedAt).Round(time.Millisecond), totalSeen, totalNew)
		fmt.Printf("fetch: %d new like(s), %d seen\n", totalNew, totalSeen)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(fetchCmd)
}
```

- [ ] **Step 2: Build**

```bash
make build && go vet ./...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add cmd/fetch.go
git commit -m "Add fetch subcommand: incremental LL playlist sync"
git push origin main
```

---

### Task 11: Render formatters with tests

**Files:**
- Create: `internal/render/format.go`
- Test: `internal/render/format_test.go`

- [ ] **Step 1: Write failing tests**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/render/format_test.go
package render

import "testing"

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{
		0:       "",
		45:      "45s",
		60:      "1m",
		3 * 60:  "3m",
		32 * 60: "32m",
		60 * 60:           "1h",
		60*60 + 14*60:     "1h 14m",
		2*60*60 + 30*60:   "2h 30m",
		2 * 60 * 60:       "2h",
	}
	for in, want := range cases {
		got := FormatDuration(in)
		if got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatViewCount(t *testing.T) {
	cases := map[int64]string{
		0:           "0 views",
		1:           "1 view",
		999:         "999 views",
		1_000:       "1.0K views",
		23_456:      "23K views",
		1_200_000:   "1.2M views",
		2_500_000_000: "2.5B views",
	}
	for in, want := range cases {
		got := FormatViewCount(in)
		if got != want {
			t.Errorf("FormatViewCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDescriptionExcerpt(t *testing.T) {
	long := "First sentence. Second sentence about something completely different and unrelated. " +
		"More filler text follows."
	cases := map[string]string{
		"":                                "",
		"Short":                           "Short",
		"A. B.":                           "A.",
		"No terminator":                   "No terminator",
		long:                              "First sentence.",
	}
	for in, want := range cases {
		got := DescriptionExcerpt(in, 240)
		if got != want {
			t.Errorf("DescriptionExcerpt(%q) = %q, want %q", in, got, want)
		}
	}

	// Long input with no period within budget falls back to char truncation.
	noPeriod := "No periods here just words that go on and on without any sentence-ending punctuation at all"
	got := DescriptionExcerpt(noPeriod, 40)
	if len(got) > 41 || got == noPeriod {
		t.Errorf("DescriptionExcerpt(no-period, 40) = %q (len %d), want truncated", got, len(got))
	}
}
```

- [ ] **Step 2: Run, watch them fail**

```bash
go test ./internal/render/... -v
```

Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Implement format.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/render/format.go
package render

import (
	"fmt"
	"strings"
)

// FormatDuration renders a duration in seconds as a compact string:
// "45s", "32m", "1h 14m", "2h". Zero returns the empty string so the
// template can omit the dot-separator field.
func FormatDuration(seconds int) string {
	switch {
	case seconds <= 0:
		return ""
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		hours := seconds / 3600
		mins := (seconds % 3600) / 60
		if mins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
}

// FormatViewCount renders a view count compactly: "23K views", "1.2M views",
// "1 view" / "999 views" exact for sub-thousand. 0 returns "0 views" rather
// than an empty string so the field reads consistently.
func FormatViewCount(n int64) string {
	switch {
	case n == 1:
		return "1 view"
	case n < 1_000:
		return fmt.Sprintf("%d views", n)
	case n < 1_000_000:
		return fmt.Sprintf("%s views", trimZero(float64(n)/1_000)+"K")
	case n < 1_000_000_000:
		return fmt.Sprintf("%s views", trimZero(float64(n)/1_000_000)+"M")
	default:
		return fmt.Sprintf("%s views", trimZero(float64(n)/1_000_000_000)+"B")
	}
}

// trimZero formats a float with one decimal place, dropping ".0".
// 1.0 -> "1", 1.2 -> "1.2", 23.4 -> "23"  (we drop the decimal on >=10).
func trimZero(f float64) string {
	if f >= 10 {
		return fmt.Sprintf("%.0f", f)
	}
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

// DescriptionExcerpt returns the first sentence (up to maxChars) of s.
// If no sentence terminator (period) exists within maxChars, falls back to
// a hard char-truncated prefix at the last space boundary (with no ellipsis).
// Empty input returns empty.
func DescriptionExcerpt(s string, maxChars int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= maxChars {
		// Try the first sentence first; fall back to whole string.
		if i := indexSentenceEnd(s); i > 0 {
			return s[:i+1]
		}
		return s
	}
	// Window: first maxChars. Prefer the last sentence end inside it.
	window := s[:maxChars]
	if i := indexSentenceEnd(window); i > 0 {
		return window[:i+1]
	}
	// Last space boundary
	if i := strings.LastIndex(window, " "); i > 0 {
		return window[:i]
	}
	return window
}

// indexSentenceEnd returns the index of the first period followed by a space
// or end-of-string. Returns -1 if none.
func indexSentenceEnd(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' && (i == len(s)-1 || s[i+1] == ' ' || s[i+1] == '\n') {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run tests, watch them pass**

```bash
go test ./internal/render/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/render/
git commit -m "Add render formatters: duration, view count, description excerpt"
git push origin main
```

---

### Task 12: Embedded template + render entrypoint + cmd/render.go

**Files:**
- Create: `internal/templates/default.md`
- Create: `internal/templates/templates.go`
- Create: `internal/templates/types.go`
- Create: `internal/render/markdown.go`
- Create: `cmd/render.go`

- [ ] **Step 1: Write the default template**

```markdown
{{!-- /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/templates/default.md
     (Go text/template syntax; the comment above is markdown, ignored at render) --}}
# Liked videos from {{ .StartDate }} to {{ .EndDate }}

{{ if not .Days -}}
_No liked videos in this window._
{{- else -}}
{{ range .Days }}
## {{ .Date }}

{{ range .Videos -}}
- [{{ .Title }}]({{ .URL }}) — {{ .ChannelTitle }}{{ if .Duration }} · {{ .Duration }}{{ end }}{{ if .ViewCount }} · {{ .ViewCount }}{{ end }}
{{- if .Excerpt }}
  > {{ .Excerpt }}
{{- end }}
{{ end }}
{{ end }}
{{- end }}
```

The leading `{{!-- ... --}}` block is markdown-as-comment; remove it if Go's parser complains — the rest of the file is the template proper. Keep the file plain `default.md` (no embedded comment) if simpler:

```markdown
# Liked videos from {{ .StartDate }} to {{ .EndDate }}

{{ if not .Days -}}
_No liked videos in this window._
{{- else -}}
{{ range .Days }}
## {{ .Date }}

{{ range .Videos -}}
- [{{ .Title }}]({{ .URL }}) — {{ .ChannelTitle }}{{ if .Duration }} · {{ .Duration }}{{ end }}{{ if .ViewCount }} · {{ .ViewCount }}{{ end }}
{{- if .Excerpt }}
  > {{ .Excerpt }}
{{- end }}
{{ end }}
{{ end }}
{{- end }}
```

- [ ] **Step 2: Define template data structs**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/templates/types.go
package templates

// TemplateData is the root structure exposed to the default.md template.
type TemplateData struct {
	StartDate string // YYYY-MM-DD
	EndDate   string // YYYY-MM-DD
	Days      []Day  // empty when no videos in window
}

// Day groups liked videos by the date (in the user's timezone) of their like.
type Day struct {
	Date   string // YYYY-MM-DD
	Videos []Video
}

// Video is one row in a Day's bullet list.
type Video struct {
	Title        string
	URL          string
	ChannelTitle string
	Duration     string // formatted by render.FormatDuration ("32m", "1h 14m", "")
	ViewCount    string // formatted by render.FormatViewCount ("23K views", etc.)
	Excerpt      string // formatted by render.DescriptionExcerpt (may be empty)
}
```

- [ ] **Step 3: Implement the template loader**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/templates/templates.go
package templates

import (
	"embed"
	"fmt"
	"io"
	"os"
	"text/template"
)

//go:embed default.md
var embedded embed.FS

// Renderer wraps a parsed Go text/template.
type Renderer struct {
	tpl *template.Template
}

// NewRenderer returns the embedded default template.
func NewRenderer() (*Renderer, error) {
	return parseFS(embedded, "default.md")
}

// NewRendererFromFile loads a custom template from disk.
func NewRendererFromFile(path string) (*Renderer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", path, err)
	}
	tpl, err := template.New(path).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", path, err)
	}
	return &Renderer{tpl: tpl}, nil
}

func parseFS(fs embed.FS, name string) (*Renderer, error) {
	b, err := fs.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read embedded template %s: %w", name, err)
	}
	tpl, err := template.New(name).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("parse embedded template %s: %w", name, err)
	}
	return &Renderer{tpl: tpl}, nil
}

// Render writes the template output for the given data to w.
func (r *Renderer) Render(w io.Writer, data TemplateData) error {
	return r.tpl.Execute(w, data)
}

// GetDefaultTemplate returns the embedded default template's source text
// (used by `init` when writing the customizable copy alongside the config).
func GetDefaultTemplate() (string, error) {
	b, err := embedded.ReadFile("default.md")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

- [ ] **Step 4: Implement internal/render/markdown.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/internal/render/markdown.go
package render

import (
	"fmt"
	"sort"
	"time"

	"github.com/lmorchard/youtube-to-markdown/internal/database"
	"github.com/lmorchard/youtube-to-markdown/internal/templates"
)

const (
	videoURLPrefix = "https://www.youtube.com/watch?v="
	excerptLimit   = 240
)

// BuildTemplateData groups rows by their liked-at calendar day (in the user's
// local timezone) and applies per-field formatters. Days are sorted ascending;
// videos within each day are sorted newest-first.
func BuildTemplateData(rows []database.LikedVideo, since, until time.Time) (templates.TemplateData, error) {
	byDay := map[string][]templates.Video{}
	for _, r := range rows {
		t, err := time.Parse(time.RFC3339, r.LikedAt)
		if err != nil {
			return templates.TemplateData{}, fmt.Errorf("parse liked_at %q: %w", r.LikedAt, err)
		}
		dayKey := t.Local().Format("2006-01-02")
		byDay[dayKey] = append(byDay[dayKey], templates.Video{
			Title:        r.Title,
			URL:          videoURLPrefix + r.VideoID,
			ChannelTitle: r.ChannelTitle,
			Duration:     FormatDuration(r.DurationSeconds),
			ViewCount:    FormatViewCount(r.ViewCount),
			Excerpt:      DescriptionExcerpt(r.Description, excerptLimit),
		})
	}

	dates := make([]string, 0, len(byDay))
	for d := range byDay {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	days := make([]templates.Day, 0, len(dates))
	for _, d := range dates {
		vids := byDay[d]
		// Within a day, sort newest-first by title-of-stored-order — we already
		// inserted in liked_at ASC order from the DB, so reverse for newest-first.
		sort.SliceStable(vids, func(i, j int) bool { return i > j })
		days = append(days, templates.Day{Date: d, Videos: vids})
	}

	return templates.TemplateData{
		StartDate: since.Local().Format("2006-01-02"),
		EndDate:   until.Local().Format("2006-01-02"),
		Days:      days,
	}, nil
}
```

- [ ] **Step 5: Implement cmd/render.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/render.go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/lmorchard/youtube-to-markdown/internal/database"
	"github.com/lmorchard/youtube-to-markdown/internal/render"
	"github.com/lmorchard/youtube-to-markdown/internal/templates"
	"github.com/lmorchard/youtube-to-markdown/internal/timewindow"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render cached liked videos to Markdown over a time window",
	Long: `Query the local cache for liked videos within [--since, --until] and
emit a day-grouped Markdown digest. Does not hit the YouTube API; run ` + "`fetch`" + `
first to sync new likes.`,
	SilenceUsage: true,
	RunE:         runRender,
}

func init() {
	rootCmd.AddCommand(renderCmd)

	renderCmd.Flags().String("since", "168h", "start of window (YYYY-MM-DD or Go duration like 168h)")
	renderCmd.Flags().String("until", "", "end of window (YYYY-MM-DD; defaults to now)")
	renderCmd.Flags().StringP("output", "o", "", "output file (default: stdout)")
	renderCmd.Flags().String("template", "", "custom template file (default: embedded)")

	_ = viper.BindPFlag("render.since", renderCmd.Flags().Lookup("since"))
	_ = viper.BindPFlag("render.until", renderCmd.Flags().Lookup("until"))
	_ = viper.BindPFlag("render.output", renderCmd.Flags().Lookup("output"))
	_ = viper.BindPFlag("render.template", renderCmd.Flags().Lookup("template"))
}

func runRender(cmd *cobra.Command, args []string) error {
	c := GetConfig()
	since, until, err := timewindow.Parse(
		viper.GetString("render.since"),
		viper.GetString("render.until"),
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("invalid time window: %w", err)
	}

	db, err := database.New(c.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryLikedVideosBetween(since, until)
	if err != nil {
		return fmt.Errorf("query liked_videos: %w", err)
	}

	data, err := render.BuildTemplateData(rows, since, until)
	if err != nil {
		return err
	}

	var renderer *templates.Renderer
	if tplPath := viper.GetString("render.template"); tplPath != "" {
		renderer, err = templates.NewRendererFromFile(tplPath)
	} else {
		renderer, err = templates.NewRenderer()
	}
	if err != nil {
		return err
	}

	out := viper.GetString("render.output")
	w := os.Stdout
	if out != "" && out != "-" {
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	return renderer.Render(w, data)
}
```

The exact signature of `timewindow.Parse` should match the one used by the family. If your scaffold's `internal/timewindow/parse.go` returns `(*timewindow.TimeRange, error)` instead of `(start, end, err)`, adjust the call here to match. Compare against `mastodon-to-markdown/internal/timewindow/` or `linkding-to-markdown/internal/timewindow/`.

- [ ] **Step 6: Build**

```bash
make build && go vet ./... && go test ./...
```

Expected: all clean + green.

- [ ] **Step 7: Commit**

```bash
git add internal/templates/ internal/render/ cmd/render.go
git commit -m "Add render: embedded template, day grouping, cmd/render.go"
git push origin main
```

---

### Task 13: Glue subcommands (run, export, init)

**Files:**
- Create: `cmd/run.go`
- Create: `cmd/export.go`
- Create: `cmd/init.go`

- [ ] **Step 1: Implement cmd/run.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/run.go
package cmd

import (
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "fetch + render in one shot (default convenience combo)",
	Long: `Convenience combo of ` + "`fetch`" + ` and ` + "`render`" + ` for
scheduled / unattended use. Time-window flags apply to the render step.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := fetchCmd.RunE(fetchCmd, nil); err != nil {
			return err
		}
		return renderCmd.RunE(renderCmd, nil)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	// run inherits render's time-window flags
	runCmd.Flags().AddFlagSet(renderCmd.Flags())
}
```

- [ ] **Step 2: Implement cmd/export.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/export.go
package cmd

import (
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Orchestrator-facing export (fetch + render to stdout/-o)",
	Long: `Family-wide ` + "`export`" + ` contract: runs ` + "`fetch`" + ` to
sync new likes, then ` + "`render`" + ` to emit Markdown for the given time
window. Used by the me-to-markdown orchestrator.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := fetchCmd.RunE(fetchCmd, nil); err != nil {
			return err
		}
		return renderCmd.RunE(renderCmd, nil)
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().AddFlagSet(renderCmd.Flags())
}
```

- [ ] **Step 3: Implement cmd/init.go**

```go
// /Users/lorchard/devel/x-to-markdown/youtube-to-markdown/cmd/init.go
package cmd

import (
	"fmt"
	"os"

	"github.com/lmorchard/youtube-to-markdown/internal/templates"
	"github.com/spf13/cobra"
)

const defaultConfigContent = `# Configuration file for youtube-to-markdown

# Path to the SQLite database that stores fetched likes and OAuth tokens.
# Default: $XDG_STATE_HOME/youtube-to-markdown/state.db
# database: "/custom/path/state.db"

# Logging
verbose: false
debug: false
log_json: false

# Required. From a Google Cloud OAuth 2.0 client — env: YOUTUBE_CLIENT_ID
# Create one at: https://console.cloud.google.com/apis/credentials
# Application type: "Desktop app" (recommended) or "Web application" with
# http://127.0.0.1:8888/callback in the Authorized redirect URIs.
# No client_secret is needed — this tool uses the PKCE flow.
client_id: ""

# Port for the local OAuth callback listener — env: YOUTUBE_REDIRECT_PORT
# Must match the redirect URI on the OAuth client.
redirect_port: 8888

# OAuth scopes to request — env: YOUTUBE_SCOPES (comma-separated)
# The default covers everything we need. Don't change unless you know why.
# scopes:
#   - https://www.googleapis.com/auth/youtube.readonly
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a configuration and template in the current directory",
	Long: `Create youtube-to-markdown.yaml + youtube-to-markdown.md (template)
in the current directory. Use --force to overwrite existing files.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := GetLogger()
		force, _ := cmd.Flags().GetBool("force")
		templateFile, _ := cmd.Flags().GetString("template-file")

		configFile := "youtube-to-markdown.yaml"
		if fileExists(configFile) && !force {
			return fmt.Errorf("config file %s already exists (use --force to overwrite)", configFile)
		}
		if fileExists(templateFile) && !force {
			return fmt.Errorf("template file %s already exists (use --force to overwrite)", templateFile)
		}

		if err := os.WriteFile(configFile, []byte(defaultConfigContent), 0o644); err != nil {
			return fmt.Errorf("create config: %w", err)
		}
		log.Infof("Created %s", configFile)

		body, err := templates.GetDefaultTemplate()
		if err != nil {
			return fmt.Errorf("read embedded template: %w", err)
		}
		if err := os.WriteFile(templateFile, []byte(body), 0o644); err != nil {
			return fmt.Errorf("create template: %w", err)
		}
		log.Infof("Created %s", templateFile)

		fmt.Println("Initialization complete. Next steps:")
		fmt.Println("  1. Edit youtube-to-markdown.yaml (or set YOUTUBE_CLIENT_ID) — see comments for setup.")
		fmt.Println("  2. (Optional) Customize youtube-to-markdown.md template.")
		fmt.Println("  3. Run: youtube-to-markdown auth")
		fmt.Println("  4. Run: youtube-to-markdown fetch")
		fmt.Println("  5. Run: youtube-to-markdown render --since 168h")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Overwrite existing files")
	initCmd.Flags().String("template-file", "youtube-to-markdown.md", "Name of template file to create")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
```

- [ ] **Step 4: Build**

```bash
make build && go vet ./... && go test ./...
```

Expected: all clean + green.

- [ ] **Step 5: Smoke `init` in a temp dir**

```bash
mkdir -p /tmp/y2m-init-test && cd /tmp/y2m-init-test && rm -f *
/Users/lorchard/devel/x-to-markdown/youtube-to-markdown/youtube-to-markdown init
ls -la
```

Expected: `youtube-to-markdown.yaml` + `youtube-to-markdown.md` written; next-steps printed.

- [ ] **Step 6: Commit**

```bash
cd /Users/lorchard/devel/x-to-markdown/youtube-to-markdown
git add cmd/run.go cmd/export.go cmd/init.go
git commit -m "Add run / export / init subcommands"
git push origin main
```

---

### Task 14: README + family-CI alignment

**Files:**
- Create / rewrite: `README.md`
- Audit: `.github/workflows/ci.yml`, `.github/workflows/rolling-release.yml`
- Audit: `go.mod`

- [ ] **Step 1: Audit go.mod for family alignment**

`go.mod` should say `go 1.25.0` and have no `toolchain` directive (matching what we aligned across the rest of the family). If the scaffold emitted an older version, edit it.

- [ ] **Step 2: Audit ci.yml for family alignment**

Compare against `/Users/lorchard/devel/x-to-markdown/mastodon-to-markdown/.github/workflows/ci.yml`. Differences to fix:
- `go-version: '1.25'` (not `'1.23'`)
- `golangci/golangci-lint-action@v8` (not `@v6`)
- The `[noci]` check uses `env: COMMIT_MSG: ${{ github.event.head_commit.message }}` plus `"$COMMIT_MSG"` in the bash test, not raw `${{ ... }}` interpolation.

- [ ] **Step 3: Audit rolling-release.yml for family alignment**

Match the sibling repos:
- `go-version: '1.25'`
- `prerelease: false` (we already decided rolling releases are installable)

- [ ] **Step 4: Write README**

```markdown
# youtube-to-markdown

Fetch your YouTube **liked videos** over a time window and render them as
Markdown. A sibling of [mastodon-to-markdown](https://github.com/lmorchard/mastodon-to-markdown),
[linkding-to-markdown](https://github.com/lmorchard/linkding-to-markdown),
[github-to-markdown](https://github.com/lmorchard/github-to-markdown),
[spotify-to-markdown](https://github.com/lmorchard/spotify-to-markdown), and
[pocketcasts-to-markdown](https://github.com/lmorchard/pocketcasts-to-markdown);
orchestrated alongside them by
[me-to-markdown](https://github.com/lmorchard/me-to-markdown).

YouTube watch history isn't accessible via the public Data API (deprecated
in 2016-17). Liked videos are accessible — and a much better signal for
weeknotes anyway: a like is a deliberate "this is worth remembering."

## Install

### From release

Download from the [releases page](https://github.com/lmorchard/youtube-to-markdown/releases) for your platform; extract the tarball / zip; drop the binary on `$PATH`.

### From source

```sh
git clone https://github.com/lmorchard/youtube-to-markdown.git
cd youtube-to-markdown
make build
```

## Setup

1. **Create a Google Cloud OAuth 2.0 client**

   - Visit [Google Cloud Console → APIs & Services → Credentials](https://console.cloud.google.com/apis/credentials)
   - Create a new OAuth 2.0 Client ID; application type **Desktop app** (recommended) or **Web application**
   - Add `http://127.0.0.1:8888/callback` as an authorized redirect URI
   - Enable the **YouTube Data API v3** for the project
   - Note the **Client ID** (no client secret needed — PKCE flow)

2. **Scaffold config + template**

   ```sh
   youtube-to-markdown init
   ```

   This creates `youtube-to-markdown.yaml` and `youtube-to-markdown.md` in the current directory.

3. **Configure**

   Either edit `youtube-to-markdown.yaml` with your `client_id`, or set:

   ```sh
   export YOUTUBE_CLIENT_ID="<your-client-id>"
   ```

4. **Authorize (one-time, browser flow)**

   ```sh
   youtube-to-markdown auth
   ```

   Opens a browser to grant `youtube.readonly` access. Tokens persist in the SQLite state file.

## Usage

```sh
# Sync new likes into the local cache (incremental).
youtube-to-markdown fetch

# Render likes from the past week.
youtube-to-markdown render --since 168h -o week.md

# Combined: fetch + render.
youtube-to-markdown run --since 168h -o week.md

# Orchestrator contract (used by me-to-markdown).
youtube-to-markdown export --since 168h
```

## Commands

| Command | Purpose |
|---|---|
| `init` | Scaffold config + template in the current directory. |
| `auth` | One-time browser OAuth flow; persists tokens. |
| `fetch` | Page through the LL playlist; upsert new rows; stop early at known IDs. |
| `render` | Query the cache for `[--since, --until]` and emit day-grouped Markdown. |
| `run` | `fetch` + `render` combo. |
| `export` | Family-wide export contract (`fetch` + `render` to stdout / `-o`). |
| `validate-auth` | Cheap auth probe; exits 0 if creds are accepted, non-zero otherwise. |
| `version` | Print version + commit + build date. |

## Configuration

`youtube-to-markdown.yaml` (created by `init`):

```yaml
# Required: from Google Cloud OAuth 2.0 client.
client_id: ""

# Local OAuth callback port. Must match the redirect URI on the OAuth client.
redirect_port: 8888

# Path to the SQLite state file (default: $XDG_STATE_HOME/youtube-to-markdown/state.db).
# database: "/custom/path/state.db"

# Logging
verbose: false
debug: false
log_json: false
```

Every config key is reachable via an environment variable with the `YOUTUBE_` prefix:

```sh
export YOUTUBE_CLIENT_ID="..."
export YOUTUBE_REDIRECT_PORT=8888
export YOUTUBE_DATABASE="/custom/path/state.db"
```

## Storage

State (cached likes + OAuth tokens) lives at `$XDG_STATE_HOME/youtube-to-markdown/state.db` (defaults to `~/.local/state/youtube-to-markdown/state.db`).

## License

MIT — see `LICENSE`.
```

- [ ] **Step 5: Build / test / vet all clean**

```bash
make build && make test && go vet ./...
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add README.md go.mod .github/workflows/
git commit -m "Add README, align with family CI / Go 1.25 / lint-action v8"
git push origin main
```

---

### Task 15: Add registry entry to me-to-markdown

**Files:**
- Modify: `/Users/lorchard/devel/x-to-markdown/me-to-markdown/internal/registry/registry.go`

- [ ] **Step 1: Add the new tool to the registry**

Open `internal/registry/registry.go`. The `Tools` slice currently has five entries. Add one between `spotify` and `pocketcasts`:

```go
{Binary: "youtube-to-markdown", Slug: "youtube", Label: "YouTube", Repo: "lmorchard/youtube-to-markdown"},
```

So the updated slice looks like:

```go
var Tools = []Tool{
	{Binary: "mastodon-to-markdown", Slug: "mastodon", Label: "Mastodon", Repo: "lmorchard/mastodon-to-markdown"},
	{Binary: "linkding-to-markdown", Slug: "linkding", Label: "Linkding", Repo: "lmorchard/linkding-to-markdown"},
	{Binary: "github-to-markdown", Slug: "github", Label: "GitHub", Repo: "lmorchard/github-to-markdown"},
	{Binary: "spotify-to-markdown", Slug: "spotify", Label: "Spotify", Repo: "lmorchard/spotify-to-markdown"},
	{Binary: "youtube-to-markdown", Slug: "youtube", Label: "YouTube", Repo: "lmorchard/youtube-to-markdown"},
	{Binary: "pocketcasts-to-markdown", Slug: "pocketcasts", Label: "Pocket Casts", Repo: "lmorchard/pocketcasts-to-markdown"},
}
```

- [ ] **Step 2: Build me-to-markdown**

```bash
cd /Users/lorchard/devel/x-to-markdown/me-to-markdown
make build && make test && go vet ./...
```

Expected: clean.

- [ ] **Step 3: Reinstall dev binaries via --from-source so the orchestrator picks up the new tool**

```bash
./me-to-markdown install --from-source
./me-to-markdown list
```

Expected: list shows 6 tools, including `youtube` at `$XDG_DATA_HOME/me-to-markdown/bin/youtube-to-markdown`.

- [ ] **Step 4: Verify orchestrator idempotent auth flow includes the new tool**

```bash
./me-to-markdown auth --tool youtube --force
```

Expected: walks the youtube auth flow (prompts for client_id if not in env file, then shells into `youtube-to-markdown auth`).

- [ ] **Step 5: Commit me-to-markdown change**

```bash
git add internal/registry/registry.go
git commit -m "Register youtube-to-markdown in the orchestrator

Slots between spotify and pocketcasts in registry order — alphabetical-ish
and roughly 'video sits between music and podcasts.' Once the tool is on
\$PATH (or installed via me-to-markdown install --from-source), this is
the only orchestrator change needed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push origin main
```

---

### Task 16: End-to-end smoke run

- [ ] **Step 1: Run the new tool's full setup interactively**

```sh
cd /Users/lorchard/devel/x-to-markdown/youtube-to-markdown
./youtube-to-markdown init             # in a scratch dir
# Edit youtube-to-markdown.yaml or set YOUTUBE_CLIENT_ID
./youtube-to-markdown auth              # browser flow
./youtube-to-markdown validate-auth     # confirms token works
./youtube-to-markdown fetch             # initial backfill
./youtube-to-markdown render --since 720h -o /tmp/y2m-smoke.md
head -20 /tmp/y2m-smoke.md
```

Expected: day-grouped Markdown digest with real liked videos.

- [ ] **Step 2: Run me-to-markdown export including youtube**

```sh
cd /Users/lorchard/devel/x-to-markdown/me-to-markdown
./me-to-markdown export --since 168h --include youtube -o /tmp/y2m-orchestrated.md
cat /tmp/y2m-orchestrated.md
```

Expected: a `## YouTube` section with the same content as the standalone render.

- [ ] **Step 3: Full family run**

```sh
./me-to-markdown export --since 168h -o /tmp/full-week.md
wc -l /tmp/full-week.md
grep -c '^## ' /tmp/full-week.md
```

Expected: six `## <Label>` section headers (Mastodon, Linkding, GitHub, Spotify, YouTube, Pocket Casts), line count meaningfully larger than the previous five-tool baseline.

---

## Plan self-review

Spec coverage (each of the spec's `Goals` and `Acceptance` items):
- ✅ "Sibling repo matching family conventions" — Task 1 (scaffold) + Task 14 (CI alignment)
- ✅ "Day-grouped Markdown output" — Task 12 (template) + Task 11 (formatters) + Task 12 (BuildTemplateData)
- ✅ "Stateful sync with SQLite cache" — Task 3 (schema), Task 7 (helpers), Task 10 (fetch with dedupe)
- ✅ "Orchestrator integration via one registry entry" — Task 15
- ✅ All subcommands in the surface table — Tasks 8 (validate-auth), 9 (auth), 10 (fetch), 12 (render), 13 (run/export/init), plus `version` from the scaffold
- ✅ `validate-auth` family contract — Task 8
- ✅ Per-tool acceptance criteria (init scaffolds, auth completes, validate-auth ok, fetch backfills, export emits `## YouTube`, me-to-markdown auth idempotent walk recognizes the tool) — covered by Tasks 13, 9, 8, 10, 15, 16

Out-of-scope items from the spec correctly absent from the plan:
- `whoami` (richer identity), description full-text search, watch-later playlist, `doctor` subcommand — none of these appear as tasks. ✅

Type consistency:
- `youtubeauth.Config` shape (ClientID / RedirectPort / Scopes) used identically across `validate_auth.go`, `auth.go`, `fetch.go` ✅
- `database.LikedVideo` struct used identically in `liked_videos.go` (definition), tests, and `cmd/fetch.go` ✅
- `youtube.PlaylistItem`/`VideoDetails` types used identically across client.go and fetch.go ✅
- `templates.TemplateData`/`Day`/`Video` types match between definition (Task 12) and use in `render.BuildTemplateData` (Task 12) and the template file ✅

Placeholders: none — every step has concrete file paths and (for code steps) full code blocks.

Scope: focused on one tool + one orchestrator-side line. No decomposition needed.
