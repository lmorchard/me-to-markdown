# Notes: env-file, install --from-source, auth

## Session start (2026-05-19)

- Branch: `env-file-and-auth` off `main` (post-merge of PR #1).
- Worktree at `.worktrees/env-file-and-auth/`.
- Phase 0 (env-var normalization across the five sibling tools) shipped first as a precondition — each tool now has a `<TOOL>_<KEY>` env-var convention. Commits on each tool's main:
  - mastodon-to-markdown: `1cfa09d` — drop redundant `mastodon.` key namespace, add `MASTODON_` prefix
  - linkding-to-markdown: `33045ef` — flatten viper keys, add `LINKDING_` prefix + replacer, delete dead config substructs
  - github-to-markdown: `b359ead` — add `GITHUB_` prefix + replacer (was using only explicit `BindEnv` for `GITHUB_TOKEN`)
  - spotify-to-markdown: `368966b` — drop redundant `spotify.` namespace, add `SPOTIFY_` prefix
  - pocketcasts-to-markdown: no change needed; already had clean prefix + replacer
- These commits are the precondition for Phase 1 of this session — the env-file feature only makes sense if every tool's secrets live under a deterministic `<TOOL>_<KEY>` namespace.

## Retrospective

### What shipped

The orchestrator landed on `env-file-and-auth` (seven commits ahead of main, ~1100 LOC net) with:

- `--env-file` (and `env_file:` config): one shared `KEY=VALUE` file is loaded once at startup and merged into every subprocess's environment. Default path `$XDG_CONFIG_HOME/me-to-markdown/env`. New `internal/envfile` package with `Load` + `Upsert` helpers and unit tests.
- `install --from-source [DIR]`: builds each registered tool from a local sibling-repo checkout via `go build`. Bare `--from-source` auto-detects the sibling layout by walking up from the running binary; `--from-source=DIR` overrides. Mutually exclusive with `--version`.
- `auth` subcommand: per-tool authorization with idempotent semantics. Pre-validates each tool, skips with `✓` if creds work, falls through to the prompt+persist flow when needed. Up to 3 retry attempts per tool. `--tool <slug>` for one-at-a-time, `--force` to bypass the skip-if-set check.
- Pocketcasts password env strip when shelling into `login --password-stdin` (the tool refuses both env-var and stdin password at once).

Plus a family-wide normalization push on the five sibling tools as preconditions for the env-file flow:
- env-var prefix + replacer normalization (each tool gets `<TOOL>_<KEY>` env-var convention)
- `validate-auth` subcommand added to each tool — single-line stdout, exit-code-driven, machine-friendly counterpart to `whoami`
- CI workflow injection fix (commit message → bash `[[ ]]` test was unquoted; spotify caught it first when its commit body had unparseable patterns)
- All released as direct pushes to each tool's `main` per the family norm

End-to-end smoke run: `me-to-markdown export --since 168h -o /tmp/me-to-markdown-smoke.md` exited 0 with 2340 lines of properly-structured combined Markdown across all five real data sources.

### Scope drift

The session almost tripled in scope from the original three-phase plan (env-file / install / auth) to a multi-arc family-normalization push. Three significant expansions, each sensible in context:

1. **Env-var normalization** wasn't in the original spec but was a precondition for `--env-file` being useful at all. Initial sizing call of "1-3 lines per tool" turned out wrong once I actually read each tool's `cmd/root.go` — the per-tool key conventions diverged enough that the "right" fix was a small refactor each (flatten redundant tool-name namespace, add `SetEnvPrefix`, add key replacer, update yaml templates, update READMEs, hand-fix some error strings). Surfaced this to Les before doing the bigger work; he confirmed full normalization was the right end state.
2. **`install --from-source`** was Les's suggestion mid-session — "we have them all checked out locally, so we can try using local builds rather than github releases." Slotted in as Phase 2 of the orchestrator branch. Useful for dev iteration; pays for itself on every subsequent rebuild-and-test cycle.
3. **Idempotent auth + `validate-auth` contract** came out of the smoke-run failure mode. Pocketcasts login failed on the first walk; Les flagged two follow-ups in one message ("is this writing creds as it goes? we probably should test each set as they're entered"). That grew into the per-tool `validate-auth` subcommand as a family contract — 5 more small commits, but it cleaned up the orchestrator's validation surface from "shell `export --since 1m`" to "shell `validate-auth`". Better contract, faster validation, easier to extend.

In hindsight all three additions made the final shape stronger; nothing felt like dead weight at session end.

### Surprises

- **Pocketcasts `login --password-stdin` refuses to run if `POCKETCASTS_PASSWORD` is also set in env or yaml.** Discovered during the first smoke run. Fix on the orchestrator side: strip the env var when shelling into `login`. Wrote up the failure mode and the partial-credentials state ("Mastodon/Linkding/GitHub/Spotify already persisted — you don't need to start over, just retry pocketcasts") so the user wasn't blocked on understanding what state they were in.
- **CI workflow injection vuln** in the shared `ci.yml` boilerplate from go-cli-builder. Commit messages with backticks / parens / multi-line content broke the unquoted `if [[ "${{ github.event.head_commit.message }}" == "[noci]"* ]]` test. Only spotify hit it in this session (its commit body had the right pattern of metachars to break `[[`); mastodon's identical-issue commit happened to evaluate clean. Fix went to all 5 sibling repos + the skill template.
- **cobra's `NoOptDefVal` footgun:** with `--from-source` configured to have a "no-arg default" of `auto`, the form `--from-source <path>` parses `<path>` as a positional arg, not the flag value. Only `--from-source=<path>` works. Documented in the commit + help text rather than restructuring the flag. Minor wart, not worth a redesign.
- **Mastodon already had `whoami`** with rich output (username, follower counts). Kept it as the human-facing identity command; added `validate-auth` as the machine-friendly companion. Slight redundancy but right audience-of-one for each.

### Workflow friction

- The dev-session skill ran loose this session — `spec.md` / `plan.md` were written manually mid-session rather than as a strict precondition for execute. That worked well for a session this fluid (scope shifted multiple times as failure modes surfaced). Strict spec/plan/execute would have either been re-written multiple times or would have constrained the right-thing-to-do moves.
- `TaskCreate` carried the cross-repo work effectively. Twelve+ tasks across two sub-arcs (env-var normalization, validate-auth contract) — easy to track in/out of progress, easy for the user to see what remained.
- Test-build-vet after each unit caught issues fast. The smoke-run-as-discovery pattern (per Les: "the smoke run is a shakedown intended to surface exactly these rough edges") was the real validation surface; unit tests alone would have missed the pocketcasts env conflict and the prompt UX issues.
- Direct push to main worked well per the family norm. The dev-session worktree pattern was useful for keeping the orchestrator's multi-commit arc separate from the per-tool drive-by commits.

### Misses

- **Should have surveyed each tool's `cmd/root.go` before sizing the normalization work.** The "1-3 lines per tool" estimate I gave Les up front was wrong by an order of magnitude. Should have read first, estimated second.
- **The CI injection vuln was pre-existing**; nobody had hit it because no prior commit message had the right pattern of metachars. Could have caught it during a paranoia pass on the shared workflow boilerplate, but only really discoverable via the failure that actually triggered it. Not actionable in hindsight; logged for the skill maintainer (Les) for future template hardening.
- **Cobra `NoOptDefVal` should have been smoke-tested with both forms** before committing — I tested `--from-source` (bare) and `--from-source=DIR` (works), missed `--from-source DIR` (footgun) until a follow-up smoke. Caught and documented but exposed a thin spot in my own validation discipline for flag UX.

### Memory candidates

- **Feedback memory:** "When estimating per-tool work across the family, survey each tool's actual code before sizing — boilerplate divergence is real." Tied to this session's underestimate of the env-var normalization work.
- **Project memory:** The go-cli-builder skill at `~/.claude/.../lmorchard-agent-skills/go-cli-builder/` is the *source* of the family's CI / release / Makefile / root.go boilerplate. Updates to family conventions (env-prefix style, ci.yml injection fix, validate-auth scaffold) belong upstream there for new tools to inherit.
- **Reference memory:** Family rolling-releases use the `latest` tag (now non-prerelease since the workflow fix earlier in this session). `me-to-markdown install` works against the published releases as well as `--from-source` locally.

### Skill candidates

- **go-cli-builder:** add `validate-auth` to the family contract documented in the skill. New fetch-and-export tools should ship a `validate-auth` subcommand stub by default.
- **go-cli-builder:** the `ci.yml.template` fix has already been staged in the skill repo (not yet committed by Les); committing it alone closes the loop.
- **dev-session retro:** acknowledged in this session that the dev-session pattern works well even with light spec/plan when the work is fluid. Probably not worth changing the retro template, but worth noting the lighter-touch mode is legitimate.

### Open question for Les

- The validate-auth contract is now codified across all 5 tools. Are there other family-wide subcommand contracts you want to formalize while we're in this mode? Candidates that come to mind: `<tool> doctor` (env + config + cached state diagnostic), `<tool> init --force` semantics consistency, or some sort of `<tool> version --json` for the orchestrator's `list` to parse instead of regexing the human-readable string.
