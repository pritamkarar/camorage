package mediamtx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"camorage/internal/config"
)

func TestConfigRendersCameras(t *testing.T) {
	cams := []config.Camera{
		{ID: "cam1", Enabled: true, MainURL: "rtsp://1.2.3.4/main", SubURL: "rtsp://1.2.3.4/sub"},
		{ID: "cam2", Enabled: false, MainURL: "rtsp://1.2.3.5/main"},
		{ID: "cam3", Enabled: true, MainURL: "rtsp://1.2.3.6/main"},
	}
	b, err := Config("/sd/rec", cams, map[string]bool{"cam1": true}, "100.64.0.7")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	paths := m["paths"].(map[string]any)
	if _, found := paths["cam2"]; found {
		t.Fatal("disabled camera rendered")
	}
	if c1 := paths["cam1"].(map[string]any); c1["source"] != "rtsp://1.2.3.4/main" || c1["record"] != true {
		t.Fatalf("cam1 = %v", c1)
	}
	if paths["cam1_sub"].(map[string]any)["record"] != false {
		t.Fatal("a substream must never record")
	}
	if paths["cam3"].(map[string]any)["record"] != false {
		t.Fatal("cam3 record should default to false")
	}
	if _, found := paths["cam3_sub"]; found {
		t.Fatal("cam3 has no substream")
	}
	pd := m["pathDefaults"].(map[string]any)
	if pd["recordPath"] != "/sd/rec/%path/%Y-%m-%d_%H-%M-%S-%f" || pd["recordSegmentDuration"] != "1m" || pd["recordDeleteAfter"] != "0s" || pd["rtspTransport"] != "tcp" {
		t.Fatalf("pathDefaults = %v", pd)
	}
	for k, want := range map[string]any{"moq": false, "rtmp": false, "srt": false, "hlsVariant": "fmp4", "apiAddress": "127.0.0.1:9997", "rtspAddress": "127.0.0.1:8554", "playbackAddress": "127.0.0.1:9996"} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
	if tr := m["rtspTransports"].([]any); len(tr) != 1 || tr[0] != "tcp" {
		t.Fatalf("rtspTransports = %v", tr)
	}
	if h := m["webrtcAdditionalHosts"].([]any); len(h) != 1 || h[0] != "100.64.0.7" {
		t.Fatalf("webrtcAdditionalHosts = %v", h)
	}
}

func TestConfigWithoutTailscaleHasEmptyHostList(t *testing.T) {
	b, err := Config("/r", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"webrtcAdditionalHosts": []`) {
		t.Fatalf("got %s", b)
	}
}

func newTestClient(t *testing.T, h http.Handler) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{API: srv.URL, Playback: srv.URL, HTTP: srv.Client()}
}

func TestPathsRecordAndSetRecord(t *testing.T) {
	var patched string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/paths/list", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"items":[{"name":"cam1","available":true,"online":true,"inboundBytes":42},{"name":"dead","available":false,"online":true}]}`)
	})
	mux.HandleFunc("GET /v3/config/paths/get/cam1", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name":"cam1","record":true}`)
	})
	mux.HandleFunc("PATCH /v3/config/paths/patch/cam1", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		patched = string(b)
	})
	mux.HandleFunc("PATCH /v3/config/paths/patch/missing", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"path not found"}`, http.StatusNotFound)
	})
	c := newTestClient(t, mux)
	ctx := context.Background()
	paths, err := c.Paths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !paths["cam1"].Available || paths["dead"].Available || paths["cam1"].InboundBytes != 42 {
		t.Fatalf("paths = %+v", paths)
	}
	if rec, err := c.Record(ctx, "cam1"); err != nil || !rec {
		t.Fatalf("Record = %v, %v", rec, err)
	}
	if err := c.SetRecord(ctx, "cam1", false); err != nil {
		t.Fatal(err)
	}
	if patched != `{"record":false}` {
		t.Fatalf("patched body %q", patched)
	}
	var se *StatusError
	if err := c.SetRecord(ctx, "missing", true); !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("want 404 StatusError, got %v", err)
	}
}

func TestSpansSendsUTCAndParses(t *testing.T) {
	var q url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		io.WriteString(w, `[{"start":"2026-09-30T07:22:34.84379Z","duration":217.520709,"url":"x"}]`)
	})
	c := newTestClient(t, mux)
	ist := time.FixedZone("IST", 19800)
	from := time.Date(2026, 9, 30, 0, 0, 0, 0, ist)
	spans, err := c.Spans(context.Background(), "cam1", from, from.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("path") != "cam1" || q.Get("start") != "2026-09-29T18:30:00Z" || q.Get("end") != "2026-09-30T18:30:00Z" {
		t.Fatalf("query = %v", q)
	}
	want := time.Date(2026, 9, 30, 7, 22, 34, 843790000, time.UTC)
	if len(spans) != 1 || !spans[0].Start.Equal(want) || spans[0].Duration.Round(time.Millisecond) != 217521*time.Millisecond {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestSpansNoRecordingsIsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"no recordings found"}`, http.StatusNotFound)
	})
	c := newTestClient(t, mux)
	spans, err := c.Spans(context.Background(), "cam1", time.Now(), time.Now())
	if err != nil || len(spans) != 0 {
		t.Fatalf("got %v, %v", spans, err)
	}
}

