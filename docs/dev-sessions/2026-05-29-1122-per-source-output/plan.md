# Per-source output mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional `--output-dir` mode to `me-to-markdown export` that writes one Markdown file per data source instead of concatenating them.

**Architecture:** The parallel tool-running stage is untouched. Output assembly is refactored so a single `renderToolSection` helper produces each tool's block (header + body, or error block), parameterized by header level. `runExport` then dispatches to either the existing concat path or a new `writePerToolFiles` path based on whether `--output-dir` is set.

**Tech Stack:** Go 1.21+, Cobra, Viper, logrus. Tests use the standard `testing` package and `t.TempDir()`.

---

## File Structure

- `cmd/export.go` (modify): lift the anonymous result struct to a named `toolResult` type; add `renderToolSection` and `writePerToolFiles`; add the `--output-dir` flag, mutual-exclusion check, and dispatch branch; refactor the concat loop to use `renderToolSection`.
- `cmd/export_test.go` (create): first test file in the `cmd` package. Unit tests for `renderToolSection` and `writePerToolFiles`.
- `README.md` (modify): document the new flag and mode.

Background facts the implementer needs:
- `registry.Tool` has fields `Binary`, `Slug`, `Label`, `Repo`.
- `errSummary(err error, stderr []byte) string` already exists in `cmd/export.go` and returns the first non-empty stderr line, falling back to `err.Error()`.
- `humanBytes(n int) string` already exists in `cmd/export.go`.
- `loggerLike` interface (already in `cmd/export.go`) has `Debugf`, `Infof`, `Errorf`.
- `runTool` currently returns an anonymous struct `{ stdout []byte; stderr []byte; err error }`; `runExport` stores these in a local `type result struct {...}` slice. Both will be replaced by the named `toolResult`.

---

## Task 1: Introduce `toolResult` type and `renderToolSection` helper

Behavior-preserving refactor: name the result struct, extract section rendering into a shared helper, and route the existing concat loop through it. No user-visible change yet.

**Files:**
- Modify: `cmd/export.go`
- Test: `cmd/export_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `cmd/export_test.go`:

```go
package cmd

import (
	"strings"
	"testing"

	"github.com/lmorchard/me-to-markdown/internal/registry"
)

var testTool = registry.Tool{
	Binary: "mastodon-to-markdown",
	Slug:   "mastodon",
	Label:  "Mastodon",
	Repo:   "lmorchard/mastodon-to-markdown",
}

func TestRenderToolSection_Success(t *testing.T) {
	r := toolResult{stdout: []byte("hello world")}
	got, skip := renderToolSection(testTool, r, "#", false)
	if skip {
		t.Fatal("did not expect skip for a successful tool")
	}
	want := "# Mastodon\n\nhello world\n\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderToolSection_HeaderLevel(t *testing.T) {
	r := toolResult{stdout: []byte("body\n")}
	got, _ := renderToolSection(testTool, r, "##", false)
	if !strings.HasPrefix(string(got), "## Mastodon\n\n") {
		t.Fatalf("expected H2 header, got %q", got)
	}
}

func TestRenderToolSection_EmptyOutput(t *testing.T) {
	r := toolResult{stdout: nil}
	got, skip := renderToolSection(testTool, r, "#", false)
	if skip {
		t.Fatal("did not expect skip for empty-but-successful output")
	}
	want := "# Mastodon\n\n\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderToolSection_Error(t *testing.T) {
	r := toolResult{stderr: []byte("boom: token expired\n"), err: errTest}
	got, skip := renderToolSection(testTool, r, "#", false)
	if skip {
		t.Fatal("did not expect skip when omitErrors is false")
	}
	want := "# Mastodon\n\n> Error: mastodon-to-markdown export failed: boom: token expired\n\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderToolSection_ErrorOmitted(t *testing.T) {
	r := toolResult{stderr: []byte("boom\n"), err: errTest}
	got, skip := renderToolSection(testTool, r, "#", true)
	if !skip {
		t.Fatal("expected skip when omitErrors is true and tool failed")
	}
	if got != nil {
		t.Fatalf("expected nil bytes on skip, got %q", got)
	}
}

