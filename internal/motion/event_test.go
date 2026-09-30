package motion

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	ist = time.FixedZone("IST", 5*3600+1800)
	t0  = time.Date(2026, 10, 1, 9, 0, 0, 0, ist)
)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

func TestTrackerOpensExtendsAndCloses(t *testing.T) {
	tr := Tracker{PostRoll: 30 * time.Second}
	yes, no := Result{Motion: true, Changed: 4}, Result{Changed: 1}
	if tr.Frame(at(0), no) != nil || tr.Open() != nil {
		t.Fatal("an event without motion")
	}
	tr.Frame(at(2), yes)
	tr.Frame(at(4), Result{Motion: true, Changed: 9})
	if o := tr.Open(); o == nil || !o.Start.Equal(at(2)) || !o.End.Equal(at(4)) || o.Peak != 9 || !o.Open || o.Source != "phone" {
		t.Fatalf("open: %+v", o)
	}
	if tr.Frame(at(30), no) != nil {
		t.Fatal("closed before the post-roll ran out")
	}
	closed := tr.Frame(at(34), no) // 30 s after the last motion
	if closed == nil || !closed.End.Equal(at(4)) || closed.Peak != 9 || closed.Open || tr.Open() != nil {
		t.Fatalf("closed: %+v, open: %+v", closed, tr.Open())
	}
	// motion after a gap longer than the post-roll starts a new event in the same call
	tr.Frame(at(40), yes)
	closed = tr.Frame(at(80), yes)
	if closed == nil || !closed.Start.Equal(at(40)) || tr.Open() == nil || !tr.Open().Start.Equal(at(80)) {
		t.Fatalf("closed %+v, open %+v", closed, tr.Open())
	}
}

func TestTrackerTickClosesWhenFramesStop(t *testing.T) {
	tr := Tracker{PostRoll: 30 * time.Second}
	tr.Frame(t0, Result{Motion: true, Changed: 3})
	if tr.Tick(at(29)) != nil {
		t.Fatal("closed early")
	}
	if e := tr.Tick(at(30)); e == nil || !e.End.Equal(t0) {
		t.Fatalf("not closed: %+v", e)
	}
	if tr.Tick(at(3600)) != nil {
		t.Fatal("closed twice")
	}
}

func TestStoreMergesOpenAndCloseLines(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Zone: ist}
	s.Append("gate", Event{Start: t0, End: t0, Peak: 3, Source: "phone", Open: true})
	s.Append("gate", Event{Start: t0, End: at(20), Peak: 7, Source: "phone"})
	evs, err := s.Between("gate", at(-3600), at(3600))
	if err != nil || len(evs) != 1 || !evs[0].End.Equal(at(20)) || evs[0].Peak != 7 || evs[0].Open {
		t.Fatalf("%+v %v", evs, err)
	}
}

func TestStoreSurvivesACrash(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Zone: ist}
	s.Append("gate", Event{Start: t0, End: t0, Peak: 3, Source: "phone"}) // written when it opened
	f, _ := os.OpenFile(filepath.Join(s.Dir, "gate", "2026-10-01.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
	f.WriteString(`{"start":"2026-10-01T09:0`) // the close was cut short
	f.Close()
	evs, err := s.Between("gate", at(-60), at(60))
	if err != nil || len(evs) != 1 || !evs[0].Start.Equal(t0) {
		t.Fatalf("%+v %v", evs, err)
	}
}

func TestStoreFilesByLocalDayAndFindsLateEvents(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Zone: ist}
	late := Event{Start: time.Date(2026, 10, 1, 23, 59, 50, 0, ist), End: time.Date(2026, 10, 2, 0, 3, 0, 0, ist), Peak: 4, Source: "phone"}
	if err := s.Append("gate", late); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "gate", "2026-10-01.jsonl")); err != nil {
		t.Fatal("not filed under the local day it started")
	}
	day2 := time.Date(2026, 10, 2, 0, 0, 0, 0, ist)
	if evs, _ := s.Between("gate", day2, day2.AddDate(0, 0, 1)); len(evs) != 1 {
		t.Fatalf("the next day misses an event running past midnight: %+v", evs)
	}
	if evs, _ := s.Between("gate", day2.Add(time.Hour), day2.AddDate(0, 0, 1)); len(evs) != 0 {
		t.Fatalf("an event that ended at 00:03 overlaps 01:00-: %+v", evs)
	}
	if evs, err := s.Between("no-such-cam", day2, day2.AddDate(0, 0, 1)); err != nil || len(evs) != 0 {
		t.Fatalf("%+v %v", evs, err)
	}
}

func TestStorePrune(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Zone: ist}
	for _, d := range []int{28, 29, 30} {
		s.Append("gate", Event{Start: time.Date(2026, 9, d, 12, 0, 0, 0, ist), Source: "phone"})
	}
	s.Append("gate", Event{Start: t0, Source: "phone"})
	if err := s.Prune(t0, func(string) int { return 1 }); err != nil {
		t.Fatal(err)
	}
	for day, want := range map[string]bool{"2026-09-28": false, "2026-09-29": false, "2026-09-30": true, "2026-10-01": true} {
		_, err := os.Stat(filepath.Join(s.Dir, "gate", day+".jsonl"))
		if (err == nil) != want {
			t.Errorf("%s: exists=%v, want %v", day, err == nil, want)
		}
	}
}
