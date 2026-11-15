package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/model"
	_ "modernc.org/sqlite"
)

// Store manages local SQLite storage for personal exhaust items and sync state.
type Store struct {
	db *sql.DB
}

// ItemFilter specifies query constraints for items.
type ItemFilter struct {
	Sources   []string
	Since     time.Time
	Until     time.Time
	Limit     int
	Ascending bool
}

// AuthToken represents an OAuth2 or bearer token for a source.
type AuthToken struct {
	Source       string    `json:"source"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// SourceStats provides high-level health and volume metrics for a source.
type SourceStats struct {
	Source       string     `json:"source"`
	ItemCount    int        `json:"item_count"`
	EarliestItem *time.Time `json:"earliest_item,omitempty"`
	LatestItem   *time.Time `json:"latest_item,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	Status       string     `json:"status,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

// Open initializes SQLite database at dbPath, creating parent directories if needed.
func Open(dbPath string) (*Store, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create store dir %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %s: %w", dbPath, err)
	}

	// Optimize pragmas for concurrent read/write
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec pragma %q: %w", p, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return s, nil
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sync_cursors (
		source TEXT PRIMARY KEY,
		cursor TEXT NOT NULL DEFAULT '',
		last_synced_at TIMESTAMP NOT NULL,
		status TEXT NOT NULL DEFAULT 'ok',
		last_error TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS items (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		external_id TEXT NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		url TEXT,
		title TEXT,
		summary TEXT,
		content TEXT,
		author_name TEXT,
		author_url TEXT,
		tags TEXT,
		media TEXT,
		metadata TEXT,
		raw_payload BLOB NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS auth_tokens (
		source TEXT PRIMARY KEY,
		access_token TEXT NOT NULL,
		refresh_token TEXT NOT NULL DEFAULT '',
		token_type TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '',
		expires_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_items_source_time ON items(source, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_items_timestamp ON items(timestamp DESC);
	`
	_, err := s.db.Exec(schema)
	return err
}

// UpsertItems stores or updates a batch of items within a transaction.
func (s *Store) UpsertItems(ctx context.Context, items []model.Item) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO items (
			id, source, external_id, timestamp, url, title, summary, content,
			author_name, author_url, tags, media, metadata, raw_payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			timestamp = excluded.timestamp,
			url = excluded.url,
			title = excluded.title,
			summary = excluded.summary,
			content = excluded.content,
			author_name = excluded.author_name,
			author_url = excluded.author_url,
			tags = excluded.tags,
			media = excluded.media,
			metadata = excluded.metadata,
			raw_payload = excluded.raw_payload;
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert stmt: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, it := range items {
		if it.ID == "" {
			it.ID = fmt.Sprintf("%s:%s", it.Source, it.ExternalID)
		}

		tagsJSON, _ := json.Marshal(it.Tags)
		mediaJSON, _ := json.Marshal(it.Media)
		metaJSON, _ := json.Marshal(it.Metadata)

		rawPayload := it.RawPayload
		if len(rawPayload) == 0 {
			rawPayload = []byte("{}")
		}

		tsStr := it.Timestamp.UTC().Format(time.RFC3339Nano)

		_, err := stmt.ExecContext(ctx,
			it.ID,
			it.Source,
			it.ExternalID,
			tsStr,
			it.URL,
			it.Title,
			it.Summary,
			it.Content,
			it.AuthorName,
			it.AuthorURL,
			string(tagsJSON),
			string(mediaJSON),
			string(metaJSON),
			[]byte(rawPayload),
		)
		if err != nil {
			return fmt.Errorf("upsert item %s: %w", it.ID, err)
		}
	}

	return tx.Commit()
}

// GetItems retrieves items matching the specified filter.
func (s *Store) GetItems(ctx context.Context, filter ItemFilter) ([]model.Item, error) {
	var where []string
	var args []any

	if len(filter.Sources) > 0 {
		placeholders := make([]string, len(filter.Sources))
		for i, src := range filter.Sources {
			placeholders[i] = "?"
			args = append(args, src)
		}
		where = append(where, fmt.Sprintf("source IN (%s)", strings.Join(placeholders, ",")))
	}

	if !filter.Since.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}

	if !filter.Until.IsZero() {
		where = append(where, "timestamp <= ?")
		args = append(args, filter.Until.UTC().Format(time.RFC3339Nano))
	}

	query := `
		SELECT id, source, external_id, timestamp, url, title, summary, content,
		       author_name, author_url, tags, media, metadata, raw_payload
		FROM items
	`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	orderDir := "DESC"
	if filter.Ascending {
		orderDir = "ASC"
	}
	query += fmt.Sprintf(" ORDER BY timestamp %s", orderDir)

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.Item
	for rows.Next() {
		var it model.Item
		var tsStr string
		var tagsJSON, mediaJSON, metaJSON sql.NullString
		var rawPayload []byte

		err := rows.Scan(
			&it.ID,
			&it.Source,
			&it.ExternalID,
			&tsStr,
			&it.URL,
			&it.Title,
			&it.Summary,
			&it.Content,
			&it.AuthorName,
			&it.AuthorURL,
			&tagsJSON,
			&mediaJSON,
			&metaJSON,
			&rawPayload,
		)
		if err != nil {
			return nil, fmt.Errorf("scan item row: %w", err)
		}

		if parsedTime, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			it.Timestamp = parsedTime.UTC()
		} else if parsedTime, err := time.Parse(time.RFC3339, tsStr); err == nil {
			it.Timestamp = parsedTime.UTC()
		}

		if tagsJSON.Valid && tagsJSON.String != "" {
			_ = json.Unmarshal([]byte(tagsJSON.String), &it.Tags)
		}
		if mediaJSON.Valid && mediaJSON.String != "" {
			_ = json.Unmarshal([]byte(mediaJSON.String), &it.Media)
		}
		if metaJSON.Valid && metaJSON.String != "" {
			_ = json.Unmarshal([]byte(metaJSON.String), &it.Metadata)
		}
		it.RawPayload = json.RawMessage(rawPayload)

		results = append(results, it)
	}

	return results, rows.Err()
}

// GetSyncCursor returns the cursor record for a source, or nil if none exists.
func (s *Store) GetSyncCursor(ctx context.Context, source string) (*model.SyncCursor, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT source, cursor, last_synced_at, status, last_error
		FROM sync_cursors
		WHERE source = ?;
	`, source)

	var sc model.SyncCursor
	var tsStr string
	err := row.Scan(&sc.Source, &sc.Cursor, &tsStr, &sc.Status, &sc.LastError)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query sync cursor: %w", err)
	}

	if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
		sc.LastSyncedAt = t.UTC()
	} else if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
		sc.LastSyncedAt = t.UTC()
	}

	return &sc, nil
}

