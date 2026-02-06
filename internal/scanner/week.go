package scanner

import "time"

// WeekStart returns the Monday 00:00 UTC of the ISO week containing t.
func WeekStart(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	weekday := t.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	return t.AddDate(0, 0, -int(weekday-time.Monday))
}
