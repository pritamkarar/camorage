package motion

import (
	"io"
	"sort"
	"sync"
	"time"

	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/schedule"
	"camorage/internal/supervisor"
)

// Supervisor is the part of *supervisor.Supervisor the manager uses.
type Supervisor interface {
	Start(supervisor.Spec)
	Stop(name string)
}

// Manager runs a keyframe reader for every camera in motion mode, turns its frames into events,
// and answers what the API and the janitor ask about them.
type Manager struct {
	Sup           Supervisor
	FFmpeg        string           // ffmpeg executable
	FFmpegVersion [2]int           // major, minor: picks the reader's flags
	RTSP          string           // MediaMTX's RTSP listener, e.g. 127.0.0.1:8554
	Store         *Store           // closed and in-progress events
	Now           func() time.Time // phone local time; stamps each frame as it arrives
	Logf          func(format string, args ...any)

	mu   sync.Mutex
	cams map[string]*camState
}

type camState struct {
	cam     config.Camera
	path    string // the MediaMTX path the reader decodes
	prev    []byte // the previous frame
	tracker Tracker
	saved   time.Time // End of the event in progress when it was last written

	lastFrame  time.Time // when the reader last delivered a frame (or the first Tick)
	blind      *Event    // recording while no frames arrive: kept like motion (Source "blind")
	blindSaved time.Time // when the blind period was last written
}

// A camera recording without frames for blindAfter is "blind": nothing can say its footage holds
// no motion, so the time is stored as a blind event and the janitor keeps that footage.
// ponytail: a crash while blind loses up to blindSaveEvery of it; save more often if that matters.
const (
	blindAfter     = 15 * time.Second
	blindSaveEvery = time.Minute
)

type pending struct {
	cam string
	ev  Event
}

// saveEvery is how often an event in progress is written again as it grows: a crash then loses
// at most this much of its end (and the janitor keeps the footage up to the stored end).
const saveEvery = 10 * time.Second

// readerName names a camera's reader in the supervisor and its log file.
func readerName(id string) string { return "motion-" + id }

// readPath is the stream motion looks at: the substream if there is one (much cheaper to decode).
func readPath(c config.Camera) string {
	if c.SubURL != "" {
		return mediamtx.SubPath(c.ID)
	}
	return c.ID
}

func postRoll(c config.Camera) time.Duration {
	return time.Duration(c.Motion.PostRollSec) * time.Second
}

// Apply runs a reader for every enabled camera in motion mode and stops the others. A camera
// whose settings changed keeps its reader and its event in progress; only a new stream path
// restarts the reader. A camera leaving motion mode has its event in progress written.
// ponytail: readers run outside the schedule too (frames there count as no motion); stop them
// outside the schedule if 3 % of a core per camera ever matters.
func (m *Manager) Apply(cams []config.Camera) {
	want := map[string]config.Camera{}
	for _, c := range cams {
		if c.Enabled && c.Mode == "motion" {
			want[c.ID] = c
		}
	}
	var stop []string
	var start []supervisor.Spec
	var flush []pending
	m.mu.Lock()
	if m.cams == nil {
		m.cams = map[string]*camState{}
	}
	for id, st := range m.cams {
		if c, ok := want[id]; ok && readPath(c) == st.path {
			continue
		}
		stop = append(stop, readerName(id))
		for _, e := range st.open() {
			flush = append(flush, pending{id, e})
		}
		delete(m.cams, id)
	}
	for id, c := range want {
		if st, ok := m.cams[id]; ok {
			st.cam, st.tracker.PostRoll = c, postRoll(c)
			continue
		}
		m.cams[id] = &camState{cam: c, path: readPath(c), tracker: Tracker{PostRoll: postRoll(c)}}
		start = append(start, m.spec(id, readPath(c)))
	}
	m.mu.Unlock()
	for _, n := range stop {
		m.Sup.Stop(n)
	}
	for _, s := range start {
		m.Sup.Start(s)
	}
	for _, p := range flush {
		m.save(p.cam, &p.ev)
	}
}

func (m *Manager) spec(id, path string) supervisor.Spec {
	return supervisor.Spec{
		Name: readerName(id),
		Path: m.FFmpeg,
		Args: ReaderArgs(m.FFmpegVersion[0], m.FFmpegVersion[1], "rtsp://"+m.RTSP+"/"+path),
		Stdout: func(r io.Reader) {
			if err := ReadFrames(r, func(f []byte) { m.frame(id, f) }); err != nil {
				m.logf("motion: %s: %v", id, err)
			}
		},
	}
}

