package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func (s *Source) Name() string  { return "youtube" }
func (s *Source) Label() string { return "YouTube" }

const (
	apiBaseURL   = "https://www.googleapis.com/youtube/v3"
	tokenURL     = "https://oauth2.googleapis.com/token"
)

type PlaylistItem struct {
	ID      string `json:"id"`
	Snippet struct {
		PublishedAt string `json:"publishedAt"`
		ChannelID   string `json:"channelId"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Thumbnails  struct {
			Default struct {
				URL string `json:"url"`
			} `json:"default"`
			High struct {
				URL string `json:"url"`
			} `json:"high"`
		} `json:"thumbnails"`
		ChannelTitle           string `json:"channelTitle"`
		VideoOwnerChannelTitle string `json:"videoOwnerChannelTitle"`
		ResourceID             struct {
			VideoID string `json:"videoId"`
		} `json:"resourceId"`
	} `json:"snippet"`
	ContentDetails struct {
		VideoID          string    `json:"videoId"`
		VideoPublishedAt time.Time `json:"videoPublishedAt"`
	} `json:"contentDetails"`
}

type PlaylistResponse struct {
	NextPageToken string         `json:"nextPageToken"`
	Items         []PlaylistItem `json:"items"`
}

func (s *Source) ValidateAuth(ctx context.Context, env map[string]string) error {
	tok, err := s.getToken(ctx, nil, env)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", apiBaseURL+"/channels?part=snippet&mine=true", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("youtube validation request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("youtube API returned %d during auth validation", resp.StatusCode)
	}
	return nil
}

func (s *Source) getToken(ctx context.Context, st *store.Store, env map[string]string) (string, error) {
	clientID := env["YOUTUBE_CLIENT_ID"]
	clientSecret := env["YOUTUBE_CLIENT_SECRET"]

	var tok *store.AuthToken
	if st != nil {
		tok, _ = st.GetAuthToken(ctx, s.Name())
	}

	if tok == nil || tok.AccessToken == "" {
		legacyTok, lErr := loadLegacyYouTubeToken()
		if lErr == nil && legacyTok != nil {
			tok = legacyTok
			if st != nil {
				_ = st.SetAuthToken(ctx, *tok)
			}
		}
	}

	if tok == nil || tok.AccessToken == "" {
		return "", fmt.Errorf("no youtube auth token available; run auth first")
	}

	// Check if token needs refresh
	if time.Now().After(tok.ExpiresAt.Add(-2 * time.Minute)) {
		if tok.RefreshToken == "" {
			return "", fmt.Errorf("youtube token expired and no refresh token available")
		}
		if clientID == "" || clientSecret == "" {
			return "", fmt.Errorf("YOUTUBE_CLIENT_ID and YOUTUBE_CLIENT_SECRET required to refresh token")
		}

		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", tok.RefreshToken)
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("refresh youtube token: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("youtube refresh failed %d: %s", resp.StatusCode, string(body))
		}

		var tr struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
			TokenType   string `json:"token_type"`
			Scope       string `json:"scope"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
			return "", err
		}

		tok.AccessToken = tr.AccessToken
		tok.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)

		if st != nil {
			_ = st.SetAuthToken(ctx, *tok)
		}
	}

	return tok.AccessToken, nil
}

func loadLegacyYouTubeToken() (*store.AuthToken, error) {
	home, _ := os.UserHomeDir()
	legacyDB := filepath.Join(home, ".local", "state", "youtube-to-markdown", "state.db")
	if _, err := os.Stat(legacyDB); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", legacyDB)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	getKV := func(k string) string {
		var val string
		_ = db.QueryRow("SELECT value FROM kv WHERE key = ?", k).Scan(&val)
		return val
	}

	accessToken := getKV("youtube.access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("no access token in legacy kv")
	}

	var tok store.AuthToken
	tok.Source = "youtube"
	tok.AccessToken = accessToken
	tok.RefreshToken = getKV("youtube.refresh_token")
	tok.TokenType = getKV("youtube.token_type")
	tok.Scope = getKV("youtube.scope")

	expStr := getKV("youtube.expires_at")
	if t, err := time.Parse(time.RFC3339, expStr); err == nil {
		tok.ExpiresAt = t.UTC()
	} else {
		tok.ExpiresAt = time.Now().Add(1 * time.Hour).UTC()
	}

	return &tok, nil
}

