package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"camorage/internal/motion"
)

type fakeMotion struct {
	evs            []motion.Event
	active         map[string]bool
	gotFrom, gotTo time.Time
}

func (f *fakeMotion) Events(_ string, from, to time.Time) ([]motion.Event, error) {
	f.gotFrom, f.gotTo = from, to
	return f.evs, nil
}
func (f *fakeMotion) Active(cam string) bool { return f.active[cam] }

func TestMotionEventsAndStatus(t *testing.T) {
	ten := time.Date(2026, 9, 30, 4, 30, 0, 0, time.UTC) // 10:00 IST
	fm := &fakeMotion{
		evs: []motion.Event{
			{Start: ten, End: ten.Add(20 * time.Second), Peak: 7},
			{Start: ten.Add(time.Hour), End: ten.Add(time.Hour), Peak: 3, Open: true},
		},
		active: map[string]bool{"front-gate": true},
	}
	e := newEnv(t, func(d *Deps) { d.Motion = fm })
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()

	w := e.do("GET", "/api/playback/events?cam=front-gate&date=2026-09-30", "")
	want := `[{"start":"2026-09-30T10:00:00+05:30","end":"2026-09-30T10:00:20+05:30","peak":7},{"start":"2026-09-30T11:00:00+05:30","end":"2026-09-30T11:00:00+05:30","peak":3,"open":true}]`
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("events: %d %s", w.Code, w.Body)
	}
	if fm.gotFrom.Format(time.RFC3339) != "2026-09-30T00:00:00+05:30" || !fm.gotTo.Equal(fm.gotFrom.AddDate(0, 0, 1)) {
		t.Fatalf("asked for %v – %v", fm.gotFrom, fm.gotTo)
	}
	if w := e.do("GET", "/api/playback/events?cam=nope&date=2026-09-30", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown camera: %d", w.Code)
	}
	if w := e.do("GET", "/api/playback/events?cam=front-gate&date=30-09-2026", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad date: %d", w.Code)
	}
	st := decode[struct {
		Cameras []struct {
			ID     string `json:"id"`
			Motion bool   `json:"motion"`
		} `json:"cameras"`
	}](t, e.do("GET", "/api/status", ""))
	if len(st.Cameras) != 1 || !st.Cameras[0].Motion {
		t.Fatalf("status: %+v", st)
	}
}

func TestMotionSinceFollowsModeSwitches(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	since := func() *time.Time {
		t.Helper()
		cfg := e.store.Get()
		c, _ := cfg.CameraByID("front-gate")
		return c.MotionSince
	}
	if since() != nil {
		t.Fatal("a continuous camera has motionSince")
	}
	motionGate := strings.Replace(gate, "{", `{"mode":"motion",`, 1)
	e.do("PUT", "/api/cameras/front-gate", motionGate)
	e.waitChanged()
	first := since()
	if first == nil || !first.Equal(e.now) {
		t.Fatalf("switching to motion: %v", first)
	}
	e.now = e.now.Add(time.Hour)
	forged := strings.Replace(gate, "{", `{"mode":"motion","motionSince":"2020-01-01T00:00:00Z",`, 1)
	e.do("PUT", "/api/cameras/front-gate", forged) // still motion; the client's value is ignored
	e.waitChanged()
	if s := since(); s == nil || !s.Equal(*first) {
		t.Fatalf("staying in motion mode moved motionSince to %v", s)
	}
	e.do("PUT", "/api/cameras/front-gate", gate)
	e.waitChanged()
	if since() != nil {
		t.Fatal("back to continuous kept motionSince")
	}
	w := e.do("POST", "/api/cameras", strings.Replace(motionGate, "Front Gate", "Back Door", 1))
	e.waitChanged()
	cfg := e.store.Get()
	if c, _ := cfg.CameraByID("back-door"); w.Code != http.StatusCreated || c.MotionSince == nil || !c.MotionSince.Equal(e.now) {
		t.Fatalf("new motion camera: %d %+v", w.Code, c.MotionSince)
	}
}

func TestBlindPeriodsAreNotMotionMarks(t *testing.T) {
	ten := time.Date(2026, 9, 30, 4, 30, 0, 0, time.UTC)
	fm := &fakeMotion{evs: []motion.Event{
		{Start: ten, End: ten.Add(time.Minute), Source: "blind"},
		{Start: ten.Add(time.Hour), End: ten.Add(time.Hour), Peak: 3, Source: "phone"},
	}}
	e := newEnv(t, func(d *Deps) { d.Motion = fm })
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	w := e.do("GET", "/api/playback/events?cam=front-gate&date=2026-09-30", "")
	if got := strings.TrimSpace(w.Body.String()); got != `[{"start":"2026-09-30T11:00:00+05:30","end":"2026-09-30T11:00:00+05:30","peak":3}]` {
		t.Fatalf("events: %s", got)
	}
}
