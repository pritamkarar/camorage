package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLiveHLSProxy(t *testing.T) {
	var gotCookie, gotURI string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie, gotURI = r.Header.Get("Cookie"), r.URL.RequestURI()
		if r.URL.Query().Get("cookieCheck") == "" { // what MediaMTX v1.21 does on the first request
			http.SetCookie(w, &http.Cookie{Name: "cookieCheck", Value: "1"})
			w.Header().Set("Location", "/front-gate_sub/index.m3u8?cookieCheck=1")
			w.WriteHeader(http.StatusFound)
			return
		}
		io.WriteString(w, "#EXTM3U\n")
	}))
	defer upstream.Close()
	e := newEnv(t, func(d *Deps) { d.HLSBase = upstream.URL })
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()

	w := e.do("GET", "/live/hls/front-gate_sub/index.m3u8", "")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/live/hls/front-gate_sub/index.m3u8?cookieCheck=1" {
		t.Fatalf("redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("MediaMTX's cookie must not reach the browser")
	}
	if gotCookie != "" {
		t.Fatalf("the portal session cookie leaked to MediaMTX: %q", gotCookie)
	}
	w = e.do("GET", "/live/hls/front-gate_sub/index.m3u8?cookieCheck=1", "")
	if w.Code != http.StatusOK || w.Body.String() != "#EXTM3U\n" || gotURI != "/front-gate_sub/index.m3u8?cookieCheck=1" {
		t.Fatalf("playlist: %d %q via %q", w.Code, w.Body, gotURI)
	}
	if w := e.do("GET", "/live/hls/not-a-camera/index.m3u8", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown path: %d", w.Code)
	}
	e.cookie = nil
	if w := e.do("GET", "/live/hls/front-gate_sub/index.m3u8", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", w.Code)
	}
}

func TestLiveWHEPProxy(t *testing.T) {
	var got []string
	var offerType string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			offerType = r.Header.Get("Content-Type")
			w.Header().Set("Location", "/front-gate/whep/7f1c2d3e-0000-4000-8000-000000000001")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, "v=0 answer")
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	e := newEnv(t, func(d *Deps) { d.WebRTCBase = upstream.URL })
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()

	sdp := func(r *http.Request) { r.Header.Set("Content-Type", "application/sdp") }
	w := e.do("POST", "/live/whep/front-gate", "v=0 offer", sdp)
	if w.Code != http.StatusCreated || w.Body.String() != "v=0 answer" || w.Header().Get("Location") != "/live/whep/front-gate/7f1c2d3e-0000-4000-8000-000000000001" {
		t.Fatalf("offer: %d %q %q", w.Code, w.Body, w.Header().Get("Location"))
	}
	if offerType != "application/sdp" {
		t.Fatalf("offer Content-Type forwarded as %q", offerType)
	}
	if w := e.do("DELETE", "/live/whep/front-gate/7f1c2d3e-0000-4000-8000-000000000001", ""); w.Code != http.StatusOK {
		t.Fatalf("hang-up: %d", w.Code)
	}
	want := "POST /front-gate/whep|DELETE /front-gate/whep/7f1c2d3e-0000-4000-8000-000000000001"
	if strings.Join(got, "|") != want {
		t.Fatalf("upstream saw %v", got)
	}
	if w := e.do("POST", "/live/whep/front-gate", "v=0", sdp, noOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin offer: %d", w.Code)
	}
}

func TestWHEPLocation(t *testing.T) {
	if got := whepLocation("/cam1/whep/abc?x=1"); got != "/live/whep/cam1/abc?x=1" {
		t.Fatalf("got %q", got)
	}
	if got := whepLocation("/elsewhere"); got != "/elsewhere" {
		t.Fatalf("got %q", got)
	}
}

func TestLiveProxyRefusesEscapes(t *testing.T) {
	var hits []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	e := newEnv(t, func(d *Deps) { d.HLSBase, d.WebRTCBase = upstream.URL, upstream.URL })
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()

	for _, c := range []struct{ method, path string }{
		// PathValue decodes %2F, so these arrive as front-gate/../…, which MediaMTX would resolve
		{"GET", "/live/hls/front-gate%2F..%2Fsecret/index.m3u8"},
		{"GET", "/live/hls/front-gate/..%2F..%2Fv3%2Fconfig%2Fglobal%2Fget"},
		{"PATCH", "/live/whep/front-gate/..%2F..%2Fsecret%2Fwhep%2F7f1c2d3e-0000-4000-8000-000000000001"},
		{"DELETE", "/live/whep/front-gate/not-a-session"},
	} {
		if w := e.do(c.method, c.path, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", c.method, c.path, w.Code)
		}
	}
	if len(hits) != 0 {
		t.Fatalf("reached MediaMTX: %v", hits)
	}
}

func TestLiveProxyDropsCORSHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // what MediaMTX sends by default
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		io.WriteString(w, "#EXTM3U\n")
	}))
	defer upstream.Close()
	e := newEnv(t, func(d *Deps) { d.HLSBase = upstream.URL })
	e.setUp()
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()

	w := e.do("GET", "/live/hls/front-gate/index.m3u8", "")
	if w.Code != http.StatusOK {
		t.Fatalf("playlist: %d", w.Code)
	}
	for k := range w.Header() {
		if strings.HasPrefix(k, "Access-Control-") {
			t.Errorf("%s reached the browser", k)
		}
	}
}
