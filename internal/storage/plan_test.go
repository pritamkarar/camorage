package storage

import (
	"strings"
	"testing"
	"time"

	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/motion"
)

func at(h, m, s int) time.Time { return time.Date(2026, 10, 1, h, m, s, 0, ist) }

func span(from, to time.Time) mediamtx.Span {
	return mediamtx.Span{Start: from, Duration: to.Sub(from)}
}

func paths(clips []Clip) string {
	var out []string
	for _, c := range clips {
		out = append(out, strings.TrimPrefix(c.Path, "camorage/gate/2026-10-01/"))
	}
	return strings.Join(out, " ")
}

var continuous = config.Camera{ID: "gate", Mode: "continuous"}

func TestDueCutsHoursAtGaps(t *testing.T) {
	spans := []mediamtx.Span{span(at(9, 0, 0), at(9, 40, 0)), span(at(9, 40, 10), at(10, 30, 0)), span(at(10, 30, 5), at(10, 30, 5).Add(500*time.Millisecond))}
	got := Due(continuous, spans, nil, at(8, 0, 0), at(10, 3, 0), ist)
	// hour 9 is due (10:00 + 2 min ≤ 10:03), cut at the 10 s gap; hour 10 is not due yet
	if p := paths(got); p != "09-00-00_hour_2400s.mp4 09-40-10_hour_1190s.mp4" {
		t.Fatalf("got %s", p)
	}
	if !got[1].From.Equal(at(9, 40, 10)) || !got[1].To.Equal(at(10, 0, 0)) || got[1].Cam != "gate" {
		t.Fatalf("clip %+v", got[1])
	}
	// at 11:05 hour 10 is due too; its half-second piece is skipped
	if p := paths(Due(continuous, spans, nil, at(8, 0, 0), at(11, 5, 0), ist)); p != "09-00-00_hour_2400s.mp4 09-40-10_hour_1190s.mp4 10-00-00_hour_1800s.mp4" {
		t.Fatalf("got %s", p)
	}
}

func TestDueUsesLocalClockHours(t *testing.T) {
	// IST is UTC+05:30: the hour starting 09:00 IST, not 08:30
	got := Due(continuous, []mediamtx.Span{span(at(8, 50, 0), at(9, 10, 0))}, nil, at(8, 40, 0), at(10, 3, 0), ist)
	if p := paths(got); p != "08-50-00_hour_600s.mp4 09-00-00_hour_600s.mp4" {
		t.Fatalf("got %s", p)
	}
}

func TestDueStartsAtSince(t *testing.T) {
	spans := []mediamtx.Span{span(at(6, 0, 0), at(10, 0, 0))}
	// cloud copy was switched on at 08:20: footage from before it stays on the phone (spec §4)
	if p := paths(Due(continuous, spans, nil, at(8, 20, 0), at(10, 5, 0), ist)); p != "08-20-00_hour_2400s.mp4 09-00-00_hour_3600s.mp4" {
		t.Fatalf("got %s", p)
	}
}

func TestDueMotionEvents(t *testing.T) {
	cam := config.Camera{ID: "gate", Mode: "motion", Motion: config.Motion{PreRollSec: 10, PostRollSec: 30}}
	spans := []mediamtx.Span{span(at(9, 0, 0), at(11, 0, 0))}
	evs := []motion.Event{ // oldest first, as motion.Manager.Events returns them
		{Start: at(9, 30, 0), End: at(9, 40, 0), Source: "blind"},  // stays on the phone
		{Start: at(10, 0, 0), End: at(10, 0, 20), Source: "phone"}, // window 09:59:50–10:00:50, due 10:01:50
		{Start: at(10, 0, 55), End: at(10, 1, 5), Source: "phone"}, // 10:00:45–10:01:35, from 10:00:50: once
		{Start: at(10, 1, 50), End: at(10, 1, 50), Source: "phone", Open: true},
	}
	if got := Due(cam, spans, evs, at(9, 0, 0), at(10, 1, 49), ist); len(got) != 0 {
		t.Fatalf("early: %s", paths(got))
	}
	if p := paths(Due(cam, spans, evs, at(9, 0, 0), at(10, 1, 50), ist)); p != "09-59-50_motion_60s.mp4" {
		t.Fatalf("got %s", p)
	}
	// the second window is due at 10:02:35, and starts where the first ended
	if p := paths(Due(cam, spans, evs, at(9, 0, 0), at(10, 2, 35), ist)); p != "09-59-50_motion_60s.mp4 10-00-50_motion_45s.mp4" {
		t.Fatalf("got %s", p)
	}
	// an event that began before cloud copy was switched on but ended after it is uploaded whole
	if p := paths(Due(cam, spans, evs[:2], at(10, 0, 30), at(10, 5, 0), ist)); p != "09-59-50_motion_60s.mp4" {
		t.Fatalf("got %s", p)
	}
}

func TestDueAfterSwitchToContinuousSkipsKeptMotionFootage(t *testing.T) {
	// a motion camera's kept minutes (already in the cloud as event clips) stay on the phone after a
	// switch to continuous at 09:30; cloud.since moved to the switch, so they do not go up again as
	// hours, not even the minutes of the recording that runs on across the switch
	spans := []mediamtx.Span{span(at(9, 14, 0), at(9, 16, 0)), span(at(9, 28, 0), at(12, 0, 0))}
	if p := paths(Due(continuous, spans, nil, at(9, 30, 0), at(12, 3, 0), ist)); p != "09-30-00_hour_1800s.mp4 10-00-00_hour_3600s.mp4 11-00-00_hour_3600s.mp4" {
		t.Fatalf("got %s", p)
	}
}
