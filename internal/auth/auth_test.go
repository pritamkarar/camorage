package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHashVerify(t *testing.T) {
	h, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify("correct horse", h) {
		t.Fatal("right password rejected")
	}
	if Verify("wrong horse", h) {
		t.Fatal("wrong password accepted")
	}
	if h2, _ := Hash("correct horse"); h == h2 {
		t.Fatal("hashes must be salted")
	}
	for _, bad := range []string{h[:len(h)-2] + "AA", "garbage", "argon2id$2$19456$0$AAAA$AAAA"} {
		if Verify("correct horse", bad) {
			t.Fatalf("malformed hash %q accepted", bad)
		}
	}
}

func TestSessions(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := Sessions{Key: []byte("k1"), TTL: time.Hour, Now: func() time.Time { return now }}
	tok := s.Issue()
	if !s.Valid(tok) {
		t.Fatal("fresh token invalid")
	}
	later := s
	later.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if later.Valid(tok) {
		t.Fatal("expired token valid")
	}
	other := s
	other.Key = []byte("k2")
	if other.Valid(tok) {
		t.Fatal("token valid under another key")
	}
	exp, sig, _ := strings.Cut(tok, ".")
	if s.Valid(exp+".forged") || s.Valid("nodot") || s.Valid("") || s.Valid("9999999999."+sig) {
		t.Fatal("forged or re-dated token accepted")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter()
	l.Now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		l.Fail("1.2.3.4")
	}
	if !l.Allowed("1.2.3.4") {
		t.Fatal("locked after 4 failures")
	}
	l.Fail("1.2.3.4")
	if l.Allowed("1.2.3.4") {
		t.Fatal("not locked after 5 failures")
	}
	if !l.Allowed("5.6.7.8") {
		t.Fatal("other clients must not be affected")
	}
	now = now.Add(15*time.Minute + time.Second)
	if !l.Allowed("1.2.3.4") {
		t.Fatal("still locked after 15 minutes")
	}
	l.Fail("1.2.3.4")
	if !l.Allowed("1.2.3.4") {
		t.Fatal("one failure after the lock expired must not re-lock")
	}
}

func req(remote string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("POST", "http://cams.example.com/api/login", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		remote string
		hdr    map[string]string
		want   string
	}{
		// Cloudflare appends the address it saw, after whatever the client sent
		{"127.0.0.1:5555", map[string]string{"X-Forwarded-For": "6.6.6.6, 1.2.3.4", "CF-Connecting-IP": "1.2.3.4"}, "1.2.3.4"},
		// tailscale serve replaces X-Forwarded-For with the tailnet peer; a forged CF-Connecting-IP is ignored
		{"127.0.0.1:5555", map[string]string{"X-Forwarded-For": "100.70.24.39", "CF-Connecting-IP": "6.6.6.6"}, "100.70.24.39"},
		// one IPv6 client usually holds a whole /64: each address in it must not be a fresh start
		{"[::1]:5555", map[string]string{"X-Forwarded-For": " 2001:db8::7 "}, "2001:db8::/64"},
		// http://<tailscale-ip>:8080 handed over by userspace tailscaled: loopback, no header
		{"127.0.0.1:5555", map[string]string{"CF-Connecting-IP": "6.6.6.6"}, "127.0.0.1"},
		// anyone on the LAN could forge any header
		{"192.168.1.50:5555", map[string]string{"X-Forwarded-For": "1.2.3.4", "CF-Connecting-IP": "1.2.3.4"}, "192.168.1.50"},
		{"192.168.1.50:5555", nil, "192.168.1.50"},
	}
	for _, c := range cases {
		if got := ClientIP(req(c.remote, c.hdr)); got != c.want {
			t.Errorf("%s %v: got %s, want %s", c.remote, c.hdr, got, c.want)
		}
	}
}

func TestFromInternet(t *testing.T) {
	cases := []struct {
		remote string
		hdr    map[string]string
		want   bool
	}{
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "1.2.3.4"}, true},                    // Cloudflare
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "2001:db8::7"}, true},                // Cloudflare, IPv6
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "100.70.24.39"}, false},              // tailscale serve
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "fd7a:115c:a1e0::3b01:d011"}, false}, // tailscale serve, IPv6
		{"127.0.0.1:1", nil, false}, // http://<tailscale-ip>:8080
		{"192.168.1.50:1", map[string]string{"X-Forwarded-For": "1.2.3.4"}, false}, // LAN
	}
	for i, c := range cases {
		if got := FromInternet(req(c.remote, c.hdr)); got != c.want {
			t.Errorf("case %d %s %v: got %v, want %v", i, c.remote, c.hdr, got, c.want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		remote string
		hdr    map[string]string
		want   bool
	}{
		{"192.168.1.50:1", map[string]string{"Origin": "http://cams.example.com"}, true},
		{"192.168.1.50:1", nil, false},
		{"192.168.1.50:1", map[string]string{"Origin": "https://evil.example"}, false},
		{"127.0.0.1:1", map[string]string{"Origin": "https://phone.tail.ts.net", "X-Forwarded-Host": "phone.tail.ts.net"}, true},
		{"192.168.1.50:1", map[string]string{"Origin": "https://evil.example", "X-Forwarded-Host": "evil.example"}, false},
	}
	for i, c := range cases {
		if got := SameOrigin(req(c.remote, c.hdr)); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestCookieSecureOnlyViaTunnel(t *testing.T) {
	cases := []struct {
		remote string
		hdr    map[string]string
		secure bool
	}{
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "100.70.24.39"}, true}, // tailscale serve / cloudflared: HTTPS
		{"127.0.0.1:1", nil, false},     // http://<tailscale-ip>:8080 through userspace tailscaled: plain HTTP
		{"192.168.1.50:1", nil, false}, // LAN: plain HTTP
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		SetCookie(w, req(c.remote, c.hdr), "tok", time.Hour)
		ck := w.Result().Cookies()[0]
		if ck.Secure != c.secure || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Name != CookieName {
			t.Errorf("%s %v: %+v", c.remote, c.hdr, ck)
		}
	}
}
