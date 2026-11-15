package mastodon

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
)

func TestMastodonRenderMarkdown(t *testing.T) {
	s := &Source{}
	items := []model.Item{
		{
			ID:        "mastodon:456",
			Source:    "mastodon",
			Timestamp: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
			URL:       "https://masto.example/@lmorchard/456",
			Summary:   "Tinkering with homelab arcade cabinet today.",
			Media: []model.MediaAttachment{
				{
					Type:        model.MediaImage,
					URL:         "https://masto.example/media/1.png",
					Description: "Arcade cabinet photo",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := s.RenderMarkdown(&buf, items); err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Tinkering with homelab arcade cabinet today.") {
		t.Errorf("expected summary in %s", out)
	}
	if !strings.Contains(out, "Media: [image](https://masto.example/media/1.png) - Arcade cabinet photo") {
		t.Errorf("expected media in %s", out)
	}
}
