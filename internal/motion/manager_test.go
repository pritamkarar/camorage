package motion

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"camorage/internal/config"
	"camorage/internal/supervisor"
)

type fakeSup struct {
	mu      sync.Mutex
	started map[string]supervisor.Spec
	log     []string
}

func (f *fakeSup) Start(s supervisor.Spec) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started[s.Name] = s
	f.log = append(f.log, "start "+s.Name)
}

func (f *fakeSup) Stop(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.started, name)
	f.log = append(f.log, "stop "+name)
}

// newMgr returns a manager whose clock advances 2 s per frame, like keyframes, from t0.
func newMgr(t *testing.T) (*Manager, *fakeSup) {
	sup := &fakeSup{started: map[string]supervisor.Spec{}}
	clk := t0
	return &Manager{
		Sup: sup, FFmpeg: "ffmpeg", FFmpegVersion: [2]int{4, 2}, RTSP: "127.0.0.1:8554",
		Store: &Store{Dir: t.TempDir(), Zone: ist}, Logf: t.Logf,
		Now: func() time.Time { clk = clk.Add(2 * time.Second); return clk },
	}, sup
}

// feed plays frames through a reader's stdout consumer, as ffmpeg would.
func feed(t *testing.T, sup *fakeSup, name string, frames ...[]byte) {
	t.Helper()
	spec, ok := sup.started[name]
	if !ok {
		t.Fatalf("no reader %s", name)
	}
	spec.Stdout(bytes.NewReader(bytes.Join(frames, nil)))
}

var gate = config.Camera{ID: "gate", Enabled: true, Mode: "motion", SubURL: "rtsp://cam/sub",
	Motion: config.Motion{Source: "phone", Sensitivity: "medium", PreRollSec: 10, PostRollSec: 30}}

func TestManagerReadsTheSubstreamAndRecordsEvents(t *testing.T) {
	m, sup := newMgr(t)
	m.Apply([]config.Camera{gate, {ID: "yard", Enabled: true, Mode: "continuous"}})
	if got := strings.Join(sup.log, ", "); got != "start motion-gate" {
		t.Fatalf("readers: %s", got)
	}
	args := strings.Join(sup.started["motion-gate"].Args, " ")
	if !strings.Contains(args, "-i rtsp://127.0.0.1:8554/gate_sub ") || !strings.Contains(args, "-stimeout") {
		t.Fatalf("args: %s", args)
	}
	bg := frame(100)
	person := paint(bg, 200, 40, 41, 56, 57)
	feed(t, sup, "motion-gate", bg, person, person, bg) // frames at +2 s … +8 s: motion at +4 and +8
	if !m.Active("gate") || m.Active("yard") {
		t.Fatal("Active wrong")
	}
	evs, _ := m.Events("gate", t0, at(3600))
	if len(evs) != 1 || !evs[0].Open || !evs[0].Start.Equal(at(4)) || !evs[0].End.Equal(at(8)) {
		t.Fatalf("events: %+v", evs)
	}
	stored, _ := m.Store.Between("gate", t0, at(3600)) // written when it opened
	if len(stored) != 1 || !stored[0].Start.Equal(at(4)) {
		t.Fatalf("stored: %+v", stored)
	}
	m.Tick(at(37)) // 29 s after the last motion: still open
	if !m.Active("gate") {
		t.Fatal("closed early")
	}
	m.Tick(at(38)) // no frames for the post-roll (camera offline): closes
	if m.Active("gate") {
		t.Fatal("still open")
	}
	stored, _ = m.Store.Between("gate", t0, at(3600))
	stored = phoneOnly(stored) // no frames since +8 s: a blind event follows (TestBlindDetectionKeepsFootage)
	if len(stored) != 1 || !stored[0].End.Equal(at(8)) || stored[0].Open {
		t.Fatalf("stored after close: %+v", stored)
	}
}

func TestManagerIgnoresMotionOutsideTheSchedule(t *testing.T) {
	m, sup := newMgr(t)
	night := gate
	night.Schedule = []config.Window{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "20:00", End: "06:00"}} // t0 is 09:00
	m.Apply([]config.Camera{night})
	bg := frame(100)
	feed(t, sup, "motion-gate", bg, paint(bg, 200, 40, 41, 56, 57), bg)
	if m.Active("gate") || len(must(m.Events("gate", t0, at(3600)))) != 0 {
		t.Fatal("motion outside the schedule became an event")
	}
}

