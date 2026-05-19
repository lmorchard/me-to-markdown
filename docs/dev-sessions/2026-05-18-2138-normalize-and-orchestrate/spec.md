# Spec: me-to-markdown orchestrator + tool normalization

_Status: brainstorm complete 2026-05-18, including self-review pass; ready for `/dev-session plan`._

## One-line summary

Build a Go CLI that orchestrates the existing `*-to-markdown` tools in parallel
over a given time window and concatenates their output into a single Markdown
document. As a prerequisite, normalize the CLI surface of each tool so the
orchestrator has a uniform contract to call.

## Scope of this session

- New repo: `me-to-markdown` — the orchestrator itself.
- Five upstream tool repos, one normalization PR each:
  - `mastodon-to-markdown`
  - `linkding-to-markdown`
  - `github-to-markdown`
  - `spotify-to-markdown`
  - `pocketcasts-to-markdown`
- One agent-skill update:
  - `lmorchard-agent-skills/go-cli-builder` — bake the `export` contract into
    the skill so future tools are born compliant. **Scoped narrowly**: the
    contract applies only to tools whose purpose is to fetch and transform
    data from a source into a Markdown document. Other Go CLIs the skill
    might be used to build (servers, web apps, generic utilities) are not
    in scope for the contract.
- Three downstream consumer updates (deferred to end of session):
  - `decafclaw/contrib/skills/linkding-ingest`
  - `decafclaw/contrib/skills/mastodon-ingest`
  - `lmorchard-agent-skills/weeknotes-blog-post-composer`

## Decisions

### Canonical contract (Q1, 2026-05-18)

Every tool exposes an `export` subcommand the orchestrator calls:

```
{tool} export --since <date|duration> --until <date> -o <file>
```