// SetSyncCursor records sync progress for a source.
func (s *Store) SetSyncCursor(ctx context.Context, sc model.SyncCursor) error {
	tsStr := sc.LastSyncedAt.UTC().Format(time.RFC3339Nano)
	if sc.LastSyncedAt.IsZero() {
		tsStr = time.Now().UTC().Format(time.RFC3339Nano)
	}
	status := sc.Status
	if status == "" {
		status = "ok"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_cursors (source, cursor, last_synced_at, status, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source) DO UPDATE SET
			cursor = excluded.cursor,
			last_synced_at = excluded.last_synced_at,
			status = excluded.status,
			last_error = excluded.last_error;
	`, sc.Source, sc.Cursor, tsStr, status, sc.LastError)

	return err
}

// GetStats returns summary statistics per source.
func (s *Store) GetStats(ctx context.Context) ([]SourceStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(i.source, c.source) as source_name,
			COUNT(i.id) as item_count,
			MIN(i.timestamp) as earliest_item,
			MAX(i.timestamp) as latest_item,
			c.last_synced_at,
			c.status,
			c.last_error
		FROM items i
		FULL OUTER JOIN sync_cursors c ON i.source = c.source
		GROUP BY source_name
		ORDER BY source_name;
	`)
	// Note: If SQLite doesn't support FULL OUTER JOIN in all builds, fall back to left join union.
	if err != nil {
		return s.getStatsFallback(ctx)
	}
	defer func() { _ = rows.Close() }()

	var stats []SourceStats
	for rows.Next() {
		var ss SourceStats
		var earliestStr, latestStr, lastSyncedStr sql.NullString
		var status, lastError sql.NullString

		if err := rows.Scan(&ss.Source, &ss.ItemCount, &earliestStr, &latestStr, &lastSyncedStr, &status, &lastError); err != nil {
			return nil, err
		}
		if earliestStr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, earliestStr.String); err == nil {
				ss.EarliestItem = &t
			}
		}
		if latestStr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, latestStr.String); err == nil {
				ss.LatestItem = &t
			}
		}
		if lastSyncedStr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, lastSyncedStr.String); err == nil {
				ss.LastSyncedAt = &t
			}
		}
		if status.Valid {
			ss.Status = status.String
		}
		if lastError.Valid {
			ss.LastError = lastError.String
		}
		stats = append(stats, ss)
	}
	return stats, rows.Err()
}