func TestManagerApplyKeepsOrStopsReaders(t *testing.T) {
	m, sup := newMgr(t)
	m.Apply([]config.Camera{gate})
	high := gate
	high.Motion.Sensitivity = "high"
	m.Apply([]config.Camera{high}) // new settings, same stream: the reader keeps running
	bg := frame(100)
	feed(t, sup, "motion-gate", bg, paint(bg, 110, 40, 41)) // faint: only high sensitivity sees it
	if !m.Active("gate") {
		t.Fatal("the new sensitivity was not applied")
	}
	off := gate
	off.Mode = "continuous"
	m.Apply([]config.Camera{off})
	if got := strings.Join(sup.log, ", "); got != "start motion-gate, stop motion-gate" {
		t.Fatalf("readers: %s", got)
	}
	if m.Active("gate") {
		t.Fatal("a camera out of motion mode still has an event in progress")
	}
	if stored, _ := m.Store.Between("gate", t0, at(3600)); len(stored) != 1 {
		t.Fatalf("the event in progress was not written when motion mode ended: %+v", stored)
	}
}

func TestFlushWritesOpenEvents(t *testing.T) {
	m, sup := newMgr(t)
	m.Apply([]config.Camera{gate})
	bg := frame(100)
	person := paint(bg, 200, 40, 41, 56, 57)
	feed(t, sup, "motion-gate", bg, person, bg) // open at +4, extended to +6
	m.Flush()                                   // shutdown
	stored, _ := m.Store.Between("gate", t0, at(3600))
	if len(stored) != 1 || !stored[0].End.Equal(at(6)) {
		t.Fatalf("stored: %+v", stored)
	}
}

func must(evs []Event, err error) []Event {
	if err != nil {
		panic(err)
	}
	return evs
}

func TestLongEventIsSavedAsItGrows(t *testing.T) {
	m, sup := newMgr(t)
	m.Apply([]config.Camera{gate})
	bg := frame(100)
	person := paint(bg, 200, 40, 41, 56, 57)
	var frames [][]byte
	for i := 0; i < 300; i++ { // ten minutes of someone moving, a keyframe every 2 s
		if i%2 == 0 {
			frames = append(frames, bg)
		} else {
			frames = append(frames, person)
		}
	}
	feed(t, sup, "motion-gate", frames...)
	// The portal dies here (no Flush, no close): what is stored must cover nearly all of it,
	// or the janitor deletes the footage after the stored end.
	stored, _ := m.Store.Between("gate", t0, at(3600))
	live := must(m.Events("gate", t0, at(3600)))
	if len(stored) != 1 || live[0].End.Sub(stored[0].End) > 10*time.Second {
		t.Fatalf("stored %+v, real end %v", stored, live[0].End)
	}
}

func phoneOnly(evs []Event) []Event {
	var out []Event
	for _, e := range evs {
		if e.Source == "phone" {
			out = append(out, e)
		}
	}
	return out
}

func TestBlindDetectionKeepsFootage(t *testing.T) {
	m, sup := newMgr(t)
	m.Apply([]config.Camera{gate})
	m.Tick(at(0)) // the reader has not delivered a frame yet (substream down, ffmpeg missing, starting)
	m.Tick(at(10))
	if evs := must(m.Events("gate", t0, at(3600))); len(evs) != 0 {
		t.Fatalf("blind too early: %+v", evs)
	}
	m.Tick(at(20)) // recording, but no frames for 20 s: motion detection is blind
	evs := must(m.Events("gate", t0, at(3600)))
	if len(evs) != 1 || evs[0].Source != "blind" || !evs[0].Open || !evs[0].Start.Equal(t0) {
		t.Fatalf("events: %+v", evs)
	}
	if m.Active("gate") {
		t.Fatal("blindness shown as motion")
	}
	if stored, _ := m.Store.Between("gate", t0, at(3600)); len(stored) != 1 || stored[0].Source != "blind" {
		t.Fatalf("not stored when it began: %+v", stored)
	}
	m.Now = func() time.Time { return at(30) }
	feed(t, sup, "motion-gate", frame(100)) // frames are back
	stored, _ := m.Store.Between("gate", t0, at(3600))
	if len(stored) != 1 || !stored[0].End.Equal(at(30)) || len(must(m.Events("gate", t0, at(3600)))) != 1 || must(m.Events("gate", t0, at(3600)))[0].Open {
		t.Fatalf("blindness did not end with the first frame: %+v", stored)
	}

	// outside the schedule nothing is recorded, so nothing needs keeping
	m2, _ := newMgr(t)
	night := gate
	night.Schedule = []config.Window{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "20:00", End: "06:00"}}
	m2.Apply([]config.Camera{night})
	m2.Tick(at(0))
	m2.Tick(at(60))
	if evs := must(m2.Events("gate", t0, at(3600))); len(evs) != 0 {
		t.Fatalf("blind event outside the schedule: %+v", evs)
	}
}
