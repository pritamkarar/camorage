package web

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func cloudEnv(t *testing.T) (*env, *fakeCloud) {
	e, fc := storageEnv(t)
	e.do("POST", "/api/storage/local", `{"name":"Folder","dir":"/tmp/cloud"}`)
	e.do("POST", "/api/cameras", strings.Replace(gate, "{", `{"cloud":{"targetId":"folder","days":7},`, 1))
	e.waitChanged()
	fc.files["folder:/tmp/cloud/camorage/front-gate/2026-09-30/10-00-00_hour_3600s.mp4"] = []byte("0123456789")
	fc.files["folder:/tmp/cloud/camorage/front-gate/2026-09-30/09-15-00_motion_40s.mp4"] = []byte("abc")
	return e, fc
}

func TestCloudSpans(t *testing.T) {
	e, _ := cloudEnv(t)
	w := e.do("GET", "/api/playback/cloud-spans?cam=front-gate&date=2026-09-30", "")
	want := `[{"file":"09-15-00_motion_40s.mp4","start":"2026-09-30T09:15:00+05:30","durationSec":40,"size":3},{"file":"10-00-00_hour_3600s.mp4","start":"2026-09-30T10:00:00+05:30","durationSec":3600,"size":10}]`
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	e.do("POST", "/api/cameras", strings.Replace(gate, "Front Gate", "Yard", 1))
	e.waitChanged()
	if w := e.do("GET", "/api/playback/cloud-spans?cam=yard&date=2026-09-30", ""); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("no cloud copy: %d %s", w.Code, w.Body)
	}
}

func TestCloudClipRanges(t *testing.T) {
	e, _ := cloudEnv(t)
	get := func(rng string, extra string) *httptestResponse {
		return e.doRange("/api/playback/cloud?cam=front-gate&date=2026-09-30&file=10-00-00_hour_3600s.mp4"+extra, rng)
	}
	for _, c := range []struct {
		rng, want, contentRange string
		code                    int
	}{
		{"", "0123456789", "", http.StatusOK},
		{"bytes=2-5", "2345", "bytes 2-5/10", http.StatusPartialContent},
		{"bytes=7-", "789", "bytes 7-9/10", http.StatusPartialContent},
		{"bytes=-3", "789", "bytes 7-9/10", http.StatusPartialContent},
		{"bytes=8-100", "89", "bytes 8-9/10", http.StatusPartialContent},
		{"bytes=10-", "", "bytes */10", http.StatusRequestedRangeNotSatisfiable},
		{"bytes=5-2", "", "bytes */10", http.StatusRequestedRangeNotSatisfiable},
	} {
		w := get(c.rng, "")
		if w.Code != c.code || (c.code != 416 && w.Body != c.want) || w.Header.Get("Content-Range") != c.contentRange {
			t.Errorf("Range %q: %d %q %q", c.rng, w.Code, w.Body, w.Header.Get("Content-Range"))
		}
		if c.code == http.StatusPartialContent && (w.Header.Get("Accept-Ranges") != "bytes" || w.Header.Get("Content-Type") != "video/mp4") {
			t.Errorf("Range %q headers %v", c.rng, w.Header)
		}
	}
	if w := get("", "&download=1"); !strings.Contains(w.Header.Get("Content-Disposition"), `attachment; filename="front-gate_2026-09-30_10-00-00_hour_3600s.mp4"`) {
		t.Errorf("download: %v", w.Header)
	}
	for _, bad := range []string{"../../etc/passwd", "10-00-00_hour_3600s.mp4/x", "nope.mp4"} {
		if w := e.do("GET", "/api/playback/cloud?cam=front-gate&date=2026-09-30&file="+bad, ""); w.Code != http.StatusBadRequest {
			t.Errorf("%q: %d", bad, w.Code)
		}
	}
	if w := e.do("GET", "/api/playback/cloud?cam=front-gate&date=2026-09-30&file=11-00-00_hour_3600s.mp4", ""); w.Code != http.StatusNotFound {
		t.Errorf("missing clip: %d", w.Code)
	}
}

func TestCloudListingHasADeadline(t *testing.T) {
	e, fc := cloudEnv(t)
	fc.hang = true
	defer func(d time.Duration) { cloudListTimeout = d }(cloudListTimeout)
	cloudListTimeout = 100 * time.Millisecond
	for _, path := range []string{
		"/api/playback/cloud-spans?cam=front-gate&date=2026-09-30",
		"/api/playback/cloud?cam=front-gate&date=2026-09-30&file=10-00-00_hour_3600s.mp4",
	} {
		done := make(chan int, 1)
		go func() { done <- e.do("GET", path, "").Code }()
		select {
		case code := <-done:
			if code != http.StatusGatewayTimeout {
				t.Errorf("%s: %d", path, code)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s waits for an unreachable cloud: the timeline would stay empty", path)
		}
	}
}
