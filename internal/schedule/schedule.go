// Package schedule decides whether a camera's weekly windows cover a moment.
package schedule

import (
	"time"

	"camorage/internal/config"
)

// Active reports whether t (read in t's own location — pass phone local time) falls in any
// window. No windows means always active. A window whose End <= Start crosses midnight and
// belongs to the day it starts on.
func Active(ws []config.Window, t time.Time) bool {
	if len(ws) == 0 {
		return true
	}
	now := t.Hour()*60 + t.Minute()
	today := isoDay(t.Weekday())
	yesterday := today - 1
	if yesterday == 0 {
		yesterday = 7
	}
	for _, w := range ws {
		start, ok1 := minutes(w.Start)
		end, ok2 := minutes(w.End)
		if !ok1 || !ok2 {
			continue
		}
		if end > start {
			if has(w.Days, today) && now >= start && now < end {
				return true
			}
			continue
		}
		if has(w.Days, today) && now >= start { // evening part of today's window
			return true
		}
		if has(w.Days, yesterday) && now < end { // morning part of yesterday's window
			return true
		}
	}
	return false
}

func isoDay(d time.Weekday) int {
	if d == time.Sunday {
		return 7
	}
	return int(d)
}

func minutes(hhmm string) (int, bool) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func has(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}
