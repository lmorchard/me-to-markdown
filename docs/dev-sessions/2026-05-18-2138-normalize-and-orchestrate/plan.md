# Plan: me-to-markdown orchestrator + tool normalization

_Sibling docs: `spec.md` (settled design), `notes.md` (running log)._

This is a multi-repo session. Each phase below either lives entirely in one
repo or affects a distinct repo, so commits land cleanly without
cross-contamination. PRs are opened per-tool-repo; the orchestrator and skill
work happens in their own repos.

## Phase dependencies

```
Phase 0 (me-to-markdown bootstrap) ──┬─► Phase 2 (orchestrator)
                                     │       │
Phase 1A–1E (5 tool PRs, parallel) ──┴───────┤
                                             ▼
                                    Phase 3 (go-cli-builder skill)
                                             │
                                             ▼
                                    Phase 4 (downstream consumers — DEFERRED)
```

Phase 0 unblocks Phase 2. Phase 1A–E unblock end-to-end testing of Phase 2,
but Phase 2's coding can start as soon as Phase 0 is done (use a dummy
fixture binary or one of the first tool PRs that lands to validate). Phase 3
and Phase 4 are independent of each other after Phase 2 lands.

Recommended landing order:
1. Phase 0 (one short PR / initial commit on `me-to-markdown`).
2. Phase 1A (mastodon — simplest stateless tool, sets the per-tool pattern).
3. Phase 1B–E in any order, in parallel with Phase 2.
4. Phase 2 (orchestrator).
5. Phase 3 (skill update).
6. Phase 4 (deferred to end of session).

---

## Phase 0: bootstrap the `me-to-markdown` repo

Goal: standard `go-cli-builder` layout in `/home/lmorchard/devel/me-to-markdown/`,
no functional commands yet beyond `version`. Done when `make build && make lint && make test`
all pass on a fresh checkout.

**0.1 — initial commit on `main`.** Use the `lmorchard-agent-skills/go-cli-builder`
skill to scaffold:

- `go.mod` with module `github.com/lmorchard/me-to-markdown`, Go 1.23.
- `cmd/` with `root.go`, `version.go`. Root command sets up Cobra + Viper +
  logrus consistent with the other tools (`--config`, `--verbose`, `--debug`,
  `--log-json` persistent flags). Env-var prefix: `ME_TO_MARKDOWN_`.
- `internal/config/` with the orchestrator's config struct (minimal: just
  `Verbose`, `Debug`, `LogJSON` for now; expanded later).
- `main.go` calling `cmd.Execute()`.
- `Makefile` matching the other tools (`build`, `lint`, `format`, `test`,
  `setup`, `clean`).
- `.github/workflows/ci.yml`, `release.yml`, `rolling-release.yml` copied
  from a tool repo (e.g. mastodon-to-markdown) with names substituted.
- `LICENSE` (MIT, consistent with the other tools).
- `README.md` stub — full README written in Phase 2.
- `.gitignore` matching the other tools.

Commit: `Bootstrap me-to-markdown repo with go-cli-builder layout`.

**0.2 — set up GitHub remote.** Create the empty `lmorchard/me-to-markdown`
repo on GitHub, add as origin, push `main`. (User action; confirm before
proceeding.)

---

## Phase 1: per-tool normalization PRs

Each tool repo gets its own feature branch (`add-export-subcommand`) and PR.
Pattern is the same: a new `cmd/export.go` file adds the `export` subcommand
that translates the canonical `--since/--until/-o` flags into the tool's
existing internal call shape. **No existing code is modified except to expose
the reusable bits** (extract a function from an existing `RunE` if needed).

Each PR also adds: a GitHub issue noting the cleanup follow-up from the spec
(rename legacy flags / consolidate verbs), referenced in the PR description
as "follow-up: #N".

### 1A — mastodon-to-markdown

Path: `/home/lmorchard/devel/mastodon-to-markdown/`. Branch:
`add-export-subcommand`.

**1A.1 — extract fetch logic.** `cmd/fetch.go`'s `RunE` currently parses
flags inline and calls the fetch pipeline. Extract the body (after flag
parsing) into an unexported function like `runMastodonExport(ctx, opts)`
that takes a struct of options instead of reading viper. Existing `fetch`
keeps working — its `RunE` builds the options struct from its (legacy)
flags and calls the extracted function.

**1A.2 — add `cmd/export.go`.** New Cobra command:
- Flags: `--since` (string, required, accepts `YYYY-MM-DD` or Go duration),
  `--until` (string, optional, `YYYY-MM-DD`, defaults to now, end-of-day
  inclusive), `-o`/`--output` (string, default stdout).
