package model

import (
	"fmt"
	"strings"
	"time"
)

// TimeWindow represents a start and end time range.
type TimeWindow struct {
	Since time.Time
	Until time.Time
}

// ParseTime parses s as a duration ("168h"), RFC3339 timestamp, or YYYY-MM-DD.
// If endOfDay is true and s is YYYY-MM-DD, the time is set to 23:59:59.999999999.
func ParseTime(s string, ref time.Time, endOfDay bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time value")
	}
	if strings.HasSuffix(s, "d") {
		if days, err := time.ParseDuration(strings.TrimSuffix(s, "d") + "h"); err == nil {
			return ref.Add(-days * 24), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return ref.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q (expected YYYY-MM-DD, RFC3339, or Go duration)", s)
}

// ParseTimeWindow parses since and until strings against a reference time.
// If until is empty, it defaults to ref (or end of today if since was a date).
func ParseTimeWindow(sinceStr, untilStr string, ref time.Time) (TimeWindow, error) {
	if sinceStr == "" {
		return TimeWindow{}, fmt.Errorf("since time is required")
	}
	since, err := ParseTime(sinceStr, ref, false)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("parse since: %w", err)
	}

	until := ref
	if untilStr != "" {
		parsedUntil, err := ParseTime(untilStr, ref, true)
		if err != nil {
			return TimeWindow{}, fmt.Errorf("parse until: %w", err)
		}
		until = parsedUntil
	}

	if until.Before(since) {
		return TimeWindow{}, fmt.Errorf("until (%s) cannot be before since (%s)", until.Format(time.RFC3339), since.Format(time.RFC3339))
	}

	return TimeWindow{Since: since, Until: until}, nil
}

// Contains returns true if t falls within [Since, Until].
func (tw TimeWindow) Contains(t time.Time) bool {
	utc := t.UTC()
	return !utc.Before(tw.Since) && !utc.After(tw.Until)
}
