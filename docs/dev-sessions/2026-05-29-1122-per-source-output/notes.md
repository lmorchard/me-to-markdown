# Notes: per-source output mode

## Summary

Added an optional `--output-dir` / `-d` flag to `me-to-markdown export`. When
set, the orchestrator writes one `{slug}.md` file per data source (each with a
`# {Label}` H1 header) into that directory instead of concatenating every
tool's output into a single document. The flag is mutually exclusive with
`-o` / `--output`.

## What shipped

- `cmd/export.go`:
  - Named the previously-anonymous result struct as a package-level
    `toolResult` type.
  - Extracted section rendering into `renderToolSection(t, r, header,
    omitErrors)`, shared by both output modes (header level `#` for per-file,
    `##` for concat).
  - Added `writePerToolFiles(...)`: `MkdirAll` the target dir, write each
    selected tool's `{slug}.md`, leave unrelated files untouched, return
    whether any tool failed.
  - Added the `--output-dir` flag, a mutual-exclusion guard with `--output`,
    and a dispatch branch in `runExport`.
- `cmd/export_test.go`: first test file in the `cmd` package — 5 cases for
  `renderToolSection`, 4 for `writePerToolFiles`.
- `README.md`: documented the flag (flags table + "What it does" note).

## Behavior decisions (from brainstorming)

- Trigger: dedicated `--output-dir` flag (not `--split` or magic `-o`
  detection).
- File naming/content: `{slug}.md` with a `# {Label}` H1 header.
- Failed tools: write `{slug}.md` with an error block, mirroring concat mode;
  `--omit-errors` skips the file; exit code still non-zero.
- Directory: created if missing; existing unrelated files left in place
  (no clearing/sync — explicitly out of scope).

## Process

Built via subagent-driven development on a `per-source-output` branch:
implementer + spec-compliance + code-quality review per task, plus a final
holistic review. Merged to `main` locally (`--no-ff`).

## Worth remembering

- The Task 1 spec'd helper had an off-by-one trailing-newline bug for the
  empty-but-successful stdout case (would emit 4 newlines, failing its own
  test). The implementer caught it and added a `len(r.stdout) > 0` guard so
  the empty case renders `# {Label}\n\n\n`. This is technically a one-newline
  change to concat mode's degenerate empty-output case — beneficial, and the
  only deviation from "don't touch concat output."
- A subagent's `make format` run reformatted two unrelated files
  (`cmd/init.go`, `internal/envfile/envfile_test.go`) — pre-existing gofumpt
  drift. Those changes were discarded at merge time to keep the feature clean;
  the drift remains for a separate cleanup.

## Follow-ups

- `main` is ahead of `origin/main` and not pushed (left to Les).
- To use the new flag from an installed binary, rebuild/reinstall the
  `me-to-markdown` binary (the smoke test used a fresh local `./me-to-markdown`).
- Optional: a separate commit to apply the pending gofumpt reformat to
  `cmd/init.go` and `internal/envfile/envfile_test.go`.
