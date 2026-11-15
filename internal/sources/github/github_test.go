package github

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
)

func TestGitHubRenderMarkdown(t *testing.T) {
	s := &Source{}
	items := []model.Item{
		{
			ID:         "github:123",
			Source:     "github",
			Timestamp:  time.Date(2026, 11, 15, 15, 30, 0, 0, time.UTC),
			URL:        "https://github.com/lmorchard/me-to-markdown/pull/1",
			Title:      "PR #1 opened: Initial single binary refactor",
			Metadata: map[string]any{
				"repo": "lmorchard/me-to-markdown",
				"kind": "PR opened",
			},
		},
	}

	var buf bytes.Buffer
	if err := s.RenderMarkdown(&buf, items); err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "**lmorchard/me-to-markdown** `[PR opened]`") {
		t.Errorf("expected repo and kind header in %s", out)
	}
	if !strings.Contains(out, "[PR #1 opened: Initial single binary refactor](https://github.com/lmorchard/me-to-markdown/pull/1)") {
		t.Errorf("expected link in %s", out)
	}
}
