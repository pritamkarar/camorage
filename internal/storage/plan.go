package storage

import (
	"sort"
	"time"

	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/motion"
)

// Clip is a piece of recording to copy to the cloud.
type Clip struct {
	Cam      string
	From, To time.Time
	Path     string // ClipPath
}

// Due lists the clips of cam that should be in the cloud by now, oldest first (spec §5.5):
//   - continuous: each clock hour (phone-local) once now ≥ its end + 2 min, the first from since;
//   - motion: each closed motion event's [start − preRoll, end + postRoll] once now ≥ its end +
//     1 min; a window starts no earlier than the previous one ended. events are oldest first.
//
// MediaMTX's /get stops at the first gap in a recording, so every window is cut into one clip per
// recorded piece of spans. Pieces under a second, and pieces ending by since, are skipped.
func Due(cam config.Camera, spans []mediamtx.Span, events []motion.Event, since, now time.Time, zone *time.Location) []Clip {
	var windows [][2]time.Time
	kind := KindHour
	if cam.Mode == "motion" {
		kind = KindMotion
		pre := time.Duration(cam.Motion.PreRollSec) * time.Second
		post := time.Duration(cam.Motion.PostRollSec) * time.Second
		var prev time.Time // end of the previous event's window
		for _, e := range events {
			if e.Open || e.Source != "phone" {
				continue
			}
			from, to := later(e.Start.Add(-pre), prev), e.End.Add(post)
			prev = to
			if !now.Before(to.Add(time.Minute)) {
				windows = append(windows, [2]time.Time{from, to})
			}
		}
	} else {
		s := since.In(zone)
		for h := time.Date(s.Year(), s.Month(), s.Day(), s.Hour(), 0, 0, 0, zone); !now.Before(h.Add(time.Hour + 2*time.Minute)); h = h.Add(time.Hour) {
			windows = append(windows, [2]time.Time{later(h, since), h.Add(time.Hour)}) // footage before since stays local
		}
	}
	var out []Clip
	for _, w := range windows {
		for _, sp := range spans {
			from, to := later(w[0], sp.Start), earlier(w[1], sp.Start.Add(sp.Duration))
			if to.Sub(from) < time.Second || !to.After(since) {
				continue
			}
			secs := int(to.Sub(from).Round(time.Second) / time.Second)
			out = append(out, Clip{Cam: cam.ID, From: from, To: to, Path: ClipPath(cam.ID, from.In(zone), kind, secs)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From.Before(out[j].From) })
	return out
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
