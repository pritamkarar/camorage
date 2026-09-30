package motion

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event is a period of motion: Start and End are the first and last frames that showed motion.
type Event struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Peak   int       `json:"peak"`           // most blocks changed at once
	Source string    `json:"source"`         // "phone", or "blind": recording while motion could not be seen
	Open   bool      `json:"open,omitempty"` // still in progress (never stored)
}

// Tracker turns one camera's detection results into events.
type Tracker struct {
	PostRoll time.Duration // no motion for this long closes the event
	open     *Event
}

// Frame records the result of the frame taken at t and returns the event it closed, if any.
// Motion after a gap longer than PostRoll closes the old event and opens a new one.
func (tr *Tracker) Frame(t time.Time, r Result) *Event {
	closed := tr.Tick(t)
	if r.Motion {
		if tr.open == nil {
			tr.open = &Event{Start: t, End: t, Peak: r.Changed, Source: "phone"}
		} else {
			tr.open.End = t
			tr.open.Peak = max(tr.open.Peak, r.Changed)
		}
	}
	return closed
}

// Tick closes the open event once PostRoll has passed since its last motion, also when frames
// have stopped coming (camera offline, reader restarting).
func (tr *Tracker) Tick(now time.Time) *Event {
	if tr.open == nil || now.Sub(tr.open.End) < tr.PostRoll {
		return nil
	}
	e := *tr.open
	tr.open = nil
	return &e
}

// Open returns a copy of the event in progress, marked Open, or nil.
func (tr *Tracker) Open() *Event {
	if tr.open == nil {
		return nil
	}
	e := *tr.open
	e.Open = true
	return &e
}

// Store keeps events as JSON lines, one file per camera and phone-local day of the event's start:
// <Dir>/<cam>/<YYYY-MM-DD>.jsonl. An event is written when it opens and again when it closes, and
// readers keep the latest line for each start, so a crash loses at most an event's end.
type Store struct {
	Dir  string
	Zone *time.Location
	mu   sync.Mutex
}

const dayLayout = "2006-01-02"

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Append adds one line for e to its camera's file.
func (s *Store) Append(cam string, e Event) error {
	e.Open = false
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.Dir, cam)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, e.Start.In(s.Zone).Format(dayLayout)+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Between returns cam's events overlapping [from, to), oldest first. It also reads the day before
// from, for events that started then and ran on. Lines that do not parse (a write cut short by a
// crash) are skipped.
func (s *Store) Between(cam string, from, to time.Time) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type key struct {
		start  int64
		source string
	}
	latest := map[key]Event{}
	for day := midnight(from.In(s.Zone)).AddDate(0, 0, -1); day.Before(to); day = day.AddDate(0, 0, 1) {
		b, err := os.ReadFile(filepath.Join(s.Dir, cam, day.Format(dayLayout)+".jsonl"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, line := range bytes.Split(b, []byte("\n")) {
			var e Event
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			k := key{e.Start.UnixNano(), e.Source}
			if old, ok := latest[k]; ok {
				if old.End.After(e.End) {
					e.End = old.End
				}
				e.Peak = max(e.Peak, old.Peak)
			}
			latest[k] = e
		}
	}
	out := []Event{}
	for _, e := range latest {
		if !e.End.Before(from) && e.Start.Before(to) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// Prune deletes each camera's event files from before the last keepDays(cam) days (phone-local),
// so timeline marks go away with the footage they describe.
func (s *Store) Prune(now time.Time, keepDays func(cam string) int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cams, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, c := range cams {
		if !c.IsDir() {
			continue
		}
		cutoff := midnight(now.In(s.Zone)).AddDate(0, 0, -keepDays(c.Name())).Format(dayLayout)
		files, err := os.ReadDir(filepath.Join(s.Dir, c.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			if day, ok := strings.CutSuffix(f.Name(), ".jsonl"); ok && day < cutoff { // YYYY-MM-DD sorts as text
				if err := os.Remove(filepath.Join(s.Dir, c.Name(), f.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
