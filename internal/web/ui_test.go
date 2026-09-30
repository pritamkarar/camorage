package web

import (
	"encoding/json"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func TestUIServedWithSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<title>camorage</title>") {
		t.Fatalf("index: %d", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self'", "style-src 'self'", "media-src 'self' blob:", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	for _, p := range []string{"/app.js", "/lib.js", "/dom.js", "/live.js", "/app.css", "/vendor/hls.min.js"} {
		if w := e.do("GET", p, ""); w.Code != http.StatusOK {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := e.do("GET", "/lib.test.mjs", ""); w.Code != http.StatusNotFound {
		t.Errorf("test file is served: %d", w.Code)
	}
	if w := e.do("GET", "/api/health", ""); w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
		t.Error("API responses lack the security headers")
	}
}

// Behind Cloudflare, files without a Cache-Control header are kept at the edge and in browsers for
// up to a month, so an update would reach remote viewers as a mix of old and new modules.
func TestUIFilesAreRevalidated(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/app.js", "/live.js", "/app.css", "/vendor/hls.min.js"} {
		// Cloudflare replaces a plain no-cache with its own month-long browser TTL; private makes
		// it pass the header through untouched (as it does for MediaMTX's segments)
		if cc := e.do("GET", p, "").Header().Get("Cache-Control"); cc != "private, no-cache" {
			t.Errorf("%s: Cache-Control %q, want private, no-cache", p, cc)
		}
	}
}

// The logo, the app icons and the PWA files are served like the rest of the UI, with the right
// types, and offline.html has nothing the CSP would block.
func TestBrandAndPWAFilesServed(t *testing.T) {
	e := newEnv(t)
	for p, want := range map[string]string{
		"/logo.svg": "image/svg+xml", "/icon-192.png": "image/png", "/icon-512.png": "image/png",
		"/icon-maskable-512.png": "image/png", "/apple-touch-icon.png": "image/png",
		"/manifest.json": "application/json", "/sw.js": "text/javascript", "/offline.html": "text/html",
	} {
		w := e.do("GET", p, "")
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), want) {
			t.Errorf("%s: %d %q, want 200 %s", p, w.Code, w.Header().Get("Content-Type"), want)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP", p)
		}
	}
	for p, px := range map[string]int{"/icon-192.png": 192, "/icon-512.png": 512, "/icon-maskable-512.png": 512, "/apple-touch-icon.png": 180} {
		c, err := png.DecodeConfig(e.do("GET", p, "").Body)
		if err != nil || c.Width != px || c.Height != px {
			t.Errorf("%s: %dx%d (%v), want %d px square", p, c.Width, c.Height, err, px)
		}
	}
	var m struct {
		Display  string `json:"display"`
		StartURL string `json:"start_url"`
		Icons    []struct{ Src, Sizes, Purpose string }
	}
	if err := json.Unmarshal(e.do("GET", "/manifest.json", "").Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, ic := range m.Icons {
		have[ic.Sizes+" "+ic.Purpose] = true
		if w := e.do("GET", ic.Src, ""); w.Code != http.StatusOK {
			t.Errorf("manifest icon %s: %d", ic.Src, w.Code)
		}
	}
	if m.Display != "standalone" || m.StartURL == "" || !have["192x192 any"] || !have["512x512 any"] || !have["512x512 maskable"] {
		t.Errorf("manifest: %+v", m)
	}
	off := e.do("GET", "/offline.html", "").Body.String()
	for _, bad := range []string{"<script", "<style", "style="} {
		if strings.Contains(off, bad) {
			t.Errorf("offline.html contains %q (the CSP blocks it)", bad)
		}
	}
}
