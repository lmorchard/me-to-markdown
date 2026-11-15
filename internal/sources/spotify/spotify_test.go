package spotify

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
)

func TestSpotifyRenderMarkdown(t *testing.T) {
	s := &Source{}
	items := []model.Item{
		{
			ID:        "spotify:789",
			Source:    "spotify",
			Timestamp: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
			Title:     "Quiet Life",
			URL:       "https://open.spotify.com/track/123",
			Metadata: map[string]any{
				"artists": "Japan",
				"album":   "Quiet Life",
			},
		},
	}

	var buf bytes.Buffer
	if err := s.RenderMarkdown(&buf, items); err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "**[Quiet Life](https://open.spotify.com/track/123)** by Japan (on _Quiet Life_)") {
		t.Errorf("unexpected output: %s", out)
	}
}