func (s *Store) getStatsFallback(ctx context.Context) ([]SourceStats, error) {
	// Simple query against distinct sources
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT source FROM (
			SELECT source FROM items
			UNION
			SELECT source FROM sync_cursors
		) ORDER BY source;
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var sources []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err == nil {
			sources = append(sources, src)
		}
	}

	var results []SourceStats
	for _, src := range sources {
		ss := SourceStats{Source: src}
		var earliestStr, latestStr sql.NullString
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(timestamp), MAX(timestamp) FROM items WHERE source = ?`, src).
			Scan(&ss.ItemCount, &earliestStr, &latestStr)
		if earliestStr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, earliestStr.String); err == nil {
				ss.EarliestItem = &t
			}
		}
		if latestStr.Valid {
			if t, err := time.Parse(time.RFC3339Nano, latestStr.String); err == nil {
				ss.LatestItem = &t
			}
		}
		sc, _ := s.GetSyncCursor(ctx, src)
		if sc != nil {
			ss.LastSyncedAt = &sc.LastSyncedAt
			ss.Status = sc.Status
			ss.LastError = sc.LastError
		}
		results = append(results, ss)
	}

	return results, nil
}

// DB returns the underlying raw *sql.DB connection.
func (s *Store) DB() *sql.DB {
	return s.db
}

// GetAuthToken retrieves a stored OAuth or bearer token for a source.
func (s *Store) GetAuthToken(ctx context.Context, source string) (*AuthToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT source, access_token, refresh_token, token_type, scope, expires_at
		FROM auth_tokens
		WHERE source = ?;
	`, source)

	var tok AuthToken
	var expStr string
	err := row.Scan(&tok.Source, &tok.AccessToken, &tok.RefreshToken, &tok.TokenType, &tok.Scope, &expStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query auth_tokens for %s: %w", source, err)
	}

	if t, err := time.Parse(time.RFC3339Nano, expStr); err == nil {
		tok.ExpiresAt = t.UTC()
	} else if t, err := time.Parse(time.RFC3339, expStr); err == nil {
		tok.ExpiresAt = t.UTC()
	}

	return &tok, nil
}

// SetAuthToken saves or updates an OAuth or bearer token for a source.
func (s *Store) SetAuthToken(ctx context.Context, tok AuthToken) error {
	expStr := tok.ExpiresAt.UTC().Format(time.RFC3339Nano)
	if tok.ExpiresAt.IsZero() {
		expStr = time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_tokens (source, access_token, refresh_token, token_type, scope, expires_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source) DO UPDATE SET
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			token_type = excluded.token_type,
			scope = excluded.scope,
			expires_at = excluded.expires_at,
			updated_at = CURRENT_TIMESTAMP;
	`, tok.Source, tok.AccessToken, tok.RefreshToken, tok.TokenType, tok.Scope, expStr)

	return err
}

