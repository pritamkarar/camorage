// Package auth: admin password hashing, session cookies, login throttling and request-origin checks.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const CookieName = "camorage_session"

// argon2id at the OWASP minimum (19 MiB, 2 passes, 1 lane): ~0.3 s on the phone.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

// Hash returns "argon2id$t$m$p$salt$key" (base64, unpadded).
func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$%d$%d$%d$%s$%s", argonTime, argonMemory, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func Verify(password, encoded string) bool {
	p := strings.Split(encoded, "$")
	if len(p) != 6 || p[0] != "argon2id" {
		return false
	}
	t, err1 := strconv.ParseUint(p[1], 10, 32)
	m, err2 := strconv.ParseUint(p[2], 10, 32)
	th, err3 := strconv.ParseUint(p[3], 10, 8)
	salt, err4 := base64.RawStdEncoding.DecodeString(p[4])
	want, err5 := base64.RawStdEncoding.DecodeString(p[5])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || th == 0 || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(t), uint32(m), uint8(th), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Sessions issues stateless tokens "<expiry-unix>.<hmac>"; changing Key logs everyone out.
type Sessions struct {
	Key []byte
	TTL time.Duration
	Now func() time.Time
}

func (s Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Sessions) Issue() string {
	exp := strconv.FormatInt(s.now().Add(s.TTL).Unix(), 10)
	return exp + "." + s.sign(exp)
}

func (s Sessions) Valid(token string) bool {
	exp, sig, found := strings.Cut(token, ".")
	if !found || !hmac.Equal([]byte(sig), []byte(s.sign(exp))) {
		return false
	}
	n, err := strconv.ParseInt(exp, 10, 64)
	return err == nil && s.now().Unix() < n
}

func (s Sessions) sign(msg string) string {
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte("camorage-session|" + msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Limiter locks a client out for Lock after Max consecutive failed logins.
// ponytail: one map entry per failing IP, never pruned; fine for a personal portal.
type Limiter struct {
	Max  int
	Lock time.Duration
	Now  func() time.Time
	mu   sync.Mutex
	m    map[string]*entry
}

type entry struct {
	fails int
	until time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{Max: 5, Lock: 15 * time.Minute, Now: time.Now, m: map[string]*entry{}}
}

func (l *Limiter) Allowed(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[ip]
	return e == nil || e.fails < l.Max || !l.Now().Before(e.until)
}

func (l *Limiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[ip]
	if e == nil || (e.fails >= l.Max && !l.Now().Before(e.until)) {
		e = &entry{}
		l.m[ip] = e
	}
	e.fails++
	if e.fails >= l.Max {
		e.until = l.Now().Add(l.Lock)
	}
}

func (l *Limiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}

// FromLoopback reports whether the TCP peer is this device (cloudflared / tailscale serve).
func FromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ViaTunnel reports whether r came through cloudflared or `tailscale serve`: both connect from
// loopback and set X-Forwarded-For. Userspace tailscaled also hands a tailnet device's plain
// http://<tailscale-ip>:8080 request over from loopback, but without that header (M1c).
func ViaTunnel(r *http.Request) bool {
	return FromLoopback(r) && r.Header.Get("X-Forwarded-For") != ""
}

// forwarded is the last X-Forwarded-For entry: Cloudflare appends the address it saw and
// `tailscale serve` replaces the header with the tailnet peer, so that entry is never the client's
// own claim. Only meaningful when ViaTunnel(r).
func forwarded(r *http.Request) string {
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return ""
	}
	parts := strings.Split(xff[len(xff)-1], ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// ClientIP is the key login lockout counts. Through a tunnel it is the forwarded address, and for
// IPv6 its /64, since one client usually holds a whole /64 and each address in it would otherwise
// be a fresh start. CF-Connecting-IP is ignored: through Tailscale anyone could send one. Anyone on
// the LAN could forge any header, so every other request counts by its peer address.
func ClientIP(r *http.Request) string {
	if ViaTunnel(r) {
		if last := forwarded(r); last != "" {
			if a, err := netip.ParseAddr(last); err == nil && a.Is6() && !a.Is4In6() {
				return netip.PrefixFrom(a, 64).Masked().String()
			}
			return last
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// tailnet holds the addresses Tailscale gives its devices.
var tailnet = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

// FromInternet reports whether r came through a tunnel from outside the tailnet, i.e. through
// Cloudflare from anywhere on the internet.
func FromInternet(r *http.Request) bool {
	if !ViaTunnel(r) {
		return false
	}
	a, err := netip.ParseAddr(forwarded(r))
	if err != nil {
		return true
	}
	for _, p := range tailnet {
		if p.Contains(a.Unmap()) {
			return false
		}
	}
	return true
}

// SameOrigin is the CSRF check for state-changing requests: Origin must name this host
// (or, behind a local tunnel, the forwarded host).
func SameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	return FromLoopback(r) && u.Host == r.Header.Get("X-Forwarded-Host")
}

// SetCookie sets the session cookie; Secure when the request came through an HTTPS tunnel.
func SetCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: ViaTunnel(r)})
}

func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: ViaTunnel(r)})
}