func (s *Source) Sync(ctx context.Context, st *store.Store, env map[string]string, opts sources.SyncOptions) (*sources.SyncResult, error) {
	token, err := s.getToken(ctx, st, env)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var fetchedItems []model.Item
	pageToken := ""

	sinceTime := opts.Since
	if sinceTime.IsZero() && !opts.FullReindex {
		cursor, err := st.GetSyncCursor(ctx, s.Name())
		if err == nil && cursor != nil && !cursor.LastSyncedAt.IsZero() {
			sinceTime = cursor.LastSyncedAt.Add(-24 * time.Hour)
		}
	}

	stop := false
	for page := 0; page < 10 && !stop; page++ {
		reqURL := fmt.Sprintf("%s/playlistItems?part=snippet,contentDetails&playlistId=LL&maxResults=50", apiBaseURL)
		if pageToken != "" {
			reqURL += "&pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch playlist items: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("youtube API returned %d: %s", resp.StatusCode, string(body))
		}

		var plResp PlaylistResponse
		err = json.NewDecoder(resp.Body).Decode(&plResp)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode youtube response: %w", err)
		}

		if len(plResp.Items) == 0 {
			break
		}

		for _, item := range plResp.Items {
			videoID := item.ContentDetails.VideoID
			if videoID == "" {
				videoID = item.Snippet.ResourceID.VideoID
			}
			if videoID == "" {
				continue
			}

			var videoTime time.Time
			if t, err := time.Parse(time.RFC3339, item.Snippet.PublishedAt); err == nil {
				videoTime = t.UTC()
			} else {
				videoTime = item.ContentDetails.VideoPublishedAt.UTC()
			}

			if !sinceTime.IsZero() && videoTime.Before(sinceTime) {
				stop = true
				break
			}

			rawJSON, _ := json.Marshal(item)
			channel := item.Snippet.VideoOwnerChannelTitle
			if channel == "" {
				channel = item.Snippet.ChannelTitle
			}

			var media []model.MediaAttachment
			thumbURL := item.Snippet.Thumbnails.High.URL
			if thumbURL == "" {
				thumbURL = item.Snippet.Thumbnails.Default.URL
			}
			if thumbURL != "" {
				media = append(media, model.MediaAttachment{
					Type:       model.MediaThumbnail,
					URL:        thumbURL,
					PreviewURL: thumbURL,
				})
			}

			videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)

			it := model.Item{
				ID:         fmt.Sprintf("%s:%s", s.Name(), videoID),
				Source:     s.Name(),
				ExternalID: videoID,
				Timestamp:  videoTime,
				URL:        videoURL,
				Title:      item.Snippet.Title,
				Summary:    item.Snippet.Description,
				AuthorName: channel,
				AuthorURL:  fmt.Sprintf("https://www.youtube.com/channel/%s", item.Snippet.ChannelID),
				Media:      media,
				Metadata: map[string]any{
					"video_id":      videoID,
					"channel_title": channel,
				},
				RawPayload: json.RawMessage(rawJSON),
			}
			fetchedItems = append(fetchedItems, it)
		}

		pageToken = plResp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	if err := st.UpsertItems(ctx, fetchedItems); err != nil {
		return nil, fmt.Errorf("store youtube items: %w", err)
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
		fmt.Fprintf(w, "### %s\n\n", d)
		for _, item := range grouped[d] {
			channel, _ := item.Metadata["channel_title"].(string)
			videoID, _ := item.Metadata["video_id"].(string)

			fmt.Fprintf(w, "- **[%s](%s)** by %s (`%s`)\n", item.Title, item.URL, channel, videoID)
			if item.Content != "" {
				fmt.Fprintf(w, "  > Transcript excerpt: %s\n\n", firstLine(item.Content))
			}
		}
		fmt.Fprintln(w)
	}

	return nil
}

func firstLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
