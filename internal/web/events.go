package web

import (
	"net/http"
	"time"

	"camorage/internal/motion"
)

// Motion is the part of the motion manager the API uses.
type Motion interface {
	Events(cam string, from, to time.Time) ([]motion.Event, error)
	Active(cam string) bool
}

type eventOut struct {
	Start string `json:"start"` // RFC3339 in phone local time
	End   string `json:"end"`
	Peak  int    `json:"peak"`
	Open  bool   `json:"open,omitempty"`
}

// events lists a camera's motion events for one local calendar day (timeline marks).
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	cam, day, ok := s.dayQuery(w, r)
	if !ok {
		return
	}
	var evs []motion.Event
	if s.d.Motion != nil {
		var err error
		if evs, err = s.d.Motion.Events(cam, day, day.AddDate(0, 0, 1)); err != nil {
			fail(w, http.StatusInternalServerError, "reading motion events: "+err.Error())
			return
		}
	}
	out := []eventOut{}
	for _, e := range evs {
		if e.Source == "blind" { // recording without detection: kept footage, but not motion
			continue
		}
		out = append(out, eventOut{
			Start: e.Start.In(s.d.Zone).Format(time.RFC3339), End: e.End.In(s.d.Zone).Format(time.RFC3339),
			Peak: e.Peak, Open: e.Open,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