- `--since` accepts `YYYY-MM-DD` or a Go duration string (`168h`). Required.
- `--until` accepts `YYYY-MM-DD`, optional, defaults to "now," interpreted as
  end-of-day inclusive (matches `github-to-markdown`'s behavior today).
- `-o` / `--output` writes to file; default stdout.
- Existing subcommands (`fetch`, `sync`, `render`, `auth`, `login`) stay where
  they are. `export` is added alongside, not as a replacement.
- For archive tools (`spotify`, `pocketcasts`), `export` internally does
  `sync && render --since X --until Y`. For stateless tools, it does the
  equivalent in one pass. The orchestrator does not need to distinguish.

Rejected alternatives:
- Making the contract the root command (no subcommand). Archive tools already
  need `sync`/`render` as named subcommands, so a dedicated `export` keeps the
  contract discoverable in `--help`.

### Registry shape (Q6, 2026-05-18)

Hardcoded Go struct in the orchestrator. Three fields per tool:

```go
type Tool struct {
    Binary string  // "mastodon-to-markdown"
    Label  string  // "Mastodon"        — used in section headers
    Repo   string  // "lmorchard/mastodon-to-markdown" — used by `install`
}
```

Release asset name derives from `Binary` by convention
(`{binary}-{goos}-{goarch}.{tar.gz|zip}`, matching every existing tool's
release workflow). Adding a tool is three lines + a rebuild.

External user-editable YAML is deferred until there's a concrete reason
(third-party tool registration, version pinning beyond what `install
--version` provides, etc.). Migration when needed: read YAML at startup,
merge over hardcoded defaults.

### Backward compatibility / per-tool PR scope (Q5, 2026-05-18)

**Additive-only.** Each tool's normalization PR only *adds* surface area; it
does not rename, remove, or modify any existing flag or subcommand.

Per-tool work:
- `mastodon-to-markdown`: add `export` subcommand. Legacy `fetch`,
  `--start/--end`, etc. stay as-is.
- `linkding-to-markdown`: add `export` subcommand. Legacy `fetch` and `--days`
  stay as-is.
- `github-to-markdown`: add `export` subcommand wrapping the existing
  root-command behavior. Root command stays as-is.
- `spotify-to-markdown`: add `export` subcommand. Add optional `--since/--until`
  to existing `render` (default-off preserves current behavior).
- `pocketcasts-to-markdown`: add `export` subcommand. `render` already has
  `--since/--until`; no other changes.

Consequence: downstream consumers (`decafclaw/contrib/skills/linkding-ingest`,
`decafclaw/contrib/skills/mastodon-ingest`,
`lmorchard-agent-skills/weeknotes-blog-post-composer`) do **not** require
updates to ship the orchestrator. Migrating them to call the orchestrator
becomes an optional later cleanup, not a forced fix. The weeknotes composer
rework is already planned because of the new sources, but it isn't a blocker.

Cleanup follow-ups (deferred, tracked as GitHub issues on each tool repo as
its normalization PR is opened):
- Rename mastodon's `--start/--end` → `--since/--until` on `fetch`, or
  deprecate `fetch` in favor of `export`.
- Drop or alias linkding's `--days`.
- Decide whether `github-to-markdown`'s root command should print help once
  `export` exists, or stay as a backward-compatible shorthand.
- Decide whether spotify's `run` should remain alongside `export`.

### Tool binary discovery (Q4, 2026-05-18)

Resolution order when the orchestrator needs to invoke a tool binary:

1. `exec.LookPath({binary})` — anything on `$PATH` wins.
2. `$XDG_DATA_HOME/me-to-markdown/bin/{binary}` (i.e. `~/.local/share/me-to-markdown/bin/` by default) — the directory `me-to-markdown install` populates.
3. Clear error pointing at `me-to-markdown install` if nothing is found.

Rationale:
- Existing-user case: tools already on `$PATH` from local builds → just works, no `install` step required.
- Dev case: a fresh local build on `$PATH` shadows any managed binary → no flag needed to test in-progress changes via the orchestrator.
- New-user case: `install` downloads everything to the managed dir; fallback finds it.

Escape hatches deferred until needed: per-tool path override in the registry config, a `--use-managed` flag, or a `--bin-path` per invocation.

### Output format (Q3, 2026-05-18)

- Single concatenated Markdown file with section dividers, in the order the
  tools appear in the orchestrator's registry.
- Section headers use friendly labels declared in the registry
  (e.g. `## Mastodon`, `## GitHub`), not the binary names.
- `-o file.md` writes there; absent `-o`, writes to stdout.
- No per-tool / split-output mode in v1. The weeknotes blog post composer
  (`lmorchard-agent-skills/weeknotes-blog-post-composer`) will be reworked
  after the orchestrator lands — that rework will absorb the new sources and
  decide its own consumption shape (calling tools directly, splitting on the
  section headers, or asking us to add a split mode later).

### Orchestrator CLI surface (G3, 2026-05-18)

Main user-facing command mirrors the per-tool contract verb:

```
me-to-markdown export --since <date|duration> --until <date> -o <file>
```

Full sketched surface:
- `export` — main command. Runs every selected tool's `export` in parallel,
  concatenates output in registry order. `-o` writes to file; default stdout.
- `install` — download release binaries for each tool into the managed dir.
  Default latest tag, per-tool `--version` override available.
- `init` — run each tool's `init` to scaffold per-tool config files, then
  print next-step instructions for the tools that need interactive auth
  (`spotify-to-markdown auth`, `pocketcasts-to-markdown login`).
- `list` — show registered tools and their status (found on `$PATH`, found in
  managed dir, missing) along with installed version where determinable.
- `version` — standard.

### Tool selection (G2, 2026-05-18)

Both flags and config keys for include/exclude:

- CLI: `--include mastodon,github` and `--exclude spotify` on `export`. Both
  accept comma-separated tool names matching `Tool.Binary` minus the
  `-to-markdown` suffix (so `mastodon`, `spotify`, etc.). Defaults: all tools
  in registry order.
- Config: `include:` and `exclude:` lists in the orchestrator's config file
  with the same semantics.
- Precedence: flags override config (standard Viper precedence).
- `--include` and `--exclude` are mutually exclusive on the same invocation.
  If both are given on the same level (flags or config), error out with a
  clear message.

### Partial-failure behavior (G1, 2026-05-18)

Default: render an error section in place of the failed tool's output.

```
## Spotify

> Error: spotify-to-markdown export failed: <stderr captured or summary>
```

Rationale: matches unattended cron use — the output document itself surfaces
what's broken without needing to scrape logs. Other tools still run; the
overall exit code reflects whether any tool errored (non-zero if so), so cron
mail still flags it.

Flag (and corresponding config key) to suppress error sections entirely when
the user prefers omission over an error block: `--omit-errors` /
`omit_errors: true`. The tool is silently skipped, error still logged to
stderr, exit code still reflects the failure.

A "fail the whole run on any tool error" mode is *not* added in v1. If
someone needs that, they can check the exit code and discard the partial
output.

### State/DB location for archive tools (Q2, 2026-05-18)

- `pocketcasts-to-markdown` already defaults to
  `$XDG_STATE_HOME/pocketcasts-to-markdown/state.db`. Keep it.
- `spotify-to-markdown` currently defaults to `./spotify-to-markdown.db`.
  Change to `$XDG_STATE_HOME/spotify-to-markdown/state.db` to match.
- `--database` stays as an escape hatch on both tools.
- The orchestrator does **not** centralize state under its own directory. Each
  tool keeps its own XDG default; the orchestrator stays out of state
  management entirely. This preserves the tools' usefulness as standalone
  binaries and avoids coupling them to the orchestrator's filesystem layout.

### `go-cli-builder` skill update (G4, 2026-05-18)

Add the `export` subcommand contract to the
`lmorchard-agent-skills/go-cli-builder` skill so future fetch-and-export tools
are born conforming. Scoped narrowly to tools whose purpose is "fetch data
from a source, transform to Markdown, emit." Other Go CLIs the skill might
build (servers, web apps, generic utilities) are unaffected.

The skill update should specify: the `export` verb, the `--since/--until/-o`
flag shape with the duration-or-date `--since` parser, the convention that
`export` may compose existing subcommands internally (sync+render for archive
tools), and the additive-only posture toward existing commands.

## Implementation details deferred to `/dev-session plan`

- Per-tool subprocess timeout (sensible default, probably 5min) and global deadline.
- Concurrency: goroutine per tool via `errgroup`, no parallelism cap in v1.
- Working directory: each subprocess inherits the orchestrator's cwd. Tool
  configs are looked up wherever each tool normally looks (cwd + XDG_CONFIG_HOME
  for pocketcasts, cwd for others). No `--config` passing.
- Orchestrator's own config: minimal. Default `--since` window, include/exclude,
  managed-bin-dir override, `omit_errors`. Decide during planning.
- `install` version pinning: default to latest tagged release per tool, allow
  `--version` per-tool override. Verify against `checksums.txt`.
- Output ordering when subprocesses complete in different orders: collect into
  per-tool buffers, then concatenate in registry order. Deterministic.
- Repo bootstrap for `me-to-markdown` itself: standard `go-cli-builder` layout
  (`cmd/`, `internal/`, Makefile, CI/release workflows, MIT license, README).
- Error section format and how stderr from the failed subprocess is captured
  and trimmed for display.
