package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"camorage/internal/auth"
	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/platform"
	"camorage/internal/supervisor"
)

type fakeMTX struct {
	mu             sync.Mutex
	paths          map[string]mediamtx.PathState
	record         map[string]bool
	spans          []mediamtx.Span
	gotFrom, gotTo time.Time
	videoURL       string
	videoStart     time.Time
}

func (f *fakeMTX) Paths(context.Context) (map[string]mediamtx.PathState, error) { return f.paths, nil }
func (f *fakeMTX) Record(_ context.Context, n string) (bool, error)             { return f.record[n], nil }
func (f *fakeMTX) Spans(_ context.Context, _ string, from, to time.Time) ([]mediamtx.Span, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotFrom, f.gotTo = from, to
	return f.spans, nil
}
func (f *fakeMTX) VideoURL(_ string, start time.Time, _ time.Duration, _ string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.videoStart = start
	return f.videoURL
}

type env struct {
	t       *testing.T
	h       http.Handler
	store   *config.Store
	mtx     *fakeMTX
	now     time.Time
	changed chan struct{}
	cookie  *http.Cookie
}

func newEnv(t *testing.T, mods ...func(*Deps)) *env {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, store: store, changed: make(chan struct{}, 10),
		mtx: &fakeMTX{paths: map[string]mediamtx.PathState{}, record: map[string]bool{}},
		now: time.Date(2026, 9, 30, 12, 0, 0, 0, ist)}
	lim := auth.NewLimiter()
	lim.Now = func() time.Time { return e.now }
	d := Deps{
		Store: store, MTX: e.mtx, Zone: ist, Limiter: lim, HTTP: http.DefaultClient,
		Now:              func() time.Time { return e.now },
		Processes:        func() []supervisor.Status { return nil },
		Health:           func(string) platform.Health { return platform.Health{BatteryPct: 88} },
		OnCamerasChanged: func() { e.changed <- struct{}{} },
	}
	for _, m := range mods {
		m(&d)
	}
	e.h = New(d)
	return e
}

func (e *env) do(method, path, body string, mod ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://example.com")
	r.Header.Set("Content-Type", "application/json")
	if e.cookie != nil {
		r.AddCookie(e.cookie)
	}
	for _, m := range mod {
		m(r)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func noOrigin(r *http.Request) { r.Header.Del("Origin") }

func (e *env) setUp() {
	e.t.Helper()
	w := e.do("POST", "/api/setup", `{"password":"correct-horse","recDir":"`+e.t.TempDir()+`"}`)
	if w.Code != http.StatusOK {
		e.t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	e.cookie = w.Result().Cookies()[0]
}

func (e *env) waitChanged() {
	e.t.Helper()
	select {
	case <-e.changed:
	case <-time.After(2 * time.Second):
		e.t.Fatal("OnCamerasChanged not called")
	}
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body, err)
	}
	return v
}

const gate = `{"name":"Front Gate","enabled":true,"mainUrl":"rtsp://admin:secret@192.168.1.5/live","subUrl":"rtsp://admin:secret@192.168.1.5/sub"}`

func TestSetupThenHealth(t *testing.T) {
	e := newEnv(t)
	if h := decode[map[string]any](t, e.do("GET", "/api/health", "")); h["setupDone"] != false {
		t.Fatalf("health before setup: %v", h)
	}
	e.setUp()
	if h := decode[map[string]any](t, e.do("GET", "/api/health", "")); h["setupDone"] != true {
		t.Fatalf("health after setup: %v", h)
	}
	if w := e.do("POST", "/api/setup", `{"password":"another-pass","recDir":"`+t.TempDir()+`"}`); w.Code != http.StatusConflict {
		t.Fatalf("second setup: %d", w.Code)
	}
}

func TestSetupValidation(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		body string
		mod  []func(*http.Request)
		code int
	}{
		{`{"password":"short","recDir":"` + t.TempDir() + `"}`, nil, http.StatusBadRequest},
		{`{"password":"long-enough","recDir":"relative/dir"}`, nil, http.StatusBadRequest},
		{`{"password":"long-enough","recDir":"` + t.TempDir() + `"}`, []func(*http.Request){noOrigin}, http.StatusForbidden},
		{`{"password":"long-enough","recDir":"` + t.TempDir() + `","extra":1}`, nil, http.StatusBadRequest},
	}
	for i, c := range cases {
		if w := e.do("POST", "/api/setup", c.body, c.mod...); w.Code != c.code {
			t.Errorf("case %d: got %d, want %d (%s)", i, w.Code, c.code, w.Body)
		}
	}
}

