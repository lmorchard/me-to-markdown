package mastodon

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/lmorchard/me-to-markdown/internal/store"
	gomastodon "github.com/mattn/go-mastodon"
)

func init() {
	sources.Register(&Source{})
}

type Source struct{}

func (s *Source) Name() string  { return "mastodon" }
func (s *Source) Label() string { return "Mastodon" }

func getCredentials(env map[string]string) (server, token string) {
	if env != nil {
		server = env["MASTODON_SERVER"]
		token = env["MASTODON_ACCESS_TOKEN"]
	}
	return strings.TrimRight(server, "/"), token
}

func (s *Source) ValidateAuth(ctx context.Context, env map[string]string) error {
	server, token := getCredentials(env)
	if server == "" {
		return fmt.Errorf("MASTODON_SERVER is required")
	}
	if token == "" {
		return fmt.Errorf("MASTODON_ACCESS_TOKEN is required")
	}

	client := gomastodon.NewClient(&gomastodon.Config{
		Server:      server,
		AccessToken: token,
	})

	_, err := client.GetAccountCurrentUser(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify mastodon credentials: %w", err)
	}
	return nil
}

func (s *Source) Sync(ctx context.Context, st *store.Store, env map[string]string, opts sources.SyncOptions) (*sources.SyncResult, error) {
	server, token := getCredentials(env)
	if server == "" || token == "" {
		return nil, fmt.Errorf("mastodon credentials missing (MASTODON_SERVER and MASTODON_ACCESS_TOKEN required)")
	}

	client := gomastodon.NewClient(&gomastodon.Config{
		Server:      server,
		AccessToken: token,
	})

	account, err := client.GetAccountCurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("get mastodon current user: %w", err)
	}

	sinceTime := opts.Since
	if sinceTime.IsZero() && !opts.FullReindex {
		cursor, err := st.GetSyncCursor(ctx, s.Name())
		if err == nil && cursor != nil && !cursor.LastSyncedAt.IsZero() {
			sinceTime = cursor.LastSyncedAt.Add(-1 * time.Hour)
		}
	}

	var fetchedItems []model.Item
	var pg gomastodon.Pagination
	pg.Limit = 40

	done := false
	for !done {
		statuses, err := client.GetAccountStatuses(ctx, account.ID, &pg)
		if err != nil {
			return nil, fmt.Errorf("get account statuses: %w", err)
		}
		if len(statuses) == 0 {
			break
		}

		for _, status := range statuses {
			if !sinceTime.IsZero() && status.CreatedAt.Before(sinceTime) {
				done = true
				break
			}

			item := statusToItem(s.Name(), status)
			fetchedItems = append(fetchedItems, item)
		}

		if len(statuses) < int(pg.Limit) || done {
			break
		}
		pg.MaxID = statuses[len(statuses)-1].ID
	}

	if err := st.UpsertItems(ctx, fetchedItems); err != nil {
		return nil, fmt.Errorf("store mastodon items: %w", err)
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

func statusToItem(sourceName string, status *gomastodon.Status) model.Item {
	rawJSON, _ := json.Marshal(status)
	content := cleanContent(status.Content)

	var tags []string
	for _, t := range status.Tags {
		tags = append(tags, t.Name)
	}

	var media []model.MediaAttachment
	targetStatus := status
	if status.Reblog != nil {
		targetStatus = status.Reblog
	}
	for _, m := range targetStatus.MediaAttachments {
		media = append(media, model.MediaAttachment{
			Type:        model.MediaType(m.Type),
			URL:         m.URL,
			PreviewURL:  m.PreviewURL,
			Description: m.Description,
		})
	}

	title := status.SpoilerText
	if title == "" {
		// Use first line of content up to 80 chars
		firstLine := strings.Split(content, "\n")[0]
		if len(firstLine) > 80 {
			title = firstLine[:80] + "..."
		} else {
			title = firstLine
		}
	}

	meta := map[string]any{
		"visibility":   status.Visibility,
		"is_reblog":    status.Reblog != nil,
		"is_reply":     status.InReplyToID != nil,
		"replies":      status.RepliesCount,
		"reblogs":      status.ReblogsCount,
		"favourites":   status.FavouritesCount,
		"spoiler_text": status.SpoilerText,
	}

	authorName := status.Account.DisplayName
	if authorName == "" {
		authorName = status.Account.Username
	}

	return model.Item{
		ID:         fmt.Sprintf("%s:%s", sourceName, status.ID),
		Source:     sourceName,
		ExternalID: string(status.ID),
		Timestamp:  status.CreatedAt.UTC(),
		URL:        status.URL,
		Title:      title,
		Summary:    content,
		AuthorName: authorName,
		AuthorURL:  status.Account.URL,
		Tags:       tags,
		Media:      media,
		Metadata:   meta,
		RawPayload: json.RawMessage(rawJSON),
	}
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
			isReblog, _ := item.Metadata["is_reblog"].(bool)
			prefix := ""
			if isReblog {
				prefix = "🔄 Boosted: "
			}

			fmt.Fprintf(w, "- %s[%s](%s)\n\n", prefix, item.Timestamp.Local().Format("15:04"), item.URL)
			if item.Summary != "" {
				lines := strings.Split(strings.TrimSpace(item.Summary), "\n")
				for _, l := range lines {
					fmt.Fprintf(w, "  > %s\n", l)
				}
				fmt.Fprintln(w)
			}

			for _, m := range item.Media {
				desc := m.Description
				if desc == "" {
					desc = "Media attachment"
				}
				fmt.Fprintf(w, "  Media: [%s](%s) - %s\n", m.Type, m.URL, desc)
			}
			if len(item.Media) > 0 {
				fmt.Fprintln(w)
			}
		}
		fmt.Fprintln(w)
	}

	return nil
}

func cleanContent(content string) string {
	repl := strings.NewReplacer(
		"<p>", "\n\n",
		"</p>", "",
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
	)
	content = repl.Replace(content)

	for strings.Contains(content, "<") && strings.Contains(content, ">") {
		start := strings.Index(content, "<")
		end := strings.Index(content, ">")
		if start < end {
			content = content[:start] + content[end+1:]
		} else {
			break
		}
	}

	content = html.UnescapeString(content)
	return strings.TrimSpace(content)
}

func dummyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.String()
}
