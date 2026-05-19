# Notes: me-to-markdown orchestrator + tool normalization

## Session bootstrap (2026-05-18)

- `me-to-markdown` started as a bare `git init` — no commits, no remote, no
  `origin/main` to rebase from. Branching/rebase steps from `/dev-session start`
  were skipped accordingly. Branching decisions deferred until there's actual
  code to commit.
- This is a multi-repo session: orchestrator implementation in
  `me-to-markdown`, plus a separate normalization PR in each of five tool
  repos, plus three downstream consumer fixes near the end. Each repo gets its
  own branch/PR; this session directory is the shared planning artifact.
- Design discussion before kicking off the session settled on:
  - shell-out orchestration (not import-as-library)
  - upstream normalization first, orchestrator second
  - universal `export --since --until -o` verb as the orchestrator contract
  - skill updates (`linkding-ingest`, `mastodon-ingest`,
    `weeknotes-blog-post-composer`) deferred to end of session

## Phase 1 + 2 + 3 execution (2026-05-18)

All six PRs opened in one pass after brainstorm/plan, in this order:

| Repo | PR | Follow-up issue |
|---|---|---|
| mastodon-to-markdown | [#2](https://github.com/lmorchard/mastodon-to-markdown/pull/2) | [#1](https://github.com/lmorchard/mastodon-to-markdown/issues/1) |
| linkding-to-markdown | [#2](https://github.com/lmorchard/linkding-to-markdown/pull/2) | [#1](https://github.com/lmorchard/linkding-to-markdown/issues/1) |
| github-to-markdown | [#2](https://github.com/lmorchard/github-to-markdown/pull/2) | [#1](https://github.com/lmorchard/github-to-markdown/issues/1) |
| pocketcasts-to-markdown | [#2](https://github.com/lmorchard/pocketcasts-to-markdown/pull/2) | [#1](https://github.com/lmorchard/pocketcasts-to-markdown/issues/1) |
| spotify-to-markdown | [#2](https://github.com/lmorchard/spotify-to-markdown/pull/2) | [#1](https://github.com/lmorchard/spotify-to-markdown/issues/1) |
| me-to-markdown (orchestrator) | [#1](https://github.com/lmorchard/me-to-markdown/pull/1) | — |
| lmorchard-agent-skills (skill update) | [#1](https://github.com/lmorchard/lmorchard-agent-skills/pull/1) | — |

### Decisions that diverged from the plan

- **Backward-compat posture moved from "clean break" to "additive only"**
  during the brainstorm self-review (Q5/G1 discussion). Recognized that
  `export` as a new subcommand means legacy `fetch`/`sync`/`render` can
  stay completely unchanged. No flag renames. No downstream skill fixes
  forced. Tracked the cleanup work as per-repo follow-up issues.

- **Reused github-to-markdown's `parseWhen` semantics in the new
  `internal/timewindow/parse.go`** rather than the plan's "copy
  github's helper." Wrote a new helper that also accepts Go durations
  (which `parseWhen` doesn't), duplicated to each tool's repo.

- **Spotify gained a `-` → stdout output convention on `render`** so the
  orchestrator can capture its output via subprocess stdout. Small
  additive behavior; flagged in the PR.

- **End-to-end smoke test of the orchestrator** against the dev builds
  (all five on `$PATH`, none configured except pocketcasts) rendered
  four clean error sections plus a working Pocket Casts section with
  ~100 real episodes. Validated: parallel exec, registry-order
  concatenation, error-section rendering, exit-code propagation.

### Phase 4 status

Downstream consumer updates (`decafclaw/contrib/skills/linkding-ingest`,
`decafclaw/contrib/skills/mastodon-ingest`,
`lmorchard-agent-skills/weeknotes-blog-post-composer`) remain
**deferred** per spec. Because the per-tool work was additive, none of
these consumers are forced to change. The weeknotes composer's planned
rewrite (to incorporate the three new sources) will be the natural
moment to decide whether downstream skills should consume the
orchestrator's combined output or keep calling individual binaries.