func TestLoginLockout(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	e.cookie = nil
	for i := 0; i < 5; i++ {
		if w := e.do("POST", "/api/login", `{"password":"wrong-horse"}`); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	if w := e.do("POST", "/api/login", `{"password":"correct-horse"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt: %d, want 429 even with the right password", w.Code)
	}
	e.now = e.now.Add(16 * time.Minute)
	if w := e.do("POST", "/api/login", `{"password":"correct-horse"}`); w.Code != http.StatusOK || len(w.Result().Cookies()) == 0 {
		t.Fatalf("login after lockout: %d", w.Code)
	}
}

func TestLoginLockoutHoldsUnderParallelGuesses(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	e.cookie = nil
	var wg sync.WaitGroup
	codes := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- e.do("POST", "/api/login", `{"password":"wrong-horse"}`).Code
		}()
	}
	wg.Wait()
	close(codes)
	checked := 0
	for c := range codes {
		if c == http.StatusUnauthorized {
			checked++
		}
	}
	if checked > 5 {
		t.Fatalf("%d parallel guesses were checked; the lockout must stop after 5", checked)
	}
}

func TestAuthAndCSRF(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	cookie := e.cookie
	e.cookie = nil
	if w := e.do("GET", "/api/cameras", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", w.Code)
	}
	e.cookie = cookie
	if w := e.do("GET", "/api/cameras", ""); w.Code != http.StatusOK {
		t.Fatalf("with cookie: %d", w.Code)
	}
	if w := e.do("POST", "/api/cameras", gate, noOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", w.Code)
	}
}

func TestCameraCRUDAndMasking(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	w := e.do("POST", "/api/cameras", gate)
	if w.Code != http.StatusCreated || decode[map[string]string](t, w)["id"] != "front-gate" {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	e.waitChanged()

	list := decode[[]config.Camera](t, e.do("GET", "/api/cameras", ""))
	if len(list) != 1 || strings.Contains(list[0].MainURL, "secret") || !strings.Contains(list[0].MainURL, "********") {
		t.Fatalf("list leaks the password or lacks the mask: %+v", list)
	}

	edited := list[0]
	edited.Name = "Gate"
	b, _ := json.Marshal(edited) // echoes the masked URLs back
	if w := e.do("PUT", "/api/cameras/front-gate", string(b)); w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	e.waitChanged()
	cfg := e.store.Get()
	stored, _ := cfg.CameraByID("front-gate")
	if stored.Name != "Gate" || stored.MainURL != "rtsp://admin:secret@192.168.1.5/live" {
		t.Fatalf("stored after masked PUT: %+v", stored)
	}

	if w := e.do("POST", "/api/cameras", `{"name":"Bad","mainUrl":"rtsp://admin:pa#ss@1.2.3.4/x"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid URL: %d", w.Code)
	}
	if w := e.do("PUT", "/api/cameras/nope", gate); w.Code != http.StatusNotFound {
		t.Fatalf("put unknown: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/cameras/front-gate", ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
	e.waitChanged()
	if n := len(e.store.Get().Cameras); n != 0 {
		t.Fatalf("%d cameras after delete", n)
	}
}

func TestSpansUsesLocalDay(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	e.mtx.spans = []mediamtx.Span{{Start: time.Date(2026, 9, 30, 7, 22, 34, 0, time.UTC), Duration: 217 * time.Second}}
	w := e.do("GET", "/api/playback/spans?cam=front-gate&date=2026-09-30", "")
	if w.Code != http.StatusOK {
		t.Fatalf("spans: %d %s", w.Code, w.Body)
	}
	if !e.mtx.gotFrom.Equal(time.Date(2026, 9, 29, 18, 30, 0, 0, time.UTC)) || !e.mtx.gotTo.Equal(time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC)) {
		t.Fatalf("queried %v – %v, want the IST day expressed in UTC", e.mtx.gotFrom, e.mtx.gotTo)
	}
	got := decode[[]map[string]any](t, w)
	if len(got) != 1 || got[0]["start"] != "2026-09-30T12:52:34+05:30" || got[0]["durationSec"] != 217.0 {
		t.Fatalf("spans = %v", got)
	}
	if w := e.do("GET", "/api/playback/spans?cam=front-gate&date=30-09-2026", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad date: %d", w.Code)
	}
	if w := e.do("GET", "/api/playback/spans?cam=nope&date=2026-09-30", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown camera: %d", w.Code)
	}
}

func TestVideoProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "VIDEO") }))
	defer upstream.Close()
	e := newEnv(t)
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	e.mtx.videoURL = upstream.URL + "/get"
	w := e.do("GET", "/api/playback/video?cam=front-gate&start=2026-09-30T12:52:34%2B05:30&duration=90&format=mp4", "")
	if w.Code != http.StatusOK || w.Body.String() != "VIDEO" || w.Header().Get("Content-Type") != "video/mp4" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "front-gate_2026-09-30_12-52-34.mp4") {
		t.Fatalf("video: %d %q %v", w.Code, w.Body, w.Header())
	}
	for _, q := range []string{"duration=0&format=mp4", "duration=90&format=avi", "duration=abc"} {
		if w := e.do("GET", "/api/playback/video?cam=front-gate&start=2026-09-30T12:52:34%2B05:30&"+q, ""); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", q, w.Code)
		}
	}
}

func TestStatus(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	e.mtx.paths["front-gate"] = mediamtx.PathState{Name: "front-gate", Available: true}
	e.mtx.record["front-gate"] = true
	var st struct {
		Cameras []struct {
			ID                                   string
			Available, Recording, ScheduleActive bool
		}
		Health platform.Health
	}
	if err := json.Unmarshal(e.do("GET", "/api/status", "").Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Cameras) != 1 || !st.Cameras[0].Available || !st.Cameras[0].Recording || !st.Cameras[0].ScheduleActive || st.Health.BatteryPct != 88 {
		t.Fatalf("status = %+v", st)
	}
}

type httptestResponse struct {
	Code   int
	Header http.Header
	Body   string
}

// doRange GETs path with a Range header (none if rng is empty).
func (e *env) doRange(path, rng string) *httptestResponse {
	w := e.do("GET", path, "", func(r *http.Request) {
		if rng != "" {
			r.Header.Set("Range", rng)
		}
	})
	return &httptestResponse{w.Code, w.Header(), w.Body.String()}
}

func TestStatusReportsVersion(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.Version = "0.4.0" })
	e.setUp()
	if st := decode[map[string]any](t, e.do("GET", "/api/status", "")); st["version"] != "0.4.0" {
		t.Fatalf("version %v", st["version"])
	}
}
