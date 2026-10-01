package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// The single-page app, its logo and app icons, and the PWA files (manifest.json, sw.js,
// offline.html). vendor/hls.min.js is hls.js 1.7.3 (Apache-2.0); vendor/hevc/ is hevc-wasm v0.2.0
// (MIT, its de265.wasm is libde265, LGPL-3.0), the H.265 decoder for browsers without one.
// *.mjs tests and testdata/ are not embedded.
//
//go:embed ui/*.html ui/*.css ui/*.js ui/*.svg ui/*.png ui/*.json ui/vendor/*.js ui/vendor/hevc/*
var uiFiles embed.FS

// uiHandler serves the app. "private, no-cache" makes browsers ask again on every load and keeps
// Cloudflare out: without it Cloudflare keeps .js files at its edge and tells browsers to keep them
// for a month (it overrides a plain no-cache), so an update would reach remote viewers as a mix of
// old and new modules.
// ponytail: the embedded files carry no modification time, so every load refetches them (~600 KB,
// mostly hls.js); add a content-hash ETag if that ever matters.
func uiHandler() http.Handler {
	sub, _ := fs.Sub(uiFiles, "ui")
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-cache")
		files.ServeHTTP(w, r)
	})
}

// securityHeaders sets a strict CSP (scripts and styles only from this origin, WebAssembly may be
// compiled for the H.265 decoder; media and workers also from blob:) and anti-sniffing/anti-framing
// headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
