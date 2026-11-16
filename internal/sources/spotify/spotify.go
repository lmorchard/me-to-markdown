package spotify

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

func (s *Source) Name() string  { return "spotify" }
func (s *Source) Label() string { return "Spotify" }

const (
	apiBaseURL = "https://api.spotify.com/v1"
	tokenURL   = "https://accounts.spotify.com/api/token"
)

type PlayHistoryItem struct {
	Track struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		DurationMs int    `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name string `json:"name"`
		} `json:"album"`
		ExternalURLs struct {
			Spotify string `json:"spotify"`
		} `json:"external_urls"`
	} `json:"track"`
	PlayedAt string `json:"played_at"`
}

type RecentlyPlayedResponse struct {
	Items []PlayHistoryItem `json:"items"`
}

func (s *Source) ValidateAuth(ctx context.Context, env map[string]string) error {
	tok, err := s.getToken(ctx, nil, env)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", apiBaseURL+"/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("spotify auth validation request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spotify API returned %d during auth validation", resp.StatusCode)
	}
	return nil
}

func (s *Source) getToken(ctx context.Context, st *store.Store, env map[string]string) (string, error) {
	clientID := env["SPOTIFY_CLIENT_ID"]

	var tok *store.AuthToken
	if st != nil {
		tok, _ = st.GetAuthToken(ctx, s.Name())
	}

	// Try fallback from legacy database if not found in current store
	if tok == nil || tok.AccessToken == "" {
		legacyTok, lErr := loadLegacySpotifyToken()
		if lErr == nil && legacyTok != nil {
			tok = legacyTok
			if st != nil {
				_ = st.SetAuthToken(ctx, *tok)
			}
		}
	}

	if tok == nil || tok.AccessToken == "" {
		return "", fmt.Errorf("no spotify auth token available; run auth first")
	}

	// Check if token needs refresh
	if time.Now().After(tok.ExpiresAt.Add(-2 * time.Minute)) {
		if tok.RefreshToken == "" {
			return "", fmt.Errorf("spotify token expired and no refresh token available")
		}
		if clientID == "" {
			return "", fmt.Errorf("SPOTIFY_CLIENT_ID required to refresh token")
		}

		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", tok.RefreshToken)
		form.Set("client_id", clientID)

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("refresh spotify token: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("spotify refresh failed %d: %s", resp.StatusCode, string(body))
		}

		var tr struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			TokenType    string `json:"token_type"`
			Scope        string `json:"scope"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
			return "", err
		}

		tok.AccessToken = tr.AccessToken
		if tr.RefreshToken != "" {
			tok.RefreshToken = tr.RefreshToken
		}
		tok.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)

		if st != nil {
			_ = st.SetAuthToken(ctx, *tok)
		}
	}

	return tok.AccessToken, nil
}

func loadLegacySpotifyToken() (*store.AuthToken, error) {
	home, _ := os.UserHomeDir()
	legacyDB := filepath.Join(home, ".local", "state", "spotify-to-markdown", "state.db")
	if _, err := os.Stat(legacyDB); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", legacyDB)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	var t store.AuthToken
	var expStr string
	err = db.QueryRow(`
		SELECT access_token, refresh_token, token_type, scope, expires_at
		FROM auth_tokens
		WHERE id = 1
	`).Scan(&t.AccessToken, &t.RefreshToken, &t.TokenType, &t.Scope, &expStr)
	if err != nil {
		return nil, err
	}

	t.Source = "spotify"
	if parsed, err := time.Parse(time.RFC3339, expStr); err == nil {
		t.ExpiresAt = parsed.UTC()
	} else if parsed, err := time.Parse("2006-01-02 15:04:05", expStr); err == nil {
		t.ExpiresAt = parsed.UTC()
	}

	return &t, nil
}

func (s *Source) Sync(ctx context.Context, st *store.Store, env map[string]string, opts sources.SyncOptions) (*sources.SyncResult, error) {
	token, err := s.getToken(ctx, st, env)
	if err != nil {
		return nil, err
	}

	reqURL := apiBaseURL + "/me/player/recently-played?limit=50"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spotify recently played: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("spotify API returned %d: %s", resp.StatusCode, string(body))
	}

	var rpResp RecentlyPlayedResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpResp); err != nil {
		return nil, fmt.Errorf("decode spotify response: %w", err)
	}

	var fetchedItems []model.Item
	for _, play := range rpResp.Items {
		var playTime time.Time
		if t, err := time.Parse(time.RFC3339, play.PlayedAt); err == nil {
			playTime = t.UTC()
		} else if t, err := time.Parse(time.RFC3339Nano, play.PlayedAt); err == nil {
			playTime = t.UTC()
		} else {
			continue
		}

		var artistNames []string
		for _, a := range play.Track.Artists {
			artistNames = append(artistNames, a.Name)
		}
		artistsStr := strings.Join(artistNames, ", ")

		rawJSON, _ := json.Marshal(play)
		item := model.Item{
			ID:         fmt.Sprintf("%s:%s", s.Name(), strconv.FormatInt(playTime.UnixMilli(), 10)),
			Source:     s.Name(),
			ExternalID: play.PlayedAt,
			Timestamp:  playTime,
			URL:        play.Track.ExternalURLs.Spotify,
			Title:      play.Track.Name,
			Summary:    fmt.Sprintf("by %s (on %s)", artistsStr, play.Track.Album.Name),
			AuthorName: artistsStr,
			Tags:       artistNames,
			Metadata: map[string]any{
				"album":       play.Track.Album.Name,
				"artists":     artistsStr,
				"duration_ms": play.Track.DurationMs,
			},
			RawPayload: json.RawMessage(rawJSON),
		}
		fetchedItems = append(fetchedItems, item)
	}

	if err := st.UpsertItems(ctx, fetchedItems); err != nil {
		return nil, fmt.Errorf("store spotify items: %w", err)
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

	ew := sources.NewErrWriter(w)
	for _, d := range dates {
		ew.Printf("## %s\n\n", d)
		for _, item := range grouped[d] {
			artists, _ := item.Metadata["artists"].(string)
			album, _ := item.Metadata["album"].(string)

			if item.URL != "" {
				ew.Printf("- **[%s](%s)** by %s (on _%s_)\n", item.Title, item.URL, artists, album)
			} else {
				ew.Printf("- **%s** by %s (on _%s_)\n", item.Title, artists, album)
			}
		}
		ew.Println()
	}

	return ew.Err()
}