var errTest = &simpleErr{"exit status 1"}

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestRenderToolSection -v`
Expected: FAIL — compile error, `undefined: toolResult` and `undefined: renderToolSection`.

- [ ] **Step 3: Add the `toolResult` type and `renderToolSection` helper**

In `cmd/export.go`, add near the top of the file (after the `defaultToolTimeout` const) a named type:

```go
// toolResult holds one tool subprocess's captured output and run error.
type toolResult struct {
	stdout []byte
	stderr []byte
	err    error
}
```

Then add the rendering helper (place it just below `runExport`, before `formatUntil`):

```go
// renderToolSection renders one tool's contribution to the output: a header
// at the given level (e.g. "#" or "##") followed by the tool's stdout, or an
// error block if the run failed. The second return value reports whether the
// section should be skipped entirely (a failed tool while omitErrors is set).
func renderToolSection(t registry.Tool, r toolResult, header string, omitErrors bool) ([]byte, bool) {
	var buf bytes.Buffer
	if r.err != nil {
		if omitErrors {
			return nil, true
		}
		fmt.Fprintf(&buf, "%s %s\n\n> Error: %s export failed: %s\n\n",
			header, t.Label, t.Binary, errSummary(r.err, r.stderr))
		return buf.Bytes(), false
	}
	fmt.Fprintf(&buf, "%s %s\n\n", header, t.Label)
	buf.Write(r.stdout)
	if !bytes.HasSuffix(r.stdout, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	return buf.Bytes(), false
}
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `go test ./cmd/ -run TestRenderToolSection -v`
Expected: PASS (5 subtests/functions pass).

- [ ] **Step 5: Refactor `runTool` and the concat loop to use the new type and helper**

In `cmd/export.go`:

Change `runExport`'s local results slice from the anonymous-struct-backed local `type result struct {...}` to the named type. Replace this block:

```go
	// Each tool's section ends up at the same index as its entry in selected.
	type result struct {
		stdout []byte
		stderr []byte
		err    error
	}
	results := make([]result, len(selected))
```

with:

```go
	// Each tool's section ends up at the same index as its entry in selected.
	results := make([]toolResult, len(selected))
```

Change `runTool`'s signature from the anonymous named return:

```go
func runTool(parentCtx context.Context, log loggerLike, t registry.Tool, since, until string) (out struct {
	stdout []byte
	stderr []byte
	err    error
}) {
```

to:

```go
func runTool(parentCtx context.Context, log loggerLike, t registry.Tool, since, until string) (out toolResult) {
```

Replace the concat loop body. Change this:

```go
	for i, t := range selected {
		r := results[i]
		if r.err != nil {
			anyFailed = true
			log.Errorf("%s export failed: %v", t.Binary, r.err)
			if omitErrors {
				continue
			}
			fmt.Fprintf(&combined, "## %s\n\n> Error: %s export failed: %s\n\n",
				t.Label, t.Binary, errSummary(r.err, r.stderr))
			continue
		}
		fmt.Fprintf(&combined, "## %s\n\n", t.Label)
		combined.Write(r.stdout)
		if !bytes.HasSuffix(r.stdout, []byte("\n")) {
			combined.WriteByte('\n')
		}
		combined.WriteByte('\n')
	}
```

to:

```go
	for i, t := range selected {
		r := results[i]
		if r.err != nil {
			anyFailed = true
			log.Errorf("%s export failed: %v", t.Binary, r.err)
		}
		section, skip := renderToolSection(t, r, "##", omitErrors)
		if skip {
			continue
		}
		combined.Write(section)
	}
```

- [ ] **Step 6: Run the full build and test suite**

Run: `make build && make test`
Expected: build succeeds; all tests pass (including `internal/envfile` and the new `cmd` tests). Behavior of concat mode is unchanged.

- [ ] **Step 7: Commit**

```bash
git add cmd/export.go cmd/export_test.go
git commit -m "refactor: extract renderToolSection and name toolResult type"
```

---

## Task 2: Add `--output-dir` flag and per-file writing

Adds the user-visible feature: a new flag, mutual exclusion with `-o`, and the `writePerToolFiles` path.

**Files:**
- Modify: `cmd/export.go`
- Test: `cmd/export_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/export_test.go`:

```go
import additions: add "os", "path/filepath" to the import block.
```

(Edit the existing import block to include `"os"` and `"path/filepath"` alongside `"strings"` and `"testing"`.)

Add a no-op logger and the tests:

```go
type nopLogger struct{}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}

func TestWritePerToolFiles_WritesFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out") // does not exist yet
	selected := []registry.Tool{
		{Binary: "mastodon-to-markdown", Slug: "mastodon", Label: "Mastodon"},
		{Binary: "github-to-markdown", Slug: "github", Label: "GitHub"},
	}
	results := []toolResult{
		{stdout: []byte("toots here\n")},
		{stdout: []byte("commits here\n")},
	}

	anyFailed, err := writePerToolFiles(nopLogger{}, dir, selected, results, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anyFailed {
		t.Fatal("did not expect anyFailed")
	}

	got, err := os.ReadFile(filepath.Join(dir, "mastodon.md"))
	if err != nil {
		t.Fatalf("reading mastodon.md: %v", err)
	}
	if string(got) != "# Mastodon\n\ntoots here\n\n" {
		t.Fatalf("mastodon.md = %q", got)
	}
	got, err = os.ReadFile(filepath.Join(dir, "github.md"))
	if err != nil {
		t.Fatalf("reading github.md: %v", err)
	}
	if string(got) != "# GitHub\n\ncommits here\n\n" {
		t.Fatalf("github.md = %q", got)
	}
}

func TestWritePerToolFiles_ErrorFileAndExitFlag(t *testing.T) {
	dir := t.TempDir()
	selected := []registry.Tool{
		{Binary: "mastodon-to-markdown", Slug: "mastodon", Label: "Mastodon"},
	}
	results := []toolResult{
		{stderr: []byte("token expired\n"), err: errTest},
	}

	anyFailed, err := writePerToolFiles(nopLogger{}, dir, selected, results, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !anyFailed {
		t.Fatal("expected anyFailed to be true")
	}
	got, err := os.ReadFile(filepath.Join(dir, "mastodon.md"))
	if err != nil {
		t.Fatalf("reading mastodon.md: %v", err)
	}
	want := "# Mastodon\n\n> Error: mastodon-to-markdown export failed: token expired\n\n"
	if string(got) != want {
		t.Fatalf("mastodon.md = %q, want %q", got, want)
	}
}

func TestWritePerToolFiles_OmitErrorsSkipsFile(t *testing.T) {
	dir := t.TempDir()
	selected := []registry.Tool{
		{Binary: "mastodon-to-markdown", Slug: "mastodon", Label: "Mastodon"},
	}
	results := []toolResult{
		{stderr: []byte("token expired\n"), err: errTest},
	}

	anyFailed, err := writePerToolFiles(nopLogger{}, dir, selected, results, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !anyFailed {
		t.Fatal("expected anyFailed to be true even when the file is skipped")
	}
	if _, err := os.Stat(filepath.Join(dir, "mastodon.md")); !os.IsNotExist(err) {
		t.Fatalf("expected mastodon.md to be absent, stat err = %v", err)
	}
}

func TestWritePerToolFiles_LeavesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "spotify.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatalf("seeding stale file: %v", err)
	}
	selected := []registry.Tool{
		{Binary: "mastodon-to-markdown", Slug: "mastodon", Label: "Mastodon"},
	}
	results := []toolResult{{stdout: []byte("toots\n")}}

	if _, err := writePerToolFiles(nopLogger{}, dir, selected, results, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("reading stale file: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("stale file was modified: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestWritePerToolFiles -v`
Expected: FAIL — compile error, `undefined: writePerToolFiles`.

- [ ] **Step 3: Implement `writePerToolFiles`**

In `cmd/export.go`, add `"path/filepath"` to the import block. Then add the function (place it just below `renderToolSection`):

```go
// writePerToolFiles writes one Markdown file per selected tool into dir,
// named "{slug}.md" with an H1 "# {Label}" header. The directory is created
// if missing; unrelated pre-existing files are left untouched. It returns
// whether any tool failed (so the caller can set a non-zero exit code).
func writePerToolFiles(log loggerLike, dir string, selected []registry.Tool, results []toolResult, omitErrors bool) (bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("creating output dir %s: %w", dir, err)
	}

	anyFailed := false
	filesWritten := 0
	totalBytes := 0
	for i, t := range selected {
		r := results[i]
		if r.err != nil {
			anyFailed = true
			log.Errorf("%s export failed: %v", t.Binary, r.err)
		}
		section, skip := renderToolSection(t, r, "#", omitErrors)
		if skip {
			continue
		}
		path := filepath.Join(dir, t.Slug+".md")
		if err := os.WriteFile(path, section, 0o644); err != nil {
			return anyFailed, fmt.Errorf("writing %s: %w", path, err)
		}
		log.Infof("Wrote %s to %s", humanBytes(len(section)), path)
		filesWritten++
		totalBytes += len(section)
	}
	log.Infof("Wrote %d file(s), %s total to %s", filesWritten, humanBytes(totalBytes), dir)
	return anyFailed, nil
}
```

- [ ] **Step 4: Run the per-file tests to verify they pass**

Run: `go test ./cmd/ -run TestWritePerToolFiles -v`
Expected: PASS (all four functions).

- [ ] **Step 5: Wire up the flag and dispatch branch**

In `cmd/export.go`'s `init()`, add the flag alongside the existing `--output`:

```go
	exportCmd.Flags().StringP("output-dir", "d", "", "Write one file per tool into this directory instead of concatenating. Mutually exclusive with --output.")
```

In `runExport`, just after `output, _ := cmd.Flags().GetString("output")`, add:

```go
	outputDir, _ := cmd.Flags().GetString("output-dir")
	if output != "" && outputDir != "" {
		return errors.New("--output and --output-dir are mutually exclusive")
	}
```

Then, after `wg.Wait()` and the "All N tool(s) finished" log line, insert the per-file branch before the concat block:

```go
	omitErrors := c.OmitErrors

	if outputDir != "" {
		anyFailed, err := writePerToolFiles(log, outputDir, selected, results, omitErrors)
		if err != nil {
			return err
		}
		if anyFailed {
			return fmt.Errorf("one or more tools failed (see error files and stderr)")
		}
		return nil
	}
```

Note: the concat block below already declares `omitErrors := c.OmitErrors`. Remove that now-duplicate declaration from the concat block (the variable is declared once above the branch). The concat block keeps using `omitErrors` as before.

- [ ] **Step 6: Run the full build and test suite**

Run: `make build && make test`
Expected: build succeeds; all tests pass.

- [ ] **Step 7: Manual smoke test**

Run:
```bash
mkdir -p /tmp/m2m-smoke
./me-to-markdown export --since 168h --include mastodon,github --output-dir /tmp/m2m-smoke
ls -la /tmp/m2m-smoke
head -5 /tmp/m2m-smoke/*.md
```
Expected: `mastodon.md` and `github.md` exist, each starting with `# {Label}`. (Requires the tools installed/authed; if not available locally, the files will contain error blocks — still a valid smoke of the write path.)

Also verify mutual exclusion:
```bash
./me-to-markdown export --since 168h -o out.md --output-dir /tmp/m2m-smoke
```
Expected: error `--output and --output-dir are mutually exclusive`, non-zero exit.

- [ ] **Step 8: Commit**

```bash
git add cmd/export.go cmd/export_test.go
git commit -m "feat: add --output-dir mode writing one file per tool"
```

---

## Task 3: Document the new mode in the README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the export flags table**

In `README.md`, in the `### export flags` table, add a row immediately after the `-o`, `--output` row:

```markdown
| `-d`, `--output-dir` | Write one `{slug}.md` file per tool into this directory instead of concatenating. Mutually exclusive with `-o`. | unset |
```

- [ ] **Step 2: Add a note in the "What it does" section**

In `README.md`, after the paragraph that ends "...the orchestrator's exit code is non-zero if any tool failed.", add:

```markdown

Pass `--output-dir <dir>` instead of `-o` to write one Markdown file per
source (`mastodon.md`, `github.md`, …), each with a `# {Label}` heading,
rather than one concatenated document. The directory is created if needed;
existing unrelated files are left in place.
```

- [ ] **Step 3: Verify the docs render sensibly**

Run: `git diff README.md`
Expected: the new table row and paragraph appear, Markdown table alignment intact.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document --output-dir per-source export mode"
```

---

## Self-Review Notes

- **Spec coverage:** interface (`--output-dir`, `-d`, mutual exclusion) → Task 2 steps 5; file naming/content (`# {Label}` + body, empty case) → Task 1 helper + Task 2 tests; errors + `--omit-errors` → Task 1 helper + Task 2 tests; directory handling (`MkdirAll`, overwrite, leave unrelated) → Task 2 `writePerToolFiles` + test; logging → Task 2 `writePerToolFiles`; docs → Task 3; testing → Tasks 1 & 2. All covered.
- **Type consistency:** `toolResult` (fields `stdout`, `stderr`, `err`), `renderToolSection(registry.Tool, toolResult, string, bool) ([]byte, bool)`, and `writePerToolFiles(loggerLike, string, []registry.Tool, []toolResult, bool) (bool, error)` are used consistently across tasks and tests.
- **No placeholders:** every code step shows complete code.
