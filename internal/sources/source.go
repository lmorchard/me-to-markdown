package sources

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/store"
)

// SyncOptions configures a sync operation.
type SyncOptions struct {
	Since       time.Time
	Until       time.Time
	FullReindex bool
}

// SyncResult captures statistics from a source sync.
type SyncResult struct {
	Source   string `json:"source"`
	Fetched  int    `json:"fetched"`
	Upserted int    `json:"upserted"`
	Cursor   string `json:"cursor,omitempty"`
}

// Source is the unified interface implemented by all exhaust providers.
type Source interface {
	// Name returns the slug for the source (e.g. "linkding", "mastodon").
	Name() string

	// Label returns the human-readable display label (e.g. "Linkding", "Mastodon").
	Label() string

	// ValidateAuth checks whether required credentials exist and are accepted by the upstream API.
	ValidateAuth(ctx context.Context, env map[string]string) error

	// Sync fetches new/updated data from upstream and stores it in the local Store.
	Sync(ctx context.Context, s *store.Store, env map[string]string, opts SyncOptions) (*SyncResult, error)

	// RenderMarkdown formats a slice of items from this source into Markdown.
	RenderMarkdown(w io.Writer, items []model.Item) error
}

var registry = make(map[string]Source)
var order []string

// Register adds a source to the global registry.
func Register(s Source) {
	name := s.Name()
	if _, exists := registry[name]; !exists {
		order = append(order, name)
	}
	registry[name] = s
}

// Get returns the registered source for the given slug, if found.
func Get(name string) (Source, bool) {
	s, ok := registry[name]
	return s, ok
}

// All returns all registered sources in registration order.
func All() []Source {
	var list []Source
	for _, name := range order {
		if s, ok := registry[name]; ok {
			list = append(list, s)
		}
	}
	return list
}

// Slugs returns the slugs of all registered sources.
func Slugs() []string {
	return append([]string{}, order...)
}

// BySlug returns the source or an error if unknown.
func BySlug(slug string) (Source, error) {
	s, ok := Get(slug)
	if !ok {
		return nil, fmt.Errorf("unknown source %q", slug)
	}
	return s, nil
}
