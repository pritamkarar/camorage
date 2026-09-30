package web

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"camorage/internal/tunnel"
)

func cfToken(secret string) string {
	b, _ := json.Marshal(map[string]string{"a": "acct", "t": "00000000-0000-4000-8000-000000000000", "s": secret})
	return base64.StdEncoding.EncodeToString(b)
}

func TestTunnelsAPI(t *testing.T) {
	changed := make(chan struct{}, 10)
	live := tunnel.Status{
		Cloudflare: tunnel.Cloudflare{Connected: true},
		Tailscale:  tunnel.Tailscale{State: "Running", URL: "https://phone.tail.ts.net/"},
	}
	e := newEnv(t, func(d *Deps) {
		d.Tunnels = func() tunnel.Status { return live }
		d.OnTunnelsChanged = func() { changed <- struct{}{} }
	})
	wait := func() {
		t.Helper()
		select {
		case <-changed:
		case <-time.After(2 * time.Second):
			t.Fatal("OnTunnelsChanged not called")
		}
	}
	e.setUp()

	got := decode[tunnel.Status](t, e.do("GET", "/api/tunnels", ""))
	if got.Cloudflare.TokenSet || got.Cloudflare.Connected || got.Tailscale.Enabled || got.Tailscale.State != "" {
		t.Fatalf("nothing configured, but shown as on: %+v", got)
	}
	if w := e.do("PUT", "/api/tunnels/cloudflare", `{"token":"","hostname":"a.example.com"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("no token saved yet: %d", w.Code)
	}
	if w := e.do("PUT", "/api/tunnels/cloudflare", `{"token":"not a token"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad token: %d", w.Code)
	}

	tok := cfToken("s3cr3t-value")
	w := e.do("PUT", "/api/tunnels/cloudflare", `{"token":"cloudflared service install `+tok+`","hostname":" HTTPS://Camorage.Example.com/ "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	wait()
	if e.store.Get().Tunnels.CloudflareToken != tok {
		t.Fatalf("stored token %q", e.store.Get().Tunnels.CloudflareToken)
	}
	w = e.do("GET", "/api/tunnels", "")
	if strings.Contains(w.Body.String(), tok) {
		t.Fatal("the API returned the token")
	}
	got = decode[tunnel.Status](t, w)
	if !got.Cloudflare.TokenSet || got.Cloudflare.Hostname != "camorage.example.com" || !got.Cloudflare.Connected {
		t.Fatalf("after save: %+v", got)
	}

	// an empty token keeps the saved one; a bad hostname is refused
	if w := e.do("PUT", "/api/tunnels/cloudflare", `{"token":"","hostname":"cams.example.com"}`); w.Code != http.StatusOK {
		t.Fatalf("hostname only: %d %s", w.Code, w.Body)
	}
	wait()
	if c := e.store.Get().Tunnels; c.CloudflareToken != tok || c.CloudflareHostname != "cams.example.com" {
		t.Fatalf("after hostname change: %+v", c)
	}
	if w := e.do("PUT", "/api/tunnels/cloudflare", `{"hostname":"not a host"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad hostname: %d", w.Code)
	}

	if w := e.do("DELETE", "/api/tunnels/cloudflare", ""); w.Code != http.StatusOK {
		t.Fatalf("remove: %d", w.Code)
	}
	wait()
	if c := e.store.Get().Tunnels; c.CloudflareToken != "" || c.CloudflareHostname != "" {
		t.Fatalf("after remove: %+v", c)
	}

	if w := e.do("PUT", "/api/tunnels/tailscale", `{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("tailscale on: %d", w.Code)
	}
	wait()
	got = decode[tunnel.Status](t, e.do("GET", "/api/tunnels", ""))
	if !got.Tailscale.Enabled || got.Tailscale.URL != "https://phone.tail.ts.net/" {
		t.Fatalf("tailscale: %+v", got.Tailscale)
	}

	if w := e.do("PUT", "/api/tunnels/tailscale", `{"enabled":false}`, noOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", w.Code)
	}
	e.cookie = nil
	if w := e.do("GET", "/api/tunnels", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", w.Code)
	}
}
