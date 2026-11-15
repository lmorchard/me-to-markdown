package model

import "time"

// SyncCursor records the sync position and status for an individual source.
type SyncCursor struct {
	Source       string    `json:"source"`
	Cursor       string    `json:"cursor"`
	LastSyncedAt time.Time `json:"last_synced_at"`
	Status       string    `json:"status"` // "ok", "error"
	LastError    string    `json:"last_error,omitempty"`
}
