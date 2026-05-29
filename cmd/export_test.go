package cmd

import (
	"errors"
	"os"
	"path/filepath"
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

var errTest = errors.New("exit status 1")

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
