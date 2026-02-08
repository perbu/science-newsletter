package scanner

import "time"

// MonthStart returns the 1st day of the calendar month containing t, at 00:00 UTC.
func MonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
