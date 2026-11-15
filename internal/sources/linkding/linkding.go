package linkding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/lmorchard/me-to-markdown/internal/store"
)

func init() {
	sources.Register(&Source{})
}

type Source struct{}

func (s *Source) Name() string  { return "linkding" }
func (s *Source) Label() string { return "Linkding" }

type bookmarkPayload struct {
	ID                 int       `json:"id"`
	URL                string    `json:"url"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	Notes              string    `json:"notes"`
	WebsiteTitle       string    `json:"website_title"`
	WebsiteDescription string    `json:"website_description"`
	IsArchived         bool      `json:"is_archived"`
	Unread             bool      `json:"unread"`
	Shared             bool      `json:"shared"`
	TagNames           []string  `json:"tag_names"`
	DateAdded          time.Time `json:"date_added"`
	DateModified       time.Time `json:"date_modified"`
}

type bookmarksResponse struct {
	Count    int               `json:"count"`
	Next     *string           `json:"next"`
	Previous *string           `json:"previous"`
	Results  []bookmarkPayload `json:"results"`
}

func getCredentials(env map[string]string) (baseURL, token string) {
	if env != nil {
		baseURL = env["LINKDING_URL"]
		token = env["LINKDING_TOKEN"]
	}
	return strings.TrimRight(baseURL, "/"), token
}

func (s *Source) ValidateAuth(ctx context.Context, env map[string]string) error {
	baseURL, token := getCredentials(env)
	if baseURL == "" {
		return fmt.Errorf("LINKDING_URL is required")
	}
	if token == "" {
		return fmt.Errorf("LINKDING_TOKEN is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/bookmarks/?limit=1", baseURL), nil)
	if err != nil {
		return fmt.Errorf("create validation request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", token))
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("linkding validation request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("linkding rejected credentials (%s)", resp.Status)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("linkding API returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *Source) Sync(ctx context.Context, st *store.Store, env map[string]string, opts sources.SyncOptions) (*sources.SyncResult, error) {
	baseURL, token := getCredentials(env)
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("linkding credentials missing (LINKDING_URL and LINKDING_TOKEN required)")
	}

	sinceTime := opts.Since
	if sinceTime.IsZero() && !opts.FullReindex {
		cursor, err := st.GetSyncCursor(ctx, s.Name())
		if err == nil && cursor != nil && !cursor.LastSyncedAt.IsZero() {
			// Back off 1 hour to accommodate clock skew
			sinceTime = cursor.LastSyncedAt.Add(-1 * time.Hour)
		}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	var fetchedItems []model.Item
	limit := 100
	offset := 0

	for {
		params := url.Values{}
		params.Set("limit", strconv.Itoa(limit))
		params.Set("offset", strconv.Itoa(offset))
		if !sinceTime.IsZero() {
			params.Set("added_since", sinceTime.UTC().Format(time.RFC3339))
		}

		apiURL := fmt.Sprintf("%s/api/bookmarks/?%s", baseURL, params.Encode())
		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Token %s", token))
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch bookmarks at offset %d: %w", offset, err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("linkding API returned status %d: %s", resp.StatusCode, string(body))
		}

		var apiResp bookmarksResponse
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode bookmarks response: %w", err)
		}

		if len(apiResp.Results) == 0 {
			break
		}

		for _, b := range apiResp.Results {
			rawJSON, _ := json.Marshal(b)
			title := b.Title
			if title == "" {
				title = b.WebsiteTitle
			}
			if title == "" {
				title = b.URL
			}

			meta := map[string]any{
				"unread":              b.Unread,
				"shared":              b.Shared,
				"is_archived":         b.IsArchived,
				"website_title":       b.WebsiteTitle,
				"website_description": b.WebsiteDescription,
			}

			item := model.Item{
				ID:         fmt.Sprintf("linkding:%d", b.ID),
				Source:     s.Name(),
				ExternalID: strconv.Itoa(b.ID),
				Timestamp:  b.DateAdded.UTC(),
				URL:        b.URL,
				Title:      title,
				Summary:    b.Description,
				Content:    b.Notes,
				Tags:       b.TagNames,
				Metadata:   meta,
				RawPayload: json.RawMessage(rawJSON),
			}
			fetchedItems = append(fetchedItems, item)
		}

		if len(apiResp.Results) < limit {
			break
		}
		offset += limit
	}

	if err := st.UpsertItems(ctx, fetchedItems); err != nil {
		return nil, fmt.Errorf("store bookmarks: %w", err)
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

	// Group by date (YYYY-MM-DD)
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
		dayItems := grouped[d]
		for _, item := range dayItems {
			fmt.Fprintf(w, "- [%s](%s)", item.Title, item.URL)
			if item.Summary != "" {
				fmt.Fprintf(w, " — %s", strings.ReplaceAll(item.Summary, "\n", " "))
			}
			if len(item.Tags) > 0 {
				fmt.Fprintf(w, " `[%s]`", strings.Join(item.Tags, ", "))
			}
			fmt.Fprintln(w)

			if strings.TrimSpace(item.Content) != "" {
				lines := strings.Split(strings.TrimSpace(item.Content), "\n")
				for _, line := range lines {
					fmt.Fprintf(w, "  > %s\n", line)
				}
				fmt.Fprintln(w)
			}
		}
		fmt.Fprintln(w)
	}

	return nil
}
