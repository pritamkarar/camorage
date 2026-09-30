package web

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"camorage/internal/mediamtx"
)

type spanOut struct {
	Start       string  `json:"start"` // RFC3339 in phone local time
	DurationSec float64 `json:"durationSec"`
}

func (s *server) knownCamera(w http.ResponseWriter, id string) bool {
	cfg := s.d.Store.Get()
	if _, found := cfg.CameraByID(id); !found {
		fail(w, http.StatusNotFound, "no such camera")
		return false
	}
	return true
}

// dayQuery reads ?cam=<known camera>&date=YYYY-MM-DD (a phone-local day) and answers errors itself.
func (s *server) dayQuery(w http.ResponseWriter, r *http.Request) (string, time.Time, bool) {
	q := r.URL.Query()
	cam := q.Get("cam")
	if !s.knownCamera(w, cam) {
		return "", time.Time{}, false
	}
	day, err := time.ParseInLocation("2006-01-02", q.Get("date"), s.d.Zone)
	if err != nil {
		fail(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return "", time.Time{}, false
	}
	return cam, day, true
}

// spans lists recorded spans of a camera for one local calendar day.
func (s *server) spans(w http.ResponseWriter, r *http.Request) {
	cam, day, ok := s.dayQuery(w, r)
	if !ok {
		return
	}
	spans, err := s.d.MTX.Spans(r.Context(), cam, day, day.AddDate(0, 0, 1)) // local midnight to local midnight
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]spanOut, 0, len(spans))
	for _, sp := range spans {
		out = append(out, spanOut{Start: sp.Start.In(s.d.Zone).Format(time.RFC3339), DurationSec: sp.Duration.Seconds()})
	}
	writeJSON(w, http.StatusOK, out)
}

// snapStart moves t forward to the exact start of the first recorded span that ends after t, if
// t falls before it. MediaMTX's /get finds nothing for a start that precedes a recording even by
// a fraction of a second, and clients only see span starts rounded to the second.
func snapStart(spans []mediamtx.Span, t time.Time) time.Time {
	for _, sp := range spans {
		if sp.Start.Add(sp.Duration).After(t) {
			if t.Before(sp.Start) {
				return sp.Start
			}
			return t
		}
	}
	return t
}

// video streams [start, start+duration) from MediaMTX's playback server.
func (s *server) video(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cam := q.Get("cam")
	if !s.knownCamera(w, cam) {
		return
	}
	start, err := time.Parse(time.RFC3339, q.Get("start"))
	if err != nil {
		fail(w, http.StatusBadRequest, "start must be RFC3339, e.g. 2026-09-30T12:52:34+05:30")
		return
	}
	secs, err := strconv.Atoi(q.Get("duration"))
	if err != nil || secs < 1 || secs > 3600 {
		fail(w, http.StatusBadRequest, "duration must be 1-3600 seconds")
		return
	}
	format := q.Get("format")
	if format == "" {
		format = "fmp4"
	}
	if format != "fmp4" && format != "mp4" {
		fail(w, http.StatusBadRequest, "format must be fmp4 or mp4")
		return
	}
	if spans, err := s.d.MTX.Spans(r.Context(), cam, start.Add(-time.Hour), start.Add(time.Duration(secs)*time.Second)); err == nil {
		start = snapStart(spans, start)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.d.MTX.VideoURL(cam, start, time.Duration(secs)*time.Second, format), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp, err := s.d.HTTP.Do(req)
	if err != nil {
		fail(w, http.StatusBadGateway, "playback server: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		code := http.StatusBadGateway
		if resp.StatusCode == http.StatusNotFound {
			code = http.StatusNotFound
		}
		fail(w, code, "playback server: "+string(b))
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	if format == "mp4" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s_%s.mp4"`, cam, start.In(s.d.Zone).Format("2006-01-02_15-04-05")))
	}
	_, _ = io.Copy(w, resp.Body)
}
