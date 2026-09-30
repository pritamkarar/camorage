package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/motion"
)

// uploaderRig is an Uploader for camera "gate" copying to a folder target through the real rclone.
type uploaderRig struct {
	u       *Uploader
	cloud   string // the target folder
	spans   []mediamtx.Span
	fetches int
	fail    error
	now     time.Time
}

func newUploaderRig(t *testing.T) *uploaderRig {
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec")
	os.MkdirAll(rec, 0o700)
	store, err := config.Open(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	since := at(8, 0, 0)
	err = store.Update(func(c *config.Config) error {
		c.RecDir = rec
		c.StorageTargets = []config.StorageTarget{{ID: "t", Name: "Folder", Type: "local", Remote: "t:" + filepath.Join(dir, "cloud")}}
		c.Cameras = []config.Camera{{ID: "gate", Name: "Gate", Enabled: true, MainURL: "rtsp://10.0.0.2/live", LocalDays: 1,
			Cloud: &config.Cloud{TargetID: "t", Days: 7, Since: &since}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &uploaderRig{cloud: filepath.Join(dir, "cloud"), now: at(10, 3, 0), spans: []mediamtx.Span{span(at(9, 0, 0), at(10, 3, 0))}}
	rc := Rclone{Bin: rcloneBin(t), Config: filepath.Join(dir, "rclone.conf")}
	SetSection(rc.Config, "t", map[string]string{"type": "local"})
	r.u = &Uploader{
		Store: store, Rclone: rc, Ledger: &Ledger{Dir: filepath.Join(dir, "uploads")}, Zone: ist,
		Now:    func() time.Time { return r.now },
		Spans:  func(context.Context, string, time.Time, time.Time) ([]mediamtx.Span, error) { return r.spans, nil },
		Events: func(string, time.Time, time.Time) ([]motion.Event, error) { return nil, nil },
		Fetch: func(_ context.Context, cam string, from time.Time, dur time.Duration, dst string) error {
			r.fetches++
			if r.fail != nil {
				return r.fail
			}
			return os.WriteFile(dst, []byte(cam+" "+from.Format("15:04:05")+" "+dur.String()), 0o600)
		},
	}
	return r
}

func TestUploaderCopiesDueClipsOnce(t *testing.T) {
	r := newUploaderRig(t)
	if err := r.u.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(r.cloud, "camorage/gate/2026-10-01/09-00-00_hour_3600s.mp4"))
	if err != nil || string(b) != "gate 09:00:00 1h0m0s" {
		t.Fatalf("cloud copy: %q %v", b, err)
	}
	st := r.u.Status()["gate"]
	if st.Queued != 0 || st.LastUpload == "" || st.LastError != "" {
		t.Fatalf("status %+v", st)
	}
	if left, _ := os.ReadDir(filepath.Join(r.u.Store.Get().RecDir, ".upload")); len(left) != 0 {
		t.Fatal("temp file left behind")
	}
	r.u.Run(context.Background())
	// later the hour's first 20 minutes are gone from the phone: the rest is not uploaded again
	r.now, r.spans = at(10, 20, 0), []mediamtx.Span{span(at(9, 20, 0), at(10, 20, 0))}
	r.u.Run(context.Background())
	if r.fetches != 1 {
		t.Fatalf("uploaded %d times", r.fetches)
	}
}

func TestUploaderBacksOffAfterAFailure(t *testing.T) {
	r := newUploaderRig(t)
	r.fail = errors.New("network is unreachable")
	if err := r.u.Run(context.Background()); err == nil {
		t.Fatal("a failed upload returned no error")
	}
	if st := r.u.Status()["gate"]; st.LastError == "" || st.Queued != 1 {
		t.Fatalf("status %+v", st)
	}
	if r.u.Ledger.Covered("gate", at(9, 0, 0), at(10, 0, 0), ist) {
		t.Fatal("a failed clip is in the ledger")
	}
	r.u.Run(context.Background()) // within the back-off: nothing tried
	if r.fetches != 1 {
		t.Fatalf("tried %d times during the back-off", r.fetches)
	}
	r.fail = nil
	r.now = r.now.Add(61 * time.Second)
	if err := r.u.Run(context.Background()); err != nil || r.fetches != 2 {
		t.Fatalf("after the back-off: %v, %d tries", err, r.fetches)
	}
	if st := r.u.Status()["gate"]; st.LastError != "" || st.Queued != 0 {
		t.Fatalf("status after recovery %+v", st)
	}
}

func TestRetention(t *testing.T) {
	r := newUploaderRig(t)
	old := filepath.Join(r.cloud, "camorage/gate/2026-09-20/09-00-00_hour_3600s.mp4")
	fresh := filepath.Join(r.cloud, "camorage/gate/2026-09-30/09-00-00_hour_3600s.mp4")
	for _, p := range []string{old, fresh} {
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte("x"), 0o600)
	}
	tenDays := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(old, tenDays, tenDays)
	if err := r.u.Retention(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("a clip older than the camera's 7 cloud days is still there")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a recent clip was deleted")
	}
}
