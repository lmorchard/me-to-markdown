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
