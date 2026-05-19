# Notes: youtube-to-markdown

## Session start (2026-05-19)

- New sibling tool in the `*-to-markdown` family.
- YouTube watch history removed from public Data API ~2016-17; using Liked Videos (LL playlist) as the proxy signal. Like-as-deliberate-bookmark is arguably better for weeknotes than passive views anyway.
- Spec covers: stateful SQLite cache, rich data (title + channel + URL + liked-at + description + duration + view count), day-grouped Markdown render, family-standard subcommand surface incl. `validate-auth`.
- Plan: 16 tasks across scaffold → foundations → API client → OAuth → subcommands → render → polish → orchestrator registry → smoke run.

## Execution log

All 16 tasks executed via the `subagent-driven-development` skill — fresh implementer per task with two-stage review (spec compliance, code quality) inline in this session. ~20 commits on `youtube-to-markdown` main, plus 2 on `me-to-markdown` main (registry entry + `authYouTube` handler).

Smoke run sequence:
1. Created Google Cloud OAuth 2.0 client (Desktop type)
2. `youtube-to-markdown auth` — browser PKCE flow completed
3. `youtube-to-markdown fetch` — initial backfill, 917 likes cached
4. `youtube-to-markdown validate-auth` — green
5. `me-to-markdown auth --tool youtube --force` — orchestrator-driven auth via shared env file
6. `me-to-markdown export --since 168h` — all six tools in combined output, exit 0
7. `youtube-to-markdown render --since 8760h` — rendered the past year (13 day-headers, 13 video bullets with full formatter output)

## Retrospective

### What shipped

A new sibling tool `youtube-to-markdown` (separate GitHub repo, ~3000 LOC net, all on `main`) plus one orchestrator change in `me-to-markdown` (registry entry + auth handler). The tool implements the full family contract — `export`, `validate-auth`, day-grouped render — and is now picked up by `me-to-markdown` automatically.

### Scope drift

Mostly stayed inside the plan. Three real-bug detours during the smoke run, each small and contained:

- **Google requires `client_secret` with PKCE.** Cribbed OAuth from spotify-to-markdown which uses true PKCE (no secret); Google treats the secret as a client-identification marker that must still be sent. Fix: added `ClientSecret` through config → youtubeauth.Config → both POST bodies (exchange + refresh) → the orchestrator's `authYouTube` prompt → README + init template + comments. Larger ripple than expected because the misleading "no client_secret needed" wording was duplicated in several spots.
- **`exit status 2` panic in `export`/`run` wrappers.** `fetchCmd.RunE(fetchCmd, nil)` and `renderCmd.RunE(renderCmd, nil)` bypass cobra's `Execute`, so the inner commands' `cmd.Context()` returned nil; `context.WithTimeout(nil, ...)` panicked. Fix: `SetContext` on each inner cmd before invoking. Latent bug — would have hit any standalone `youtube-to-markdown export` that got past the no-creds precondition; only surfaced under the orchestrator because that's the only path that ever has valid creds AND a captured stderr.
- **`fetch` status to stdout** polluted the orchestrator's section content. Moved to stderr.
- **Multi-line description newlines** broke the Markdown blockquote in render. Fix: `strings.Fields` in `DescriptionExcerpt`.

Each was caught + fixed inside the same session, no scope leak.

### Surprises

