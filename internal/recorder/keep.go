package recorder

import (
	"time"

	"camorage/internal/motion"
)

// EventsFunc returns a camera's motion events overlapping [from, to), the one in progress marked Open.
type EventsFunc func(cam string, from, to time.Time) ([]motion.Event, error)

const (
	// recentBuffer is how long motion-mode footage that no event keeps stays on the phone (spec §5.3).
	recentBuffer = time.Hour
	// segCap bounds a segment's assumed length when the next one starts much later (recording paused).
	segCap = 2 * time.Minute
)

// Kept reports whether segment [s0, s1) holds motion or its pre/post roll: some event E with
// E.Start − pre < s1 and (E open or E.End + post > s0) (spec §5.3).
func Kept(s0, s1 time.Time, evs []motion.Event, pre, post time.Duration) bool {
	for _, e := range evs {
		if e.Start.Add(-pre).Before(s1) && (e.Open || e.End.Add(post).After(s0)) {
			return true
		}
	}
	return false
}
