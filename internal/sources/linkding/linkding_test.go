package linkding

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/lmorchard/me-to-markdown/internal/store"
)

func TestLinkdingSource(t *testing.T) {
	s := &Source{}
	if s.Name() != "linkding" {
		t.Errorf("expected linkding, got %s", s.Name())
	}

	t.Run("render markdown", func(t *testing.T) {
		ts := time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)
		items := []model.Item{
			{
				ID:        "linkding:1",
				Source:    "linkding",
				Title:     "DecafClaw Project",
				URL:       "https://github.com/lmorchard/decafclaw",
				Summary:   "Minimal AI agent built in Python",
				Content:   "Interesting notes here",
				Tags:      []string{"ai", "python"},
				Timestamp: ts,
			},
		}

		var buf bytes.Buffer
		if err := s.RenderMarkdown(&buf, items); err != nil {
			t.Fatalf("RenderMarkdown failed: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "[DecafClaw Project](https://github.com/lmorchard/decafclaw)") {
			t.Errorf("missing markdown link: %s", out)
		}
		if !strings.Contains(out, "`[ai, python]`") {
			t.Errorf("missing tags: %s", out)
		}
		if !strings.Contains(out, "> Interesting notes here") {
			t.Errorf("missing notes quote: %s", out)
		}
	})

	t.Run("sync against mock linkding server", func(t *testing.T) {
		ts := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
		mockBookmarks := bookmarksResponse{
			Count: 1,
			Results: []bookmarkPayload{
				{
					ID:          42,
					URL:         "https://example.com/test",
					Title:       "Test Bookmark",
					Description: "A bookmark test",
					TagNames:    []string{"test"},
					DateAdded:   ts,
				},
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/bookmarks/" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(mockBookmarks)
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		st, err := store.Open(":memory:")
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		defer func() { _ = st.Close() }()

		env := map[string]string{
			"LINKDING_URL":   server.URL,
			"LINKDING_TOKEN": "mock-token",
		}

		res, err := s.Sync(context.Background(), st, env, sources.SyncOptions{})
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if res.Fetched != 1 || res.Upserted != 1 {
			t.Errorf("expected 1 fetched/upserted, got %+v", res)
		}

		stored, err := st.GetItems(context.Background(), store.ItemFilter{Sources: []string{"linkding"}})
		if err != nil {
			t.Fatalf("GetItems: %v", err)
		}
		if len(stored) != 1 || stored[0].Title != "Test Bookmark" {
			t.Errorf("stored items mismatch: %+v", stored)
		}
	})
}