// frame runs detection on one keyframe of camera id and records what changed.
func (m *Manager) frame(id string, f []byte) {
	now := m.Now()
	m.mu.Lock()
	st := m.cams[id]
	if st == nil {
		m.mu.Unlock()
		return
	}
	st.lastFrame = now
	var sighted *Event // the blind period that this frame ends
	if st.blind != nil {
		st.blind.End = now
		sighted, st.blind = st.blind, nil
	}
	var r Result
	// Outside the schedule nothing is recorded, so motion there is no event (spec §5.1).
	if st.prev != nil && schedule.Active(st.cam.Schedule, now) {
		r = Detect(st.prev, f, st.cam.Motion.Sensitivity, st.cam.Motion.Ignore)
	}
	st.prev = append(st.prev[:0], f...)
	wasOpen := st.tracker.Open() != nil
	closed := st.tracker.Frame(now, r)
	var write *Event // written when it opens and then every saveEvery as it grows
	if o := st.tracker.Open(); o != nil && (!wasOpen || closed != nil || o.End.Sub(st.saved) >= saveEvery) {
		write, st.saved = o, o.End
	}
	m.mu.Unlock()
	m.save(id, sighted)
	m.save(id, closed)
	m.save(id, write)
}

// Tick closes events whose post-roll ran out without frames (camera offline, reader restarting),
// and records blind periods: recording while no frames arrive.
func (m *Manager) Tick(now time.Time) {
	var saves []pending
	m.mu.Lock()
	for id, st := range m.cams {
		if e := st.tracker.Tick(now); e != nil {
			saves = append(saves, pending{id, *e})
		}
		if st.lastFrame.IsZero() {
			st.lastFrame = now // a new reader's clock starts with the first tick
		}
		switch {
		case st.blind != nil:
			st.blind.End = now
			if now.Sub(st.blindSaved) >= blindSaveEvery {
				st.blindSaved = now
				saves = append(saves, pending{id, *st.blind})
			}
		case now.Sub(st.lastFrame) >= blindAfter && schedule.Active(st.cam.Schedule, now):
			st.blind = &Event{Start: st.lastFrame, End: now, Source: "blind"}
			st.blindSaved = now
			saves = append(saves, pending{id, *st.blind})
		}
	}
	m.mu.Unlock()
	for _, p := range saves {
		m.save(p.cam, &p.ev)
	}
}

// Events returns cam's events overlapping [from, to), oldest first, including the one in progress
// (marked Open). An error means the stored events could not be read: callers must not take the
// list as complete.
func (m *Manager) Events(cam string, from, to time.Time) ([]Event, error) {
	evs, err := m.Store.Between(cam, from, to)
	m.mu.Lock()
	var open []Event
	if st := m.cams[cam]; st != nil {
		open = st.open()
	}
	m.mu.Unlock()
	for _, o := range open {
		if !o.Start.Before(to) {
			continue
		}
		stored := false
		for i := range evs {
			if evs[i].Start.Equal(o.Start) && evs[i].Source == o.Source { // its earlier lines are stored too
				evs[i], stored = o, true
			}
		}
		if !stored {
			evs = append(evs, o)
		}
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].Start.Before(evs[j].Start) })
	return evs, err
}

// open returns the camera's events in progress, marked Open: motion and a blind period.
func (st *camState) open() []Event {
	var out []Event
	if o := st.tracker.Open(); o != nil {
		out = append(out, *o)
	}
	if st.blind != nil {
		b := *st.blind
		b.Open = true
		out = append(out, b)
	}
	return out
}

// Active reports whether cam has an event in progress (the Live badge).
func (m *Manager) Active(cam string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.cams[cam]
	return st != nil && st.tracker.Open() != nil
}

// Flush writes every event in progress with its latest end (at shutdown).
func (m *Manager) Flush() {
	var saves []pending
	m.mu.Lock()
	for id, st := range m.cams {
		for _, e := range st.open() {
			saves = append(saves, pending{id, e})
		}
	}
	m.mu.Unlock()
	for _, p := range saves {
		m.save(p.cam, &p.ev)
	}
}

func (m *Manager) save(cam string, e *Event) {
	if e == nil {
		return
	}
	if err := m.Store.Append(cam, *e); err != nil {
		m.logf("motion: %s: write event: %v", cam, err)
	}
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}
