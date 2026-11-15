package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
)

func TestStoreOperations(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory store: %v", err)
	}
	defer func() { _ = s.Close() }()

	t1 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	items := []model.Item{
		{
			ID:         "mastodon:101",
			Source:     "mastodon",
			ExternalID: "101",
			Timestamp:  t1,
			URL:        "https://masto.example/@user/101",
			Title:      "First Toot",
			Summary:    "Hello fediverse!",
			Tags:       []string{"fediverse", "hello"},
			RawPayload: json.RawMessage(`{"id":"101","body":"Hello fediverse!"}`),
		},
		{
			ID:         "linkding:202",
			Source:     "linkding",
			ExternalID: "202",
			Timestamp:  t2,
			URL:        "https://example.com/article",
			Title:      "Great Article",
			Summary:    "A very interesting read",
			Tags:       []string{"reading", "tech"},
			RawPayload: json.RawMessage(`{"id":202,"title":"Great Article"}`),
		},
		{
			ID:         "github:303",
			Source:     "github",
			ExternalID: "303",
			Timestamp:  t3,
			URL:        "https://github.com/lmorchard/repo/pull/1",
			Title:      "Feature PR",
			Tags:       []string{"code"},
			RawPayload: json.RawMessage(`{"id":303}`),
		},
	}

	if err := s.UpsertItems(ctx, items); err != nil {
		t.Fatalf("UpsertItems failed: %v", err)
	}

	t.Run("query all items", func(t *testing.T) {
		res, err := s.GetItems(ctx, ItemFilter{})
		if err != nil {
			t.Fatalf("GetItems failed: %v", err)
		}
		if len(res) != 3 {
			t.Fatalf("expected 3 items, got %d", len(res))
		}
		// Default order is DESC (newest first)
		if res[0].ID != "github:303" {
			t.Errorf("expected newest item first, got %s", res[0].ID)
		}
	})

	t.Run("query by source", func(t *testing.T) {
		res, err := s.GetItems(ctx, ItemFilter{Sources: []string{"linkding"}})
		if err != nil {
			t.Fatalf("GetItems failed: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 item, got %d", len(res))
		}
		if res[0].Title != "Great Article" {
			t.Errorf("expected title 'Great Article', got %s", res[0].Title)
		}
		if len(res[0].Tags) != 2 || res[0].Tags[0] != "reading" {
			t.Errorf("expected tags [reading, tech], got %v", res[0].Tags)
		}
		if string(res[0].RawPayload) != `{"id":202,"title":"Great Article"}` {
			t.Errorf("unexpected raw payload: %s", string(res[0].RawPayload))
		}
	})

	t.Run("query by time window", func(t *testing.T) {
		res, err := s.GetItems(ctx, ItemFilter{
			Since: t1.Add(30 * time.Minute),
			Until: t2.Add(30 * time.Minute),
		})
		if err != nil {
			t.Fatalf("GetItems failed: %v", err)
		}
		if len(res) != 1 || res[0].ID != "linkding:202" {
			t.Fatalf("expected only linkding:202, got %v", res)
		}
	})

	t.Run("upsert updates existing item without duplication", func(t *testing.T) {
		updated := model.Item{
			ID:         "mastodon:101",
			Source:     "mastodon",
			ExternalID: "101",
			Timestamp:  t1,
			Title:      "Updated Toot",
			Summary:    "Updated content",
			Tags:       []string{"updated"},
			RawPayload: json.RawMessage(`{"id":"101","updated":true}`),
		}
		if err := s.UpsertItems(ctx, []model.Item{updated}); err != nil {
			t.Fatalf("UpsertItems update failed: %v", err)
		}

		res, err := s.GetItems(ctx, ItemFilter{Sources: []string{"mastodon"}})
		if err != nil {
			t.Fatalf("GetItems failed: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 item after update, got %d", len(res))
		}
		if res[0].Title != "Updated Toot" || res[0].Summary != "Updated content" {
			t.Errorf("item was not properly updated: %+v", res[0])
		}
	})

	t.Run("sync cursor tracking", func(t *testing.T) {
		cursor, err := s.GetSyncCursor(ctx, "mastodon")
		if err != nil {
			t.Fatalf("GetSyncCursor failed: %v", err)
		}
		if cursor != nil {
			t.Fatalf("expected nil cursor initially, got %+v", cursor)
		}

		sc := model.SyncCursor{
			Source:       "mastodon",
			Cursor:       "max_id_999",
			LastSyncedAt: t2,
			Status:       "ok",
		}
		if err := s.SetSyncCursor(ctx, sc); err != nil {
			t.Fatalf("SetSyncCursor failed: %v", err)
		}

		cursor, err = s.GetSyncCursor(ctx, "mastodon")
		if err != nil {
			t.Fatalf("GetSyncCursor failed: %v", err)
		}
		if cursor == nil || cursor.Cursor != "max_id_999" || cursor.Status != "ok" {
			t.Fatalf("unexpected cursor: %+v", cursor)
		}
	})

	t.Run("get stats", func(t *testing.T) {
		stats, err := s.GetStats(ctx)
		if err != nil {
			t.Fatalf("GetStats failed: %v", err)
		}
		if len(stats) < 3 {
			t.Fatalf("expected at least 3 source stats, got %d", len(stats))
		}
	})
}
