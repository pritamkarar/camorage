package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"camorage/internal/mediamtx"
)

// MediaMTX's /get finds nothing for a start even a fraction of a second before a recording, and
// clients only ever see span starts rounded to the second (found by the M1b end-to-end test).
func TestVideoSnapsStartIntoTheRecording(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "VIDEO") }))
	defer upstream.Close()
	e := newEnv(t)
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	e.mtx.videoURL = upstream.URL + "/get"
	exact := time.Date(2026, 9, 30, 9, 15, 53, 223427000, time.UTC) // 14:45:53.223427 IST
	e.mtx.spans = []mediamtx.Span{{Start: exact, Duration: 10 * time.Minute}}

	for _, q := range []string{
		"2026-09-30T14:45:53%2B05:30", // the span's start as the spans API reports it (rounded down)
		"2026-09-30T14:40:00%2B05:30", // in the gap just before the recording
	} {
		if w := e.do("GET", "/api/playback/video?cam=front-gate&duration=30&start="+q, ""); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", q, w.Code, w.Body)
		}
		if !e.mtx.videoStart.Equal(exact) {
			t.Fatalf("start %s went to MediaMTX as %v, want the recording's exact start %v", q, e.mtx.videoStart, exact)
		}
	}

	e.do("GET", "/api/playback/video?cam=front-gate&duration=30&start=2026-09-30T14:50:00%2B05:30", "")
	if want := time.Date(2026, 9, 30, 9, 20, 0, 0, time.UTC); !e.mtx.videoStart.Equal(want) {
		t.Fatalf("a start inside the recording must be kept: got %v, want %v", e.mtx.videoStart, want)
	}
}
