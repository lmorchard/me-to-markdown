package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	_ "github.com/lmorchard/me-to-markdown/internal/sources/linkding"
	_ "github.com/lmorchard/me-to-markdown/internal/sources/mastodon"
	"github.com/lmorchard/me-to-markdown/internal/store"
)

func TestExportFormats(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	ts := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	items := []model.Item{
		{
			ID:        "linkding:1",
			Source:    "linkding",
			Timestamp: ts,
			URL:       "https://example.com/test",
			Title:     "Test Bookmark",
			Summary:   "Description of test",
			Tags:      []string{"test"},
		},
	}
	if err := st.UpsertItems(context.Background(), items); err != nil {
		t.Fatalf("upsert items: %v", err)
	}

	src, _ := sources.Get("linkding")
	data := []SourceExportData{
		{Source: src, Items: items},
	}

	t.Run("export markdown file", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "weeknotes.md")
		if err := exportMarkdown(outPath, "", data, false); err != nil {
			t.Fatalf("exportMarkdown: %v", err)
		}
		b, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.Contains(string(b), "## Linkding") || !strings.Contains(string(b), "[Test Bookmark](https://example.com/test)") {
			t.Errorf("unexpected content: %s", string(b))
		}
	})

	t.Run("export per-source directory", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "sources")
		if err := exportMarkdown("", outDir, data, false); err != nil {
			t.Fatalf("exportMarkdown dir: %v", err)
		}
		b, err := os.ReadFile(filepath.Join(outDir, "linkding.md"))
		if err != nil {
			t.Fatalf("read linkding.md: %v", err)
		}
		if !strings.Contains(string(b), "# Linkding") {
			t.Errorf("unexpected content: %s", string(b))
		}
	})

	t.Run("export json", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "export.json")
		if err := exportJSON(outPath, "", data); err != nil {
			t.Fatalf("exportJSON: %v", err)
		}
		b, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var parsed map[string][]model.Item
		if err := json.Unmarshal(b, &parsed); err != nil {
			t.Fatalf("decode json: %v", err)
		}
		if len(parsed["linkding"]) != 1 || parsed["linkding"][0].Title != "Test Bookmark" {
			t.Errorf("unexpected json: %+v", parsed)
		}
	})

	t.Run("export jsonl", func(t *testing.T) {
		outPath := filepath.Join(t.TempDir(), "export.jsonl")
		if err := exportJSONL(outPath, "", data); err != nil {
			t.Fatalf("exportJSONL: %v", err)
		}
		b, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var it model.Item
		if err := json.Unmarshal(b, &it); err != nil {
			t.Fatalf("decode jsonl line: %v", err)
		}
		if it.ID != "linkding:1" {
			t.Errorf("unexpected jsonl item: %+v", it)
		}
	})
}
