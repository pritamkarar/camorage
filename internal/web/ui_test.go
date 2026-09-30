package web

import (
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
