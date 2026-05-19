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
  - clean break on renamed flags (revisit if brainstorm changes the call)
  - skill updates (`linkding-ingest`, `mastodon-ingest`,
    `weeknotes-blog-post-composer`) deferred to end of session
