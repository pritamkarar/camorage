package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var ist = time.FixedZone("IST", 5*3600+1800)

func TestClipNames(t *testing.T) {
	start := time.Date(2026, 10, 1, 9, 5, 7, 0, ist)
	p := ClipPath("gate", start, KindHour, 3293)
	if p != "camorage/gate/2026-10-01/09-05-07_hour_3293s.mp4" {
		t.Fatalf("got %s", p)
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, ist)
	st, length, kind, ok := ParseClip(day, "09-05-07_hour_3293s.mp4")
	if !ok || !st.Equal(start) || length != 3293*time.Second || kind != KindHour {
		t.Fatalf("parse: %v %v %s %v", st, length, kind, ok)
	}
	for _, bad := range []string{"../x.mp4", "09-05-07_hour_3293s.mp4.part", "9-5-7_hour_1s.mp4", "09-05-07_other_1s.mp4"} {
		if _, _, _, ok := ParseClip(day, bad); ok || ValidClipName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !ValidClipName("23-59-50_motion_40s.mp4") {
		t.Error("valid name refused")
	}
}

func TestLedger(t *testing.T) {
	l := &Ledger{Dir: t.TempDir()}
	at := func(d, h, m, s int) time.Time { return time.Date(2026, 10, d, h, m, s, 0, ist) }
	covered := func(cam string, from, to time.Time) bool { return l.Covered(cam, from, to, ist) }
	if covered("gate", at(1, 9, 0, 0), at(1, 10, 0, 0)) {
		t.Fatal("empty ledger covers a clip")
	}
	a := "camorage/gate/2026-10-01/09-00-00_hour_3600s.mp4"
	if err := l.Add("gate", a); err != nil {
		t.Fatal(err)
	}
	l.Add("gate", "camorage/gate/2026-10-01/10-00-00_hour_1800s.mp4")
	l.Add("gate", "camorage/gate/2026-09-30/23-59-30_motion_60s.mp4")
	for _, c := range []struct {
		cam      string
		from, to time.Time
		want     bool
	}{
		{"gate", at(1, 9, 0, 0), at(1, 10, 0, 0), true},
		{"gate", at(1, 9, 20, 0).Add(400 * time.Millisecond), at(1, 10, 0, 0), true}, // its start deleted since
		{"gate", at(1, 9, 30, 0), at(1, 10, 30, 0), true},                            // two clips together
		{"gate", at(1, 0, 0, 10), at(1, 0, 0, 30), true},                             // a clip from the day before
		{"gate", at(1, 8, 59, 0), at(1, 10, 0, 0), false},                            // starts earlier
		{"gate", at(1, 10, 0, 0), at(1, 11, 0, 0), false},                            // runs on past 10:30
		{"yard", at(1, 9, 0, 0), at(1, 10, 0, 0), false},
	} {
		if got := covered(c.cam, c.from, c.to); got != c.want {
			t.Errorf("%s %s–%s: covered %v", c.cam, c.from.Format("15:04:05.0"), c.to.Format("15:04:05"), got)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(l.Dir, "gate", "2026-10-01.txt")); string(b) != a+"\ncamorage/gate/2026-10-01/10-00-00_hour_1800s.mp4\n" {
		t.Fatalf("file: %q", b)
	}
	l.Add("gate", "camorage/gate/2026-09-28/09-00-00_hour_3600s.mp4")
	if err := l.Prune(at(1, 12, 0, 0), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.Dir, "gate", "2026-09-28.txt")); !os.IsNotExist(err) {
		t.Fatal("old ledger kept")
	}
	if !covered("gate", at(1, 9, 0, 0), at(1, 10, 0, 0)) {
		t.Fatal("pruned a current ledger")
	}
}
