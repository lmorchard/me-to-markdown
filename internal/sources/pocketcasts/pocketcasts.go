package pocketcasts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/lmorchard/me-to-markdown/internal/store"
)

func init() {
	sources.Register(&Source{})
}

type Source struct{}

func (s *Source) Name() string  { return "pocketcasts" }
func (s *Source) Label() string { return "Pocket Casts" }

const defaultBaseURL = "https://api.pocketcasts.com"

type Episode struct {
	UUID         string `json:"uuid"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	PodcastTitle string `json:"podcastTitle"`
	PodcastUUID  string `json:"podcastUuid"`
	Published    string `json:"published"`
	PlayedUpTo   int    `json:"playedUpTo"`
	Duration     int    `json:"duration"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Scope    string `json:"scope"`
}

type loginResponse struct {
	Token string `json:"token"`
}

type episodeListResponse struct {
	Episodes []Episode `json:"episodes"`
}

func (s *Source) ValidateAuth(ctx context.Context, env map[string]string) error {
	email := env["POCKETCASTS_EMAIL"]
	password := env["POCKETCASTS_PASSWORD"]
	if email == "" || password == "" {
		return fmt.Errorf("POCKETCASTS_EMAIL and POCKETCASTS_PASSWORD are required")
	}

	body := loginRequest{Email: email, Password: password, Scope: "webplayer"}
	buf, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", defaultBaseURL+"/user/login", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("pocketcasts login failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pocketcasts login failed with status %d", resp.StatusCode)
	}
	return nil
}

func (s *Source) getToken(ctx context.Context, st *store.Store, env map[string]string) (string, error) {
	// Try cached token in store first
	if st != nil {
		tok, err := st.GetAuthToken(ctx, s.Name())
		if err == nil && tok != nil && tok.AccessToken != "" && tok.ExpiresAt.After(time.Now()) {
			return tok.AccessToken, nil
		}
	}

	email := env["POCKETCASTS_EMAIL"]
	password := env["POCKETCASTS_PASSWORD"]
	if email == "" || password == "" {
		return "", fmt.Errorf("pocketcasts credentials missing (POCKETCASTS_EMAIL and POCKETCASTS_PASSWORD required)")
	}

	body := loginRequest{Email: email, Password: password, Scope: "webplayer"}
	buf, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", defaultBaseURL+"/user/login", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	var lResp loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lResp); err != nil {
		return "", err
	}

	if st != nil {
		_ = st.SetAuthToken(ctx, store.AuthToken{
			Source:      s.Name(),
			AccessToken: lResp.Token,
			ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
		})
	}

	return lResp.Token, nil
}

func (s *Source) Sync(ctx context.Context, st *store.Store, env map[string]string, opts sources.SyncOptions) (*sources.SyncResult, error) {
	token, err := s.getToken(ctx, st, env)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", defaultBaseURL+"/user/history", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch history: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("history request failed with status %d", resp.StatusCode)
	}

	var histResp episodeListResponse
	if err := json.NewDecoder(resp.Body).Decode(&histResp); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}

	sinceTime := opts.Since
	if sinceTime.IsZero() && !opts.FullReindex {
		cursor, err := st.GetSyncCursor(ctx, s.Name())
		if err == nil && cursor != nil && !cursor.LastSyncedAt.IsZero() {
			sinceTime = cursor.LastSyncedAt.Add(-24 * time.Hour)
		}
	}

	var fetchedItems []model.Item
	for _, ep := range histResp.Episodes {
		var epTime time.Time
		if t, err := time.Parse(time.RFC3339, ep.Published); err == nil {
			epTime = t.UTC()
		} else if t, err := time.Parse("2006-01-02 15:04:05", ep.Published); err == nil {
			epTime = t.UTC()
		} else {
			epTime = time.Now().UTC()
		}

		if !sinceTime.IsZero() && epTime.Before(sinceTime) {
			continue
		}

		rawJSON, _ := json.Marshal(ep)
		urlStr := ep.URL
		if urlStr == "" && ep.UUID != "" {
			urlStr = fmt.Sprintf("https://pca.st/episode/%s", ep.UUID)
		}

		item := model.Item{
			ID:         fmt.Sprintf("%s:%s", s.Name(), ep.UUID),
			Source:     s.Name(),
			ExternalID: ep.UUID,
			Timestamp:  epTime,
			URL:        urlStr,
			Title:      ep.Title,
			Summary:    fmt.Sprintf("From podcast %s", ep.PodcastTitle),
			AuthorName: ep.PodcastTitle,
			Tags:       []string{ep.PodcastTitle},
			Metadata: map[string]any{
				"podcast":      ep.PodcastTitle,
				"podcast_uuid": ep.PodcastUUID,
				"duration":     ep.Duration,
				"played_up_to": ep.PlayedUpTo,
			},
			RawPayload: json.RawMessage(rawJSON),
		}
		fetchedItems = append(fetchedItems, item)
	}

	if err := st.UpsertItems(ctx, fetchedItems); err != nil {
		return nil, fmt.Errorf("store pocketcasts items: %w", err)
	}

	_ = st.SetSyncCursor(ctx, model.SyncCursor{
		Source:       s.Name(),
		LastSyncedAt: time.Now().UTC(),
		Status:       "ok",
	})

	return &sources.SyncResult{
		Source:   s.Name(),
		Fetched:  len(fetchedItems),
		Upserted: len(fetchedItems),
	}, nil
}

func (s *Source) RenderMarkdown(w io.Writer, items []model.Item) error {
	if len(items) == 0 {
		return nil
	}

	// Group by date
	grouped := make(map[string][]model.Item)
	var dates []string

	for _, item := range items {
		d := item.Timestamp.Local().Format("2006-01-02")
		if _, ok := grouped[d]; !ok {
			dates = append(dates, d)
		}
		grouped[d] = append(grouped[d], item)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	for _, d := range dates {
		fmt.Fprintf(w, "## %s\n\n", d)
		for _, item := range grouped[d] {
			podcast, _ := item.Metadata["podcast"].(string)
			durationSec, _ := item.Metadata["duration"].(int)
			durationMin := durationSec / 60

			if durationMin > 0 {
				fmt.Fprintf(w, "- **%s** — [%s](%s) (%d min)\n", podcast, item.Title, item.URL, durationMin)
			} else {
				fmt.Fprintf(w, "- **%s** — [%s](%s)\n", podcast, item.Title, item.URL)
			}
		}
		fmt.Fprintln(w)
	}

	return nil
}