- `RunE` parses `--since`/`--until` via a small helper (see 1A.3), builds
  the options struct, calls the extracted function.
- Other behavior toggles (`--exclude-replies`, etc.) are *not* exposed on
  `export` in v1 — orchestrator-facing surface stays minimal. Users who want
  those keep using `fetch`. (If a real need surfaces, add later as opt-in
  flags.)

**1A.3 — duration-or-date `--since` parser.** Add
`internal/timewindow/parse.go` (new package) with one exported function:

```go
// Parse interprets s as either YYYY-MM-DD (local-tz midnight) or a Go duration
// applied relative to ref. Returns (time, error). Empty string is an error.
func Parse(s string, ref time.Time, endOfDay bool) (time.Time, error)
```

Logic: try `time.ParseDuration` first; if it succeeds, return `ref.Add(-d)`
(since durations on `--since` are "ago"). If that fails, try
`time.ParseInLocation("2006-01-02", s, time.Local)`; if `endOfDay`, advance
to 23:59:59.999. Otherwise return an error. **This helper gets copied into
each tool's repo, not shared via module.** Cross-repo Go dependency is more
trouble than three duplicated files.

**1A.4 — tests.** `internal/timewindow/parse_test.go` covering duration,
date, end-of-day, bad input. `cmd/export_test.go` (or extend an existing
test file) covering: flag wiring, missing `--since` errors clearly.

**1A.5 — README update.** Add a short "Orchestrator-friendly `export`
subcommand" section pointing at the canonical flag shape, without rewriting
the existing `fetch` docs.

**1A.6 — open PR.** Title: `Add export subcommand for orchestrator use`.
Body: explain the additive contract, link the cleanup follow-up issue.

**1A.7 — file follow-up issue.** Title: `Cleanup: consolidate fetch and
export, rename --start/--end to --since/--until`. Reference the spec
location and explain the deferral.

### 1B — linkding-to-markdown

Path: `/home/lmorchard/devel/linkding-to-markdown/`. Branch:
`add-export-subcommand`.

