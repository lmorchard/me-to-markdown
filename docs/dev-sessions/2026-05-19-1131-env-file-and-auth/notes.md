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
