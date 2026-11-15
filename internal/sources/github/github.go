package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
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

func (s *Source) Name() string  { return "github" }
func (s *Source) Label() string { return "GitHub" }

const (
	defaultBaseURL = "https://api.github.com"
	userAgent      = "me-to-markdown"
	apiVersion     = "2022-11-28"
	acceptHeader   = "application/vnd.github+json"
	maxEventPages  = 10
)

type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Actor     struct{ Login string } `json:"actor"`
	Repo      struct{ Name string }  `json:"repo"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

func getCredentials(env map[string]string) string {
	if env != nil {
		return env["GITHUB_TOKEN"]
	}
	return ""
}

func (s *Source) ValidateAuth(ctx context.Context, env map[string]string) error {
	token := getCredentials(env)
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", defaultBaseURL+"/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", acceptHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("github validation failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("github rejected token (%s)", resp.Status)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *Source) Sync(ctx context.Context, st *store.Store, env map[string]string, opts sources.SyncOptions) (*sources.SyncResult, error) {
	token := getCredentials(env)
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is required")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// 1. Get authenticated user login
	userReq, err := http.NewRequestWithContext(ctx, "GET", defaultBaseURL+"/user", nil)
	if err != nil {
		return nil, err
	}
	userReq.Header.Set("Authorization", "Bearer "+token)
	userReq.Header.Set("User-Agent", userAgent)
	userReq.Header.Set("Accept", acceptHeader)

	userResp, err := client.Do(userReq)
	if err != nil {
		return nil, fmt.Errorf("fetch github user: %w", err)
	}
	defer func() { _ = userResp.Body.Close() }()

	if userResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get user failed with status %d", userResp.StatusCode)
	}

	var u struct{ Login string `json:"login"` }
	if err := json.NewDecoder(userResp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}

	sinceTime := opts.Since
	if sinceTime.IsZero() && !opts.FullReindex {
		cursor, err := st.GetSyncCursor(ctx, s.Name())
		if err == nil && cursor != nil && !cursor.LastSyncedAt.IsZero() {
			sinceTime = cursor.LastSyncedAt.Add(-1 * time.Hour)
		}
	}

	// 2. Fetch events
	urlStr := fmt.Sprintf("%s/users/%s/events?per_page=100", defaultBaseURL, u.Login)
	var fetchedItems []model.Item
	stop := false

	for page := 0; page < maxEventPages && urlStr != "" && !stop; page++ {
		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", acceptHeader)
		req.Header.Set("X-GitHub-Api-Version", apiVersion)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch events page: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("events API returned status %d", resp.StatusCode)
		}

		var events []Event
		err = json.NewDecoder(resp.Body).Decode(&events)
		linkHeader := resp.Header.Get("Link")
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode events: %w", err)
		}

		for _, ev := range events {
			if !sinceTime.IsZero() && ev.CreatedAt.Before(sinceTime) {
				stop = true
				break
			}
			item := eventToItem(s.Name(), ev)
			fetchedItems = append(fetchedItems, item)
		}

		urlStr = parseNextLink(linkHeader)
	}

	if err := st.UpsertItems(ctx, fetchedItems); err != nil {
		return nil, fmt.Errorf("store github items: %w", err)
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

func eventToItem(sourceName string, ev Event) model.Item {
	rawJSON, _ := json.Marshal(ev)
	kind, summary, url := normalizeEvent(ev)

	return model.Item{
		ID:         fmt.Sprintf("%s:%s", sourceName, ev.ID),
		Source:     sourceName,
		ExternalID: ev.ID,
		Timestamp:  ev.CreatedAt.UTC(),
		URL:        url,
		Title:      summary,
		Summary:    fmt.Sprintf("[%s] %s in %s", kind, summary, ev.Repo.Name),
		AuthorName: ev.Actor.Login,
		Tags:       []string{ev.Repo.Name, ev.Type},
		Metadata: map[string]any{
			"repo": ev.Repo.Name,
			"type": ev.Type,
			"kind": kind,
		},
		RawPayload: json.RawMessage(rawJSON),
	}
}

func normalizeEvent(ev Event) (kind, summary, url string) {
	kind = ev.Type
	summary = ev.Type
	url = fmt.Sprintf("https://github.com/%s", ev.Repo.Name)

	switch ev.Type {
	case "PushEvent":
		var p struct {
			Ref     string `json:"ref"`
			Before  string `json:"before"`
			Head    string `json:"head"`
			Size    int    `json:"size"`
			Commits []struct {
				SHA     string `json:"sha"`
				Message string `json:"message"`
			} `json:"commits"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		branch := strings.TrimPrefix(p.Ref, "refs/heads/")
		kind = "Push"
		if p.Size > 0 && len(p.Commits) > 0 {
			summary = fmt.Sprintf("pushed %d commit(s) to `%s`: %s", p.Size, branch, firstLine(p.Commits[0].Message))
			url = fmt.Sprintf("https://github.com/%s/commit/%s", ev.Repo.Name, p.Commits[0].SHA)
		} else if isRealSHA(p.Before) && isRealSHA(p.Head) {
			summary = fmt.Sprintf("pushed to `%s`", branch)
			url = fmt.Sprintf("https://github.com/%s/compare/%s...%s", ev.Repo.Name, p.Before, p.Head)
		} else {
			summary = fmt.Sprintf("pushed to `%s`", branch)
		}

	case "PullRequestEvent":
		var p struct {
			Action      string `json:"action"`
			Number      int    `json:"number"`
			PullRequest struct {
				Title   string `json:"title"`
				HTMLURL string `json:"html_url"`
			} `json:"pull_request"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		kind = fmt.Sprintf("PR %s", p.Action)
		summary = fmt.Sprintf("PR #%d %s: %s", p.Number, p.Action, p.PullRequest.Title)
		url = p.PullRequest.HTMLURL

	case "IssuesEvent":
		var p struct {
			Action string `json:"action"`
			Issue  struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				HTMLURL string `json:"html_url"`
			} `json:"issue"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		kind = fmt.Sprintf("Issue %s", p.Action)
		summary = fmt.Sprintf("Issue #%d %s: %s", p.Issue.Number, p.Action, p.Issue.Title)
		url = p.Issue.HTMLURL

	case "CreateEvent":
		var p struct {
			RefType string `json:"ref_type"`
			Ref     string `json:"ref"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		kind = "Create"
		summary = fmt.Sprintf("created %s `%s`", p.RefType, p.Ref)

	case "WatchEvent":
		kind = "Star"
		summary = fmt.Sprintf("starred %s", ev.Repo.Name)
	}

	return kind, summary, url
}

func firstLine(s string) string {
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}

func isRealSHA(s string) bool {
	return len(s) == 40 && !strings.HasPrefix(s, "000000")
}

var nextLinkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func parseNextLink(header string) string {
	if header == "" {
		return ""
	}
	m := nextLinkRE.FindStringSubmatch(header)
	if len(m) < 2 {
		return ""
	}
	return m[1]
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
			repo, _ := item.Metadata["repo"].(string)
			kind, _ := item.Metadata["kind"].(string)

			fmt.Fprintf(w, "- **%s** `[%s]` [%s](%s) (%s)\n",
				repo,
				kind,
				item.Title,
				item.URL,
				item.Timestamp.Local().Format("15:04"),
			)
		}
		fmt.Fprintln(w)
	}

	return nil
}
