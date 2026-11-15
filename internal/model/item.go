package model

import (
	"encoding/json"
	"time"
)

// MediaType represents the kind of media attached to an item.
type MediaType string

const (
	MediaImage     MediaType = "image"
	MediaVideo     MediaType = "video"
	MediaAudio     MediaType = "audio"
	MediaThumbnail MediaType = "thumbnail"
)

// MediaAttachment holds structured media references (images, video clips, thumbs).
type MediaAttachment struct {
	Type        MediaType `json:"type"`
	URL         string    `json:"url"`
	PreviewURL  string    `json:"preview_url,omitempty"`
	Description string    `json:"description,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
}

// Item is the canonical normalized representation of a single personal data exhaust entry.
type Item struct {
	// ID is the composite unique key: "{source}:{external_id}".
	ID string `json:"id"`

	// Source identifies the provider ("mastodon", "linkding", "github", "spotify", "youtube", "pocketcasts").
	Source string `json:"source"`

	// ExternalID is the native identifier in the origin system.
	ExternalID string `json:"external_id"`

	// Timestamp is the canonical event time in UTC.
	Timestamp time.Time `json:"timestamp"`

	// URL is the permalink or canonical web link.
	URL string `json:"url,omitempty"`

	// Title is the short title, track name, bookmark title, commit summary, or video title.
	Title string `json:"title,omitempty"`

	// Summary is an excerpt, toot body, bookmark description, or short note.
	Summary string `json:"summary,omitempty"`

	// Content is the full body, transcript, or markdown content if distinct from summary.
	Content string `json:"content,omitempty"`

	// AuthorName is the creator name (artist, committer, tooter, channel).
	AuthorName string `json:"author_name,omitempty"`

	// AuthorURL is a link to the author/channel profile.
	AuthorURL string `json:"author_url,omitempty"`

	// Tags is a list of categorization tags, hashtags, or topics.
	Tags []string `json:"tags,omitempty"`

	// Media holds any associated images, videos, audio clips, or thumbnails.
	Media []MediaAttachment `json:"media,omitempty"`

	// Metadata holds source-specific arbitrary key-values (e.g. repo name, duration, stars).
	Metadata map[string]any `json:"metadata,omitempty"`

	// RawPayload is the verbatim raw JSON from the origin API response, preserved for future re-processing.
	RawPayload json.RawMessage `json:"raw_payload,omitempty"`
}
