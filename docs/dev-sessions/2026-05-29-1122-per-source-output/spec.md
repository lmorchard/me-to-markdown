# Spec: per-source output mode for `me-to-markdown export`

## Goal

Add an optional mode to the `export` command that writes one Markdown file
per data source instead of concatenating every tool's output into a single
document.

## Motivation

The orchestrator currently runs each `*-to-markdown` tool in parallel and
concatenates the results into one combined document (with `## {Label}`
section headers), written to `-o` or stdout. Some downstream workflows want
the per-source output kept in separate files instead — easier to feed into
tooling that expects one file per source, or to diff/track sources
independently.

## Interface

- New flag on `export`: `--output-dir` (short `-d`), a directory path.
- Mutually exclusive with `-o` / `--output`; setting both is an error.
- Flag-only — no config key, matching how `--output` is handled today.
- The parallel tool-running stage is unchanged. Only output assembly branches
  on whether `--output-dir` is set.

## Behavior

### File naming and content

- For each selected tool, write `{dir}/{slug}.md`.
- File content: `# {Label}\n\n` followed by the tool's raw export output,
  newline-normalized exactly as concat mode does (ensure trailing newline).
- The H1 header makes each file self-describing when opened standalone.
- A tool that succeeds but produces empty output still gets a file containing
  just the `# {Label}` header (consistent with concat mode emitting the
  header regardless).

### Errors

- A failed tool writes `{dir}/{slug}.md` containing
  `# {Label}\n\n> Error: {binary} export failed: {summary}\n\n`, mirroring the
  error section format used in concat mode (header level aside).
- `--omit-errors` skips writing the file for a failed tool instead.
- The process exit code is still non-zero if any tool failed, identical to
  concat mode.

### Directory handling

- `os.MkdirAll` the target path if it does not exist.
- Overwrite each selected tool's `slug.md`.
- Leave unrelated pre-existing files (e.g. `slug.md` for a deselected tool)
  untouched — no clearing of the directory.

### Logging

- One `Wrote {bytes} to {dir}/{slug}.md` line per file written.
- A closing summary line: number of files written and total bytes.

## Code shape

In `cmd/export.go`:

- Factor the per-tool section rendering into a small helper that takes the
  tool, its result, the header level (`#` for files, `##` for concat), and the
  `omitErrors` flag, returning the rendered bytes plus a flag indicating the
  section should be skipped. Both concat and per-file modes share identical
  body/error formatting and differ only in header depth and destination.
- `runExport` resolves `--output` vs `--output-dir` (mutual-exclusion check
  near the existing include/exclude check), runs tools unchanged, then
  dispatches to the existing concat path or a new `writePerToolFiles` path.

## Testing

- The `cmd` package currently has no tests (only `internal/envfile` does);
  this work adds the first `cmd/export_test.go`.
- Add unit tests for:
  - the shared render helper (success body, empty body, error block,
    `omitErrors` skip behavior, header level).
  - the per-file writing path against a temp dir (files created, contents
    correct, `MkdirAll` of a missing dir, unrelated files left intact).
- Keep tests at the same altitude as existing ones.

## Docs

- README: add `--output-dir` to the `export` flags table.
- README: short note in the "What it does" / quick-start area describing the
  per-source mode.

## Out of scope

- Clearing/syncing stale files in the output dir.
- A config key for the output directory.
- Changing concat-mode behavior or output format.