- **`go-cli-builder` scaffold quality.** The Task 1 review flagged broken template substitution: `cmd/init.go` had unresolved `{{.ModuleName}}` placeholders + mangled raw-string artifacts; `internal/templates/default.md` shipped Go-template escape sequences (`{{"{{"}}` etc.) instead of literal `{{` delimiters; `go.mod` / `main.go` / `cmd/root.go` had `github.com/yourusername/...` placeholders the substitution missed. The implementer hand-patched the broken emit to make the scaffold compile. Worth tracking these upstream in the skill template.
- **Stale-managed-binary footgun.** Local `make build` updated the standalone binary; orchestrator kept using the previous `install --from-source` build. Got debugged by MD5-comparing the two binaries when behavior diverged. Filed as [me-to-markdown#5](https://github.com/lmorchard/me-to-markdown/issues/5).
- **Cobra `init()` ordering** for `runCmd.Flags().AddFlagSet(renderCmd.Flags())`. Source files initialize alphabetically, so `export.go` runs before `render.go` — renderCmd's flags weren't populated yet. Workaround: move the AddFlagSet wiring into `run.go`'s init (post-render alphabetically) and wire both `runCmd` and `exportCmd` from there. Documented inline.
- **YouTube `LL` playlist `snippet.publishedAt`** turned out to be the actual like-at timestamp in this dataset, not the video's publish date — both spread across many years, none of them coincident. Good outcome; some old reports suggested the API quirk could go either way. The spec called the right shot.

### Workflow friction

- **Subagent-driven-development worked well at scale.** 16 tasks across two repos, dispatched sequentially with two-stage review on each. Total wall time was substantial but parallel subagent dispatches kept review feedback tight. The "one bash subagent per implementation task + one for spec review + one for code quality" pattern catches more than self-review alone, especially the Task 1 critical template bugs.
- **The "trust but verify" pattern** caught real things: the spec-compliance reviewer for Task 6 (OAuth) read every diff vs. the spotify reference; the code-quality reviewer for Task 1 noticed the embedded template was broken; the final cross-task reviewer caught the spec-vs-actual discrepancy in token storage (kv table vs. dedicated auth_tokens). Cheap insurance.
- **Reviewer dispatch overhead** is real for trivial mechanical tasks (Task 4: duration parser was paste-mechanical, the dispatch+review round-trip felt heavier than the work). I used a combined spec+quality review for those — that's a useful shortcut to keep in the toolbox.
- **Plan code blocks paid for themselves repeatedly.** Most tasks were "paste the code, build, commit." Implementers rarely had to invent anything — they just had to translate the plan's stubs into the project's actual API surface (e.g., when the family timewindow signature differed from what the plan sketched).

### Misses

- **Should have flagged Google's `client_secret` requirement at brainstorm time.** It's a known difference between Spotify's and Google's OAuth implementations; would have been caught by a 5-minute API-doc check before committing to the "mirror spotify-to-markdown" approach. Fixing it mid-smoke-run cost an extra ~30 min of focused rework.
- **`go-cli-builder` template bugs landed in the scaffold's first commit** and were patched up implicitly in Task 1. They should be filed upstream to the skill — otherwise the next new tool hits the same potholes. Noted as a follow-up to do after this session.
- **The `1_000 → "1.0K views"` test case in the plan vs. `trimZero` behavior** — caught + adjusted at implementation time, but writing the plan I should have noticed the inconsistency. Plan reviews would help (an extra eye on the plan before execution).

### Memory candidates

- **Feedback memory:** Google OAuth 2.0 token endpoint requires `client_secret` even when the client is using PKCE — unlike Spotify's pure-PKCE flow. Worth saving because this same gotcha will recur if we add any other Google-authenticated tool (Drive, Calendar, etc.).
- **Project memory:** When new tools are scaffolded via `go-cli-builder`, the template emission has known bugs (`cmd/init.go` placeholder substitution, `internal/templates/default.md` escape artifacts, scaffold yaml comments staying stale). Saves rediscovery; supports the issue we should file upstream.
- **Project memory:** The orchestrator's `runner.Resolve` checks `$PATH` first, then the managed bin dir. Sibling repos aren't typically on `$PATH`, so dev iteration's `make build` produces a binary that the orchestrator NEVER USES. Always re-run `me-to-markdown install --from-source` after editing a sibling repo before testing via the orchestrator. (This duplicates the content of [issue #5](https://github.com/lmorchard/me-to-markdown/issues/5) but as a fast-access memory.)

### Skill candidates

- **go-cli-builder:** the template emit bugs in Task 1 should be filed upstream. Specific finds:
  - `cmd/init.go` template uses `{{.ModuleName}}` / `{{.ProjectName}}` style but the scaffold script only does literal `{{KEY}}` substitution. Mismatch produces unsubstituted placeholders + mangled raw-string artifacts.
  - `internal/templates/default.md` ships `{{"{{"}}` / `{{"}}"}}` escape sequences that aren't valid as a baseline Go text/template; they roundtrip through the embed correctly but produce a non-parseable file when written to disk by `init`.
  - `github.com/yourusername/...` placeholders in `go.mod` / `main.go` / `cmd/root.go` aren't substituted; user has to fix manually.
  - Docker-tag references in commented-out `release.yml` / `rolling-release.yml` stanzas similarly retain `yourusername/...`.

- **go-cli-builder:** consider shipping a `validate-auth` subcommand stub alongside the standard `version`/`init` so new family members get the auth contract for free. We added this manually to each tool last session and to youtube-to-markdown this session; the next new tool will need it too.

### Open question for Les

The Google-vs-Spotify OAuth deviation (`client_secret` requirement under PKCE) is one of several places where provider-specific behavior leaked into what we'd hoped was a generic "OAuth2 PKCE flow" pattern. If you end up adding more Google-authenticated tools to the family (Drive, Calendar, Photos), is there an `internal/googleauth` shared package worth extracting at some point, or do you prefer each tool to keep its own auth code (matching current siblings)? The current shape — each tool ships its own `internal/<tool>auth` package — preserves independence at the cost of mild duplication; a shared `googleauth` could reduce that but introduces inter-repo dependency that the family's been carefully avoiding so far.