// A camera that never recorded has no folder yet; MediaMTX answers 400, not 404.
func TestSpansNoFolderYetIsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":"error","error":"lstat /rec/cam1: no such file or directory"}`, http.StatusBadRequest)
	})
	c := newTestClient(t, mux)
	spans, err := c.Spans(context.Background(), "cam1", time.Now(), time.Now())
	if err != nil || len(spans) != 0 {
		t.Fatalf("got %v, %v", spans, err)
	}
}

// The janitor deleting a segment while MediaMTX reads the folder fails that one listing; the next
// one no longer sees the file.
func TestSpansRetriesSegmentDeletedMeanwhile(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list", func(w http.ResponseWriter, r *http.Request) {
		if calls++; calls == 1 {
			http.Error(w, `{"status":"error","error":"open /rec/cam1/2026-10-01_11-29-17-292287.mp4: no such file or directory"}`, http.StatusInternalServerError)
			return
		}
		io.WriteString(w, `[{"start":"2026-09-30T07:22:34Z","duration":60}]`)
	})
	c := newTestClient(t, mux)
	spans, err := c.Spans(context.Background(), "cam1", time.Now(), time.Now())
	if err != nil || len(spans) != 1 {
		t.Fatalf("got %v, %v", spans, err)
	}
}

func TestVideoURL(t *testing.T) {
	c := &Client{Playback: "http://127.0.0.1:9996"}
	ist := time.FixedZone("IST", 19800)
	got := c.VideoURL("cam1", time.Date(2026, 9, 30, 12, 52, 34, 0, ist), 90*time.Second, "mp4")
	want := "http://127.0.0.1:9996/get?duration=90&format=mp4&path=cam1&start=2026-09-30T07%3A22%3A34Z"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestProcessSpecRunsInUTC(t *testing.T) {
	s := ProcessSpec("/bin/mediamtx", "/d/mediamtx.yml")
	if s.Name != "mediamtx" || s.Path != "/bin/mediamtx" || strings.Join(s.Args, " ") != "/d/mediamtx.yml" {
		t.Fatalf("%+v", s)
	}
	// segment names are parsed as UTC (recorder.ParseSegmentName); a TZ in camorage's environment
	// must not shift them, or the keep rule would delete the wrong footage
	if !slices.Contains(s.Env, "TZ=UTC") {
		t.Fatalf("env %v", s.Env)
	}
}

func TestDownload(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if got.Get("path") == "missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("mp4-bytes"))
	}))
	defer srv.Close()
	c := &Client{Playback: srv.URL, HTTP: srv.Client()}
	dst := filepath.Join(t.TempDir(), "clip.mp4")
	start := time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)
	if err := c.Download(context.Background(), "gate", start, 90*time.Second, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "mp4-bytes" || got.Get("format") != "mp4" || got.Get("duration") != "90" || got.Get("start") != "2026-10-01T03:30:00Z" {
		t.Fatalf("file %q, query %v", b, got)
	}
	if err := c.Download(context.Background(), "missing", start, time.Second, dst); err == nil {
		t.Fatal("a 404 is not an error")
	}
}

func TestPathsReportsTracks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/paths/list", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"items":[{"name":"cam1","available":true,"tracks":["H265"]},{"name":"cam2","available":false}]}`)
	})
	paths, err := newTestClient(t, mux).Paths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := paths["cam1"].Tracks; len(got) != 1 || got[0] != "H265" {
		t.Fatalf("cam1 tracks = %v", got)
	}
	if paths["cam2"].Tracks != nil {
		t.Fatalf("cam2 tracks = %v", paths["cam2"].Tracks)
	}
}