Same pattern as 1A:
1. Extract `fetch` pipeline body into a function taking an options struct.
2. Add `cmd/export.go` with `--since/--until/-o`.
3. Copy `internal/timewindow/parse.go` from mastodon-to-markdown (or whichever
   tool lands first — they're identical).
4. Tests + README + PR + follow-up issue.

Linkding-specific notes:
- Legacy `fetch` exposes `--days N`. `export` doesn't expose it — `--since
  168h` is the canonical equivalent.
- `--title` and the include/exclude-notes/tags flags are not on `export` in
  v1.
- Follow-up issue covers deprecating `--days` and consolidating.

### 1C — github-to-markdown

Path: `/home/lmorchard/devel/github-to-markdown/`. Branch:
`add-export-subcommand`.

Slight wrinkle: root command currently has `RunE: runFetch` (root is the
fetch). The new `export` subcommand wraps the same `runFetch` function.

1. Verify `runFetch` is already exported-enough or rename to `runExport` if
   that reads better internally — but **keep** the root command's `RunE`
   pointing at it. Root behavior unchanged.
2. Add `cmd/export.go` registering a subcommand whose `RunE` calls the same
   underlying function. Re-declare the same flag set on `exportCmd.Flags()`
   so they're visible in `--help`.
3. Drop `internal/timewindow/parse.go` in (github's existing `parseWhen`
   already does exactly this — could re-use it. Decide: copy the new helper
   for cross-tool consistency, or keep github's existing helper and adapt
   the others to match it. **Recommendation: copy github's `parseWhen` to
   the other tools rather than the other way around — it already exists,
   already tested.**) → revise step 1A.3 accordingly when this lands.
4. Tests + README + PR + follow-up issue (the issue is whether root command
   should ever stop being the implicit `export`).

### 1D — spotify-to-markdown

Path: `/home/lmorchard/devel/spotify-to-markdown/`. Branch:
`add-export-subcommand`. Most invasive of the five.

**1D.1 — change default DB path.** `internal/config/` and wherever the
default is set: change from `spotify-to-markdown.db` (cwd) to
`$XDG_STATE_HOME/spotify-to-markdown/state.db`, falling back to
`~/.local/state/spotify-to-markdown/state.db`. Match pocketcasts'
implementation exactly. `--database` flag still overrides.

**1D.2 — add windowed query to store.** New method on
`internal/store/store.go` alongside `RecentPlays`:

```go
// PlaysBetween returns plays with played_at in [since, until], newest first.
func (s *Store) PlaysBetween(since, until time.Time) ([]PlayView, error)
```

Plays already have `played_at`. Add an index migration if needed; check
`internal/database/schema.sql` first (probably already indexed).

**1D.3 — add `--since/--until` to `render`.** `cmd/render.go`:
- Add the two flags (optional).
- If `--since` is set, call `PlaysBetween(since, until)` instead of
  `RecentPlays(50)`. Defaults: `--since` empty → existing behavior preserved.
- `--until` defaults to now (end-of-day) when `--since` is set.

**1D.4 — add `cmd/export.go`.** Mirrors `cmd/run.go`'s pattern — call
`fetchCmd.RunE` then `renderCmd.RunE`, but first override viper values for
`--since`, `--until`, `--output` so render picks them up. Or, more cleanly,
factor render's body so export can call it directly with explicit args.

**1D.5 — tests.** `PlaysBetween` (with seeded data), `render` with windowed
flags, `export` end-to-end with a fake DB.

**1D.6 — README update.** Note new default DB path (migration note for
existing users: move file or set `--database`), document `export`.

**1D.7 — PR + follow-up issue.**

### 1E — pocketcasts-to-markdown

Path: `/home/lmorchard/devel/pocketcasts-to-markdown/`. Branch:
`add-export-subcommand`. Simplest of the five.

1. Add `cmd/export.go` that runs `syncCmd.RunE` then `renderCmd.RunE` —
   render already accepts `--since/--until`, so just plumb the flags
   through.
2. Tests confirming export = sync + render with the right flags.
3. README + PR + follow-up issue (the issue: should `sync` and `render`
   stay as separate verbs, or become hidden once `export` covers the
   primary use?).

---

## Phase 2: orchestrator implementation in `me-to-markdown`

Builds on Phase 0 (scaffolding) and any tools from Phase 1 that have
landed. Branch in `me-to-markdown` repo: feature-branch-per-step is
unnecessary — work directly on `main` or one feature branch since this is
the project's own repo and the early commits are all setup.

**2.1 — registry package.** `internal/registry/registry.go`:

```go
type Tool struct {
    Binary string  // "mastodon-to-markdown"
    Slug   string  // "mastodon"  (Binary minus "-to-markdown" suffix)
    Label  string  // "Mastodon"
    Repo   string  // "lmorchard/mastodon-to-markdown"
}

var Tools = []Tool{ /* five entries */ }

func BySlug(slug string) (Tool, bool)
```

Plus a derived helper `func (t Tool) AssetName(goos, goarch string) string`
returning `{binary}-{goos}-{goarch}.tar.gz` (or `.zip` for windows).

**2.2 — binary resolver.** `internal/runner/resolve.go`:

```go
func Resolve(binary string) (path string, source string, err error)
```

Checks `exec.LookPath(binary)` first, then `$XDG_DATA_HOME/me-to-markdown/bin/{binary}`.
Returns the resolved path and a `source` tag (`"path"` or `"managed"`) for
logging. Error if neither exists.

**2.3 — orchestrator config.** `internal/config/config.go`:

```go
type Config struct {
    Verbose    bool
    Debug      bool
    LogJSON    bool
    Since      string   // default --since used when none given to `export`
    Include    []string // optional default
    Exclude    []string // optional default
    OmitErrors bool
}
```

Bind with viper, standard precedence.

**2.4 — `export` command.** `cmd/export.go`:

- Flags: `--since`, `--until`, `-o`/`--output`, `--include`, `--exclude`,
  `--omit-errors`.
- Validation: `--include` and `--exclude` mutually exclusive (in the same
  source — both flags, or both config); error with a clear message if both
  are set at the same level.
- Selection: build the ordered list of tools from registry, filtered by
  include/exclude. Preserve registry order.
- Parallel execution: `errgroup.Group` (with no parallelism cap), one
  goroutine per selected tool:
  1. Resolve binary via runner.Resolve.
  2. `exec.CommandContext(ctx, path, "export", "--since", since, "--until",
     until)` — write stdout to a per-tool buffer, stderr to a separate
     buffer.
  3. On error: if `omit_errors` set, log and record skip; else record an
     error section: `## {Label}\n\n> Error: {tool} export failed: {stderr summary}`.
  4. On success: record the buffer contents as the tool's section body.
- After all goroutines complete: concatenate in registry order with `##
  {Label}` headers separating sections. Write to `-o` or stdout.
- Exit code: 0 if all tools succeeded, 1 if any failed (even with
  `--omit-errors`).
- Default timeout per tool: 5 minutes via `context.WithTimeout`.

**2.5 — `list` command.** `cmd/list.go`. Iterates registry, calls resolver
for each, prints a table:

```
Tool          Status           Path
mastodon      installed (PATH) /home/lmorchard/bin/mastodon-to-markdown
linkding      managed          ~/.local/share/me-to-markdown/bin/linkding-to-markdown
spotify       missing          —
```

Bonus: invoke each found binary's `version` and include the version string.
Defer if it slows the command down materially.

**2.6 — `install` command.** `cmd/install.go`:

- Positional args: zero or more tool slugs. Empty = all tools.
- `--version` per-tool override or `--version latest` (default).
- For each tool to install:
  1. Resolve latest release tag via `GET https://api.github.com/repos/{repo}/releases/latest`
     (or use a provided version).
  2. Compose asset URL:
     `https://github.com/{repo}/releases/download/{tag}/{asset}` where asset
     is from `Tool.AssetName(runtime.GOOS, runtime.GOARCH)`.
  3. Download to a temp file.
  4. Download `checksums.txt` from the same release, verify the asset's
     SHA-256. Fail loudly on mismatch.
  5. Extract the archive (tar.gz / zip).
  6. Move the binary to `$XDG_DATA_HOME/me-to-markdown/bin/{binary}`, mode
     0755.
- Idempotent: re-running `install mastodon` re-downloads and overwrites.
- Progress output: one line per tool with version and result.

**2.7 — `init` command.** `cmd/init.go`. For each tool in the registry,
resolve its binary (if missing, skip with a warning) and `exec.Command(path,
"init", "--force=false")`. After running everyone's init, print a
"next-steps" block:

```
Next steps for interactive setup:
  - spotify-to-markdown auth          (one-time OAuth flow)
  - pocketcasts-to-markdown login --email …  (one-time password login)
  - Add your tokens to mastodon-to-markdown.yaml, linkding-to-markdown.yaml,
    github-to-markdown.yaml
```

**2.8 — README.** Full project README replacing the Phase 0.1 stub.
Sections: what it is, install (build from source / pre-built), quick start,
commands, configuration, contributing/license. Match the tone and depth of
the other tools' READMEs.

**2.9 — end-to-end manual test.** With at least three of the five tools'
PRs landed and binaries built locally, run `me-to-markdown export --since
168h -o /tmp/combined.md`. Verify section headers, ordering, partial-failure
behavior (test by temporarily breaking one tool's config).

---

## Phase 3: `go-cli-builder` skill update

Path: `/home/lmorchard/devel/lmorchard-agent-skills/go-cli-builder/`.
Branch: `add-export-contract`.

**3.1 — read the existing skill.** Understand current scope and where the
export contract fits.

**3.2 — add an "Orchestrator-friendly export contract" section.** Document
- when the contract applies (fetch-and-transform-to-markdown tools only);
- the `export` subcommand verb;
- the `--since/--until/-o` flag shape with the duration-or-date `--since`
  semantics;
- the additive posture (don't break existing subcommands);
- for archive-style tools, that `export` composes existing
  sync/fetch+render.

Include a short reference implementation snippet pulled from one of the
landed tool PRs.

**3.3 — PR in lmorchard-agent-skills repo.**

---

## Phase 4: downstream consumer updates (deferred)

These are *optional* per the spec — they only become necessary if/when we
decide to migrate consumers to the orchestrator. The additive design means
existing skill behavior keeps working.

Defer the actual changes; just create tracking notes in `notes.md` listing
what each consumer would migrate to:

- `decafclaw/contrib/skills/linkding-ingest` → consider calling
  `me-to-markdown export --include linkding` or staying on the direct
  binary call.
- `decafclaw/contrib/skills/mastodon-ingest` → same shape.
- `lmorchard-agent-skills/weeknotes-blog-post-composer` → planned rewrite
  to incorporate three new sources (spotify, pocketcasts, github). Decide
  during that rewrite whether to consume the orchestrator's combined
  output or call binaries directly.

---

## Notes on `/dev-session execute` across repos

The standard `/dev-session execute` flow ("for each step: implement → lint
→ test → commit") assumes a single repo. This session spans six. Practical
adaptation:

- Treat each phase as its own mini-execute run. Switch repos between
  phases.
- Run `make lint` and `make test` in whichever repo the step modifies.
- Commit in the modified repo's branch.
- Each tool PR (1A–1E) is opened separately and merged separately; don't
  squash them across repos.
- The orchestrator phase (2.x) can squash all its commits into one final
  history-clean commit before opening the me-to-markdown PR.
