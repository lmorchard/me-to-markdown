package youtube

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
)

func TestYouTubeRenderMarkdown(t *testing.T) {
	s := &Source{}
	items := []model.Item{
		{
			ID:        "youtube:dQw4w9WgXcQ",
			Source:    "youtube",
			Timestamp: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC),
			Title:     "Never Gonna Give You Up",
			URL:       "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			Metadata: map[string]any{
				"video_id":      "dQw4w9WgXcQ",
				"channel_title": "Rick Astley",
			},
		},
	}

	var buf bytes.Buffer
	if err := s.RenderMarkdown(&buf, items); err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "**[Never Gonna Give You Up](https://www.youtube.com/watch?v=dQw4w9WgXcQ)** by Rick Astley (`dQw4w9WgXcQ`)") {
		t.Errorf("unexpected output: %s", out)
	}
}
