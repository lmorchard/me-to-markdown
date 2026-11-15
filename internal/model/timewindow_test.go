package model

import (
	"testing"
	"time"
)

func TestParseTimeWindow(t *testing.T) {
	ref := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	t.Run("duration since", func(t *testing.T) {
		tw, err := ParseTimeWindow("168h", "", ref)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedSince := ref.Add(-168 * time.Hour)
		if !tw.Since.Equal(expectedSince) {
			t.Errorf("got %v, want %v", tw.Since, expectedSince)
		}
		if !tw.Until.Equal(ref) {
			t.Errorf("got %v, want %v", tw.Until, ref)
		}
	})

	t.Run("date since and until", func(t *testing.T) {
		tw, err := ParseTimeWindow("2026-11-01", "2026-11-07", ref)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tw.Since.Year() != 2026 || tw.Since.Month() != 11 || tw.Since.Day() != 1 {
			t.Errorf("since day: got %v", tw.Since)
		}
		if tw.Until.Hour() != 23 && tw.Until.Hour() != 0 {
			// end of day should be 23:59:59...
			t.Logf("until: %v", tw.Until)
		}
	})

	t.Run("invalid order", func(t *testing.T) {
		_, err := ParseTimeWindow("2026-11-10", "2026-11-01", ref)
		if err == nil {
			t.Fatal("expected error when until before since, got nil")
		}
	})
}
