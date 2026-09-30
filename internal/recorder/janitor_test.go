package recorder

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camorage/internal/config"
	"camorage/internal/motion"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// seg creates a sparse segment file (Truncate uses no real disk space).
func seg(t *testing.T, dir, cam string, start time.Time, size int64) string {
	t.Helper()
	d := filepath.Join(dir, cam)
	os.MkdirAll(d, 0o755)
	p := filepath.Join(d, start.UTC().Format("2006-01-02_15-04-05")+"-000000.mp4")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func plenty(string) (uint64, uint64, error) { return 100 << 30, 200 << 30, nil }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestParseSegmentName(t *testing.T) {
	got, ok := ParseSegmentName("2026-09-30_07-22-34-843790.mp4")
	if !ok || !got.Equal(time.Date(2026, 9, 30, 7, 22, 34, 0, time.UTC)) {
		t.Fatalf("got %v %v", got, ok)
	}
	for _, bad := range []string{"junk.mp4", "2026-09-30_07-22-34-843790.mp4.tmp", "notes.txt"} {
		if _, ok := ParseSegmentName(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestJanitorRetentionKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	old1 := seg(t, dir, "cam1", now.Add(-50*time.Hour), 10)
	old2 := seg(t, dir, "cam1", now.Add(-30*time.Hour), 10)
	recent := seg(t, dir, "cam1", now.Add(-2*time.Hour), 10)
	onlyOld := seg(t, dir, "cam2", now.Add(-100*time.Hour), 10) // cam2's newest: MediaMTX may still write it
	cams := []config.Camera{{ID: "cam1", LocalDays: 1}, {ID: "cam2", LocalDays: 1}}
	deleted, err := Janitor(dir, cams, now, plenty, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || exists(old1) || exists(old2) || !exists(recent) || !exists(onlyOld) {
		t.Fatalf("deleted %v", deleted)
	}
}

func TestJanitorDeletedCameraUsesDefaultDays(t *testing.T) {
	dir := t.TempDir()
	old := seg(t, dir, "gone", now.Add(-48*time.Hour), 10)
	newest := seg(t, dir, "gone", now.Add(-47*time.Hour), 10)
	if _, err := Janitor(dir, nil, now, plenty, nil); err != nil {
		t.Fatal(err)
	}
	if exists(old) || !exists(newest) {
		t.Fatal("footage of a deleted camera must age out with DefaultLocalDays, keeping the newest")
	}
}

func TestJanitorFloorDeletesOldestAcrossCameras(t *testing.T) {
	dir := t.TempDir()
	const mb = 1 << 20
	a := seg(t, dir, "cam1", now.Add(-3*time.Hour), 60*mb)
	b := seg(t, dir, "cam1", now.Add(-2*time.Hour), 60*mb)
	c := seg(t, dir, "cam1", now.Add(-1*time.Hour), 60*mb)
	d := seg(t, dir, "cam2", now.Add(-150*time.Minute), 60*mb)
	e := seg(t, dir, "cam2", now.Add(-30*time.Minute), 60*mb)
	disk := func(string) (uint64, uint64, error) { return 400 * mb, 4 << 30, nil } // floor = 500 MB
	cams := []config.Camera{{ID: "cam1", LocalDays: 7}, {ID: "cam2", LocalDays: 7}}
	deleted, err := Janitor(dir, cams, now, disk, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || exists(a) || exists(d) || !exists(b) || !exists(c) || !exists(e) {
		t.Fatalf("deleted %v", deleted)
	}
}

func TestJanitorUnderFloorReportsError(t *testing.T) {
	dir := t.TempDir()
	only := seg(t, dir, "cam1", now.Add(-time.Hour), 10)
	disk := func(string) (uint64, uint64, error) { return 100 << 20, 4 << 30, nil }
	deleted, err := Janitor(dir, []config.Camera{{ID: "cam1", LocalDays: 7}}, now, disk, nil)
	if err == nil || !strings.Contains(err.Error(), "floor") || len(deleted) != 0 || !exists(only) {
		t.Fatalf("deleted %v, err %v", deleted, err)
	}
}

func TestJanitorMissingRecDir(t *testing.T) {
	if _, err := Janitor(filepath.Join(t.TempDir(), "sd-card-removed"), nil, now, plenty, nil); err == nil {
		t.Fatal("expected an error for a missing recordings dir")
	}
}

func TestFloor(t *testing.T) {
	if Floor(4<<30) != 500<<20 {
		t.Fatal("small volume: floor is 500 MB")
	}
	if Floor(100<<30) != 5<<30 {
		t.Fatal("large volume: floor is 5 %")
	}
}

func TestKept(t *testing.T) {
	ten := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	ev := []motion.Event{{Start: ten, End: ten.Add(20 * time.Second)}} // motion 10:00:00–10:00:20
	pre, post := 10*time.Second, 30*time.Second                        // keep 09:59:50–10:00:50
	m := func(h, mm int) time.Time { return time.Date(2026, 9, 30, h, mm, 0, 0, time.UTC) }
	for _, c := range []struct {
		s0, s1 time.Time
		want   bool
	}{
		{m(9, 58), m(9, 59), false},
		{m(9, 59), m(10, 0), true},                   // holds the pre-roll
		{m(10, 0), m(10, 1), true},                   // holds the motion and the post-roll
		{ten.Add(50 * time.Second), m(10, 2), false}, // starts exactly when the post-roll ends
	} {
		if got := Kept(c.s0, c.s1, ev, pre, post); got != c.want {
			t.Errorf("[%s, %s): got %v", c.s0.Format("15:04:05"), c.s1.Format("15:04:05"), got)
		}
	}
	open := []motion.Event{{Start: ten, End: ten, Open: true}}
	if !Kept(m(10, 30), m(10, 31), open, pre, post) {
		t.Error("an event in progress does not keep what follows it")
	}
}

func TestJanitorMotionModeKeepsOnlyMotion(t *testing.T) {
	dir := t.TempDir()
	cam := config.Camera{ID: "gate", Mode: "motion", LocalDays: 1, Motion: config.Motion{PreRollSec: 10, PostRollSec: 30}}
	var p []string
	for i := 0; i < 5; i++ { // one-minute segments at -3h00, -2h59, -2h58, -2h57, -2h56
		p = append(p, seg(t, dir, "gate", now.Add(-3*time.Hour+time.Duration(i)*time.Minute), 10))
	}
	recent := seg(t, dir, "gate", now.Add(-30*time.Minute), 10) // no motion, but under an hour old
	newest := seg(t, dir, "gate", now.Add(-time.Minute), 10)
	// motion -2h58m30s … -2h58m20s: keeps -2h58m40s … -2h57m50s
	ev := motion.Event{Start: now.Add(-2*time.Hour - 58*time.Minute - 30*time.Second), End: now.Add(-2*time.Hour - 58*time.Minute - 20*time.Second)}
	events := func(c string, from, to time.Time) ([]motion.Event, error) {
		if c != "gate" {
			t.Fatalf("asked for %s", c)
		}
		return []motion.Event{ev}, nil
	}
	deleted, err := Janitor(dir, []config.Camera{cam}, now, plenty, events)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{false, true, true, false, false} {
		if exists(p[i]) != want {
			t.Errorf("segment %d: kept=%v, want %v", i, exists(p[i]), want)
		}
	}
	if !exists(recent) || !exists(newest) || len(deleted) != 3 {
		t.Fatalf("recent %v newest %v deleted %v", exists(recent), exists(newest), deleted)
	}
}

func TestJanitorKeepsFootageFromBeforeMotionMode(t *testing.T) {
	dir := t.TempDir()
	since := now.Add(-90 * time.Minute)
	cam := config.Camera{ID: "gate", Mode: "motion", MotionSince: &since, LocalDays: 1, Motion: config.Motion{PreRollSec: 10, PostRollSec: 30}}
	before := seg(t, dir, "gate", now.Add(-2*time.Hour), 10)   // recorded while it was continuous
	after := seg(t, dir, "gate", now.Add(-80*time.Minute), 10) // motion mode, no motion, over an hour old
	newest := seg(t, dir, "gate", now.Add(-time.Minute), 10)
	if _, err := Janitor(dir, []config.Camera{cam}, now, plenty, nil); err != nil {
		t.Fatal(err)
	}
	if !exists(before) || exists(after) || !exists(newest) {
		t.Fatalf("before %v after %v newest %v", exists(before), exists(after), exists(newest))
	}
}

func TestJanitorFloorDeletesUnkeptMotionFirst(t *testing.T) {
	dir := t.TempDir()
	const gib = 1 << 30
	gate := config.Camera{ID: "gate", Mode: "motion", LocalDays: 7, Motion: config.Motion{PreRollSec: 10, PostRollSec: 30}}
	yard := config.Camera{ID: "yard", Mode: "continuous", LocalDays: 7}
	yardOld := seg(t, dir, "yard", now.Add(-5*time.Hour), gib)
	seg(t, dir, "yard", now.Add(-2*time.Minute), gib)
	kept := seg(t, dir, "gate", now.Add(-4*time.Hour), gib) // holds motion
	spare1 := seg(t, dir, "gate", now.Add(-20*time.Minute), gib)
	spare2 := seg(t, dir, "gate", now.Add(-10*time.Minute), gib)
	seg(t, dir, "gate", now.Add(-time.Minute), gib)
	ev := motion.Event{Start: now.Add(-4*time.Hour + 10*time.Second), End: now.Add(-4*time.Hour + 20*time.Second)}
	events := func(string, time.Time, time.Time) ([]motion.Event, error) { return []motion.Event{ev}, nil }
	total := uint64(100) * gib                                                              // floor 5 GiB
	disk := func(string) (uint64, uint64, error) { return 5*gib - gib - gib/2, total, nil } // 1.5 GiB short
	deleted, err := Janitor(dir, []config.Camera{gate, yard}, now, disk, events)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || exists(spare1) || exists(spare2) || !exists(kept) || !exists(yardOld) {
		t.Fatalf("deleted %v", deleted)
	}
}

func TestJanitorKeepsMotionFootageWhenEventsCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	cam := config.Camera{ID: "gate", Mode: "motion", LocalDays: 1, Motion: config.Motion{PreRollSec: 10, PostRollSec: 30}}
	old := seg(t, dir, "gate", now.Add(-3*time.Hour), 10)
	seg(t, dir, "gate", now.Add(-time.Minute), 10)
	broken := func(string, time.Time, time.Time) ([]motion.Event, error) {
		return nil, errors.New("read events: input/output error")
	}
	_, err := Janitor(dir, []config.Camera{cam}, now, plenty, broken)
	if !exists(old) {
		t.Fatal("footage deleted because the events could not be read")
	}
	if err == nil || !strings.Contains(err.Error(), "input/output error") {
		t.Fatalf("the read error was not reported: %v", err)
	}
}
