# M1b Local Portal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put a web UI on top of the M1a core, so everything is usable from a browser on the home network:
- **Setup and sign-in:** first-run setup, login.
- **Live:** a live grid, and a full-screen view using WebRTC with an HLS fallback.
- **Playback:** a 24-hour timeline.
- **Cameras:** add/edit/delete, a weekly schedule editor, ONVIF network scan.
- **Settings:** phone health, password change, sign-out.

**Architecture:**
- **Go additions to M1a:**
  - `platform.Volumes`: storage choices at setup.
  - `internal/onvif`: WS-Discovery plus SOAP stream lookup.
  - `web` handlers: setup volumes, password change, ONVIF.
  - A **live proxy** that forwards `/live/hls/...` and `/live/whep/...` to MediaMTX's loopback servers, so one origin and one login cover everything.
- **The UI:** vanilla ES modules plus a vendored hls.js, embedded in the binary with `go:embed`.
  - **Tested logic:** all decision logic (day boundaries in the phone's timezone, timeline math, schedule text) lives in `lib.js` with `node --test` tests.
  - **DOM code:** the view modules are thin DOM glue, verified in a real browser against the phone in Task 13.
- **Remote access (Cloudflare/Tailscale) is M1c.**

**Tech Stack:** Go 1.27 stdlib (+ `golang.org/x/crypto` already present), MediaMTX v1.21.1, hls.js 1.7.3 (vendored), plain HTML/CSS/ES modules (no build step, no npm), Node 24 for `node --test`, Chrome via the chrome-devtools MCP for acceptance.

**Spec:** `docs/superpowers/specs/2026-09-30-camorage-portal-design.md` (§6 UI, §3 components, §7 security; M1 row of §10 minus remote access). Builds on M1a, merged on `main`.

## Global Constraints

- Work on branch `m1b-ui` created from `main`; commit after each task with the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Module `camorage`; no new Go dependencies. **UI: no npm packages and no build step.** The only third-party UI file is `internal/web/ui/vendor/hls.min.js` = **hls.js 1.7.3**.
- **CSP on every response:** `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`.
  - That means **no inline `<script>`, `<style>` or `style="…"`**. Set element styles through `el.style` (the `h()` helper takes a `style` object).
- **User-supplied text** (camera names, errors) is only ever inserted as text (`h()` / `textContent`), never as HTML.
- **HTTP 401 means only "not signed in".** The UI treats every 401 as a sign-out. A camera rejecting ONVIF credentials returns **400**, and a wrong current password on password change returns **403**.
- **Live proxy:**
  - **Allowed paths:** only MediaMTX paths of *enabled* cameras (`<id>` and, if there is a substream, `<id>_sub`); everything else is 404.
  - **Cookies:** strip the browser's `Cookie` header before forwarding, and drop MediaMTX's `Set-Cookie`. MediaMTX falls back to `?session=` URLs, verified on the phone.
  - **Locations:** rewrite absolute `Location` headers. HLS redirects become `/live/hls` + loc. WHEP sessions go from `/<path>/whep/<id>` to `/live/whep/<path>/<id>`.
- **Time:**
  - **The UI shows the phone's local time, whatever the browser's timezone.** The phone's UTC offset comes from `/api/status` `.time` (RFC 3339 with offset). Day boundaries use `dayStart(date, offset)`.
  - Playback `start` values are RFC 3339 **with the phone's offset**.
- **Live view:** tiles use the substream (`<id>_sub`) when the camera has one. Full screen uses the main stream via WebRTC (WHEP, non-trickle) and falls back to HLS if no frame arrives within 4 s.
- **Playback:** fetched in 10-minute chunks (`duration=600`) as fMP4. The next chunk starts where the previous one ended; gaps snap forward to the next recorded span.
- **Phone dev access (unchanged from M1a):**
  - Connect with `. spike/env.sh; $P '<cmd>'` and deploy with `scripts/deploy.sh`.
  - Launch background processes as `( nohup … </dev/null & )`.
  - Never `pkill -f` a pattern that also appears in the same SSH command.
- **The admin password** is in `.cache/admin-password` (generated in M1a). Task 13 signs in with it; the user changes it afterwards in Settings.

## Review Focus

1. **MediaMTX restarts while live tiles are open** (a camera save, a crash, a camera reboot). Tiles must recover by themselves within ~20 s, with no page reload. → Task 13, step 7.
2. **The session ends while a page is open** (sign-out elsewhere, password change, 30-day expiry). The app must show the login screen once, with no error loop and no stale view. → Task 13, step 8.
3. **A camera name containing HTML/JS** (`<img src=x onerror=…>`) must show as literal text and never run. → Task 13, step 6.
4. **Phone-sized screen (390 px wide).** Every page must be usable with no horizontal scrolling. → Task 13, step 9.
5. **WebRTC unavailable** (UDP blocked, or a browser without `RTCPeerConnection`). Full screen must fall back to HLS and play within ~6 s. → Task 13, step 5.

---

### Task 1: Storage choices for first-run setup (`platform.Volumes`)

**Files:**
- Create: `internal/platform/volumes.go`
- Test: `internal/platform/volumes_test.go`

**Interfaces:**
- Consumes: `platform.Disk(path) (free, total uint64, err error)` (M1a)
- Produces:
  - `platform.Volume{Path, Label string; FreeMB, TotalMB uint64}` (JSON `path`, `label`, `freeMB`, `totalMB`).
  - `platform.Volumes() []Volume`.
  - `platform.VolumesUnder(storage, home string) []Volume`: each SD card's `<storage>/<card>/Android/data/com.termux/files/camorage-rec` (skipping `emulated`, `self` and cards without that folder), then `<home>/camorage-rec`.

- [ ] **Step 0: Branch**

Run: `cd ~/src/camorage && git switch -c m1b-ui && git branch --show-current`
Expected: `m1b-ui`.

- [ ] **Step 1: Write the failing tests** in `internal/platform/volumes_test.go`

```go
package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVolumesUnder(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	sd := filepath.Join(storage, "1234-ABCD", "Android", "data", "com.termux", "files")
	os.MkdirAll(sd, 0o755)
	os.MkdirAll(filepath.Join(storage, "emulated", "0", "Android", "data", "com.termux", "files"), 0o755)
	os.MkdirAll(filepath.Join(storage, "ABCD-0000"), 0o755) // an SD card Termux has no folder on
	home := filepath.Join(root, "home")
	os.MkdirAll(home, 0o755)

	got := VolumesUnder(storage, home)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Path != filepath.Join(sd, "camorage-rec") || got[0].Label != "SD card 1234-ABCD" || got[0].TotalMB == 0 {
		t.Fatalf("SD volume = %+v", got[0])
	}
	if got[1].Path != filepath.Join(home, "camorage-rec") || got[1].Label != "Phone storage" {
		t.Fatalf("home volume = %+v", got[1])
	}
}

func TestVolumesUnderMissingStorage(t *testing.T) {
	if got := VolumesUnder(filepath.Join(t.TempDir(), "nope"), ""); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/platform/`
Expected: FAIL (undefined: `VolumesUnder`).

- [ ] **Step 3: Implement** `internal/platform/volumes.go`

```go
package platform

import (
	"os"
	"path/filepath"
)

// Volume is a candidate recordings folder offered at first-run setup.
type Volume struct {
	Path    string `json:"path"`  // folder camorage creates and records into
	Label   string `json:"label"` // "SD card 1234-ABCD" or "Phone storage"
	FreeMB  uint64 `json:"freeMB"`
	TotalMB uint64 `json:"totalMB"`
}

// Volumes lists writable places for recordings: each SD card's Termux app folder (the only SD
// location Termux can write without root), then Termux's home directory.
func Volumes() []Volume {
	home, _ := os.UserHomeDir()
	return VolumesUnder("/storage", home)
}

// VolumesUnder is Volumes with the storage root and home made explicit (for tests).
func VolumesUnder(storage, home string) []Volume {
	var out []Volume
	entries, _ := os.ReadDir(storage)
	for _, e := range entries {
		if e.Name() == "emulated" || e.Name() == "self" {
			continue
		}
		dir := filepath.Join(storage, e.Name(), "Android", "data", "com.termux", "files")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out = append(out, volume(filepath.Join(dir, "camorage-rec"), dir, "SD card "+e.Name()))
		}
	}
	if home != "" {
		out = append(out, volume(filepath.Join(home, "camorage-rec"), home, "Phone storage"))
	}
	return out
}

func volume(path, statDir, label string) Volume {
	v := Volume{Path: path, Label: label}
	if free, total, err := Disk(statDir); err == nil {
		v.FreeMB, v.TotalMB = free>>20, total>>20
	}
	return v
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/platform/`
Expected: `ok  camorage/internal/platform`.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/volumes.go internal/platform/volumes_test.go
git commit -m "feat(platform): list recording volumes for first-run setup

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: ONVIF client (WS-Discovery + stream URIs)

**Files:**
- Create: `internal/onvif/onvif.go`
- Test: `internal/onvif/onvif_test.go`

**Interfaces:**
- Produces:
  - **Errors:** `onvif.ErrAuth`.
  - **Types:** `onvif.Device{XAddr, IP, Name, Hardware string}` (JSON `xaddr`, `ip`, `name`, `hardware`), `onvif.Profile{Token, Encoding string; Width, Height int; URI string}` (JSON `token`, `encoding`, `width`, `height`, `uri`).
  - **Discovery and streams:** `onvif.Discover(ctx, timeout time.Duration) ([]Device, error)`, `onvif.Streams(ctx, xaddr, user, pass string) ([]Profile, error)`. Streams only accepts `http`/`https` xaddrs; WS-UsernameToken timestamps use the camera's clock.
  - **Helpers:** `onvif.MainAndSub(ps []Profile, user, pass string) (main, sub string)` (largest resolution = main, smallest = sub, "" if only one), `onvif.WithCredentials(uri, user, pass string) string` (percent-encodes).
  - **Production backend:** `onvif.LAN{}` with methods `Discover(ctx) ([]Device, error)` (3 s) and `Streams(ctx, xaddr, user, pass) ([]Profile, error)`.

- [ ] **Step 1: Write the failing tests** in `internal/onvif/onvif_test.go`

```go
package onvif

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A ProbeMatch as sent by the real camera (macro-video-soft).
const probeMatchXML = `<?xml version="1.0" encoding="UTF-8"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:wsdd="http://schemas.xmlsoap.org/ws/2005/04/discovery"><SOAP-ENV:Body><wsdd:ProbeMatches><wsdd:ProbeMatch><wsdd:XAddrs>http://192.168.1.129:8899/onvif/device_service</wsdd:XAddrs><wsdd:Scopes>onvif://www.onvif.org/location/country/china onvif://www.onvif.org/name/IP-Camera onvif://www.onvif.org/hardware/IPC%20BO</wsdd:Scopes></wsdd:ProbeMatch></wsdd:ProbeMatches></SOAP-ENV:Body></SOAP-ENV:Envelope>`

func TestDiscoverCollectsProbeMatches(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() { // a fake camera: answers every probe (the client sends two)
		buf := make([]byte, 64<<10)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if bytes.Contains(buf[:n], []byte("NetworkVideoTransmitter")) {
				pc.WriteTo([]byte(probeMatchXML), from)
			}
		}
	}()
	devs, err := discover(context.Background(), pc.LocalAddr().String(), 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("want 1 device (duplicates merged), got %+v", devs)
	}
	d := devs[0]
	if d.XAddr != "http://192.168.1.129:8899/onvif/device_service" || d.IP != "127.0.0.1" || d.Name != "IP-Camera" || d.Hardware != "IPC BO" {
		t.Fatalf("device = %+v", d)
	}
}

// validToken checks a WS-UsernameToken digest like a camera does (±5 min around its own clock).
func validToken(body, user, pass string, camNow time.Time) bool {
	if user == "" {
		return true
	}
	get := func(tag string) string {
		m := regexp.MustCompile(`<(?:\w+:)?` + tag + `[^>]*>([^<]*)<`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		return m[1]
	}
	nonce, err := base64.StdEncoding.DecodeString(get("Nonce"))
	if err != nil || get("Username") != user {
		return false
	}
	created := get("Created")
	ts, err := time.Parse("2006-01-02T15:04:05Z", created)
	if err != nil || ts.Sub(camNow) > 5*time.Minute || camNow.Sub(ts) > 5*time.Minute {
		return false
	}
	sum := sha1.Sum([]byte(string(nonce) + created + pass))
	return get("Password") == base64.StdEncoding.EncodeToString(sum[:])
}

// fakeCamera speaks just enough ONVIF: clock, capabilities, two profiles, their stream URIs.
func fakeCamera(t *testing.T, user, pass string, clockSkew time.Duration) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		reply := func(code int, inner string) {
			w.Header().Set("Content-Type", "application/soap+xml")
			w.WriteHeader(code)
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body>%s</s:Body></s:Envelope>`, inner)
		}
		camNow := time.Now().UTC().Add(clockSkew)
		if strings.Contains(body, "GetSystemDateAndTime") {
			reply(200, fmt.Sprintf(`<tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime><tt:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time><tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
				camNow.Hour(), camNow.Minute(), camNow.Second(), camNow.Year(), int(camNow.Month()), camNow.Day()))
			return
		}
		if !validToken(body, user, pass, camNow) {
			reply(400, `<s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:NotAuthorized</s:Value></s:Subcode></s:Code></s:Fault>`)
			return
		}
		switch {
		case strings.Contains(body, "GetCapabilities"):
			reply(200, `<tds:GetCapabilitiesResponse><tds:Capabilities><tt:Media><tt:XAddr>`+srv.URL+`/onvif/media_service</tt:XAddr></tt:Media></tds:Capabilities></tds:GetCapabilitiesResponse>`)
		case strings.Contains(body, "GetProfiles"):
			reply(200, `<trt:GetProfilesResponse><trt:Profiles token="PROFILE_000"><tt:VideoEncoderConfiguration><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1280</tt:Width><tt:Height>720</tt:Height></tt:Resolution></tt:VideoEncoderConfiguration></trt:Profiles><trt:Profiles token="PROFILE_001"><tt:VideoEncoderConfiguration><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution></tt:VideoEncoderConfiguration></trt:Profiles></trt:GetProfilesResponse>`)
		case strings.Contains(body, "GetStreamUri"):
			ch := "0"
			if strings.Contains(body, "PROFILE_001") {
				ch = "1"
			}
			reply(200, `<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>rtsp://192.168.1.129/live/ch00_`+ch+`</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`)
		default:
			reply(400, `<s:Fault/>`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamsWithAuthAndCameraClockSkew(t *testing.T) {
	srv := fakeCamera(t, "admin", "p#ss", time.Hour) // the camera's clock is an hour ahead
	ps, err := Streams(context.Background(), srv.URL+"/onvif/device_service", "admin", "p#ss")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Token != "PROFILE_000" || ps[0].Width != 1280 || ps[0].Encoding != "H264" || ps[1].URI != "rtsp://192.168.1.129/live/ch00_1" {
		t.Fatalf("profiles = %+v", ps)
	}
}

func TestStreamsWithoutAuth(t *testing.T) {
	srv := fakeCamera(t, "", "", 0)
	if ps, err := Streams(context.Background(), srv.URL+"/onvif/device_service", "", ""); err != nil || len(ps) != 2 {
		t.Fatalf("got %+v, %v", ps, err)
	}
}

func TestStreamsWrongPassword(t *testing.T) {
	srv := fakeCamera(t, "admin", "right", 0)
	if _, err := Streams(context.Background(), srv.URL+"/onvif/device_service", "admin", "wrong"); !errors.Is(err, ErrAuth) {
		t.Fatalf("want ErrAuth, got %v", err)
	}
}

func TestStreamsRejectsNonHTTPXAddr(t *testing.T) {
	if _, err := Streams(context.Background(), "file:///etc/passwd", "", ""); err == nil {
		t.Fatal("expected an error for a non-http xaddr")
	}
}

func TestMainAndSubEncodesCredentials(t *testing.T) {
	ps := []Profile{{Token: "b", Width: 640, Height: 360, URI: "rtsp://1.2.3.4/sub"}, {Token: "a", Width: 1280, Height: 720, URI: "rtsp://1.2.3.4/main"}}
	main, sub := MainAndSub(ps, "admin", "p#ss/1")
	if main != "rtsp://admin:p%23ss%2F1@1.2.3.4/main" || sub != "rtsp://admin:p%23ss%2F1@1.2.3.4/sub" {
		t.Fatalf("main=%s sub=%s", main, sub)
	}
	main, sub = MainAndSub(ps[:1], "", "")
	if main != "rtsp://1.2.3.4/sub" || sub != "" {
		t.Fatalf("single profile: main=%s sub=%s", main, sub)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/onvif/`
Expected: FAIL (undefined: `discover`, `Streams`, …).

- [ ] **Step 3: Implement** `internal/onvif/onvif.go`

```go
// Package onvif finds cameras on the LAN (WS-Discovery) and asks them for their RTSP stream URLs.
package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrAuth means the camera rejected the user name or password.
var ErrAuth = errors.New("camera rejected the user name or password")

// Device is a camera that answered a WS-Discovery probe.
type Device struct {
	XAddr    string `json:"xaddr"` // device service URL
	IP       string `json:"ip"`
	Name     string `json:"name"`
	Hardware string `json:"hardware"`
}

// Profile is one media profile of a camera with its RTSP URI (as the camera reports it).
type Profile struct {
	Token    string `json:"token"`
	Encoding string `json:"encoding"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	URI      string `json:"uri"`
}

// LAN is the production backend: a 3-second discovery on the phone's network.
type LAN struct{}

func (LAN) Discover(ctx context.Context) ([]Device, error) { return Discover(ctx, 3*time.Second) }

func (LAN) Streams(ctx context.Context, xaddr, user, pass string) ([]Profile, error) {
	return Streams(ctx, xaddr, user, pass)
}

const probeTarget = "239.255.255.250:3702"

// Discover multicasts a WS-Discovery probe for video transmitters and collects the answers.
func Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	return discover(ctx, probeTarget, timeout)
}

func discover(ctx context.Context, target string, timeout time.Duration) ([]Device, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	dst, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return nil, err
	}
	probe := probeMessage()
	for i := 0; i < 2; i++ { // UDP may drop one
		if _, err := conn.WriteToUDP(probe, dst); err != nil {
			return nil, err
		}
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	seen := map[string]bool{}
	var out []Device
	buf := make([]byte, 64<<10)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // deadline reached
		}
		dev, ok := parseProbeMatch(buf[:n])
		if !ok || seen[dev.XAddr] {
			continue
		}
		seen[dev.XAddr] = true
		dev.IP = from.IP.String()
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

func probeMessage() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<e:Header><w:MessageID>uuid:` + newUUID() + `</w:MessageID><w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>` +
		`<w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header>` +
		`<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`)
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func parseProbeMatch(b []byte) (Device, bool) {
	var env struct {
		Matches []struct {
			XAddrs string `xml:"XAddrs"`
			Scopes string `xml:"Scopes"`
		} `xml:"Body>ProbeMatches>ProbeMatch"`
	}
	if xml.Unmarshal(b, &env) != nil || len(env.Matches) == 0 {
		return Device{}, false
	}
	m := env.Matches[0]
	xaddrs := strings.Fields(m.XAddrs)
	if len(xaddrs) == 0 {
		return Device{}, false
	}
	d := Device{XAddr: xaddrs[0]}
	for _, s := range strings.Fields(m.Scopes) {
		if i := strings.Index(s, "onvif.org/name/"); i >= 0 {
			d.Name, _ = url.PathUnescape(s[i+len("onvif.org/name/"):])
		}
		if i := strings.Index(s, "onvif.org/hardware/"); i >= 0 {
			d.Hardware, _ = url.PathUnescape(s[i+len("onvif.org/hardware/"):])
		}
	}
	return d, true
}

// Streams asks the camera at xaddr (its device-service URL) for the RTSP URI of every profile.
// ponytail: WS-Security digest only; cameras that want HTTP Digest auth need it added here.
func Streams(ctx context.Context, xaddr, user, pass string) ([]Profile, error) {
	u, err := url.Parse(xaddr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("xaddr must be an http(s) URL")
	}
	c := &client{http: &http.Client{Timeout: 8 * time.Second}, user: user, pass: pass}
	c.syncClock(ctx, xaddr)

	var caps struct {
		XAddr string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
	}
	if err := c.call(ctx, xaddr, `<tds:GetCapabilities><tds:Category>Media</tds:Category></tds:GetCapabilities>`, &caps); err != nil {
		return nil, fmt.Errorf("GetCapabilities: %w", err)
	}
	media := caps.XAddr
	if media == "" {
		media = xaddr
	}
	var profs struct {
		Profiles []struct {
			Token    string `xml:"token,attr"`
			Encoding string `xml:"VideoEncoderConfiguration>Encoding"`
			Width    int    `xml:"VideoEncoderConfiguration>Resolution>Width"`
			Height   int    `xml:"VideoEncoderConfiguration>Resolution>Height"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	if err := c.call(ctx, media, `<trt:GetProfiles/>`, &profs); err != nil {
		return nil, fmt.Errorf("GetProfiles: %w", err)
	}
	var out []Profile
	for _, p := range profs.Profiles {
		var su struct {
			URI string `xml:"Body>GetStreamUriResponse>MediaUri>Uri"`
		}
		body := `<trt:GetStreamUri><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup>` +
			`<trt:ProfileToken>` + xmlEscape(p.Token) + `</trt:ProfileToken></trt:GetStreamUri>`
		if err := c.call(ctx, media, body, &su); err != nil {
			return nil, fmt.Errorf("GetStreamUri %s: %w", p.Token, err)
		}
		out = append(out, Profile{Token: p.Token, Encoding: p.Encoding, Width: p.Width, Height: p.Height, URI: su.URI})
	}
	if len(out) == 0 {
		return nil, errors.New("the camera reported no media profiles")
	}
	return out, nil
}

type client struct {
	http       *http.Client
	user, pass string
	offset     time.Duration // camera clock minus our clock
}

// syncClock reads the camera's UTC clock (allowed without credentials) so WS-Security timestamps
// fall within the camera's tolerance even when its clock is wrong.
func (c *client) syncClock(ctx context.Context, xaddr string) {
	user := c.user
	c.user = ""
	defer func() { c.user = user }()
	var r struct {
		D struct {
			Hour   int `xml:"Time>Hour"`
			Minute int `xml:"Time>Minute"`
			Second int `xml:"Time>Second"`
			Year   int `xml:"Date>Year"`
			Month  int `xml:"Date>Month"`
			Day    int `xml:"Date>Day"`
		} `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime"`
	}
	if c.call(ctx, xaddr, `<tds:GetSystemDateAndTime/>`, &r) == nil && r.D.Year > 2000 {
		cam := time.Date(r.D.Year, time.Month(r.D.Month), r.D.Day, r.D.Hour, r.D.Minute, r.D.Second, 0, time.UTC)
		c.offset = time.Until(cam)
	}
}

func (c *client) call(ctx context.Context, to, body string, out any) error {
	env := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
		c.header() + `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to, strings.NewReader(env))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || bytes.Contains(b, []byte("NotAuthorized")) {
		return ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return xml.Unmarshal(b, out)
}

// header is a WS-Security UsernameToken with a password digest: base64(sha1(nonce+created+pass)).
func (c *client) header() string {
	if c.user == "" {
		return ""
	}
	nonce := make([]byte, 16)
	rand.Read(nonce)
	created := time.Now().Add(c.offset).UTC().Format("2006-01-02T15:04:05Z")
	sum := sha1.Sum(append(append(append([]byte{}, nonce...), created...), c.pass...))
	return `<s:Header><Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"><UsernameToken>` +
		`<Username>` + xmlEscape(c.user) + `</Username>` +
		`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + base64.StdEncoding.EncodeToString(sum[:]) + `</Password>` +
		`<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` + base64.StdEncoding.EncodeToString(nonce) + `</Nonce>` +
		`<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` + created + `</Created>` +
		`</UsernameToken></Security></s:Header>`
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// MainAndSub picks the highest-resolution profile as the main stream and the lowest as the
// substream ("" when there is only one), with credentials embedded.
func MainAndSub(ps []Profile, user, pass string) (main, sub string) {
	if len(ps) == 0 {
		return "", ""
	}
	sorted := append([]Profile(nil), ps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Width*sorted[i].Height > sorted[j].Width*sorted[j].Height
	})
	main = WithCredentials(sorted[0].URI, user, pass)
	if last := sorted[len(sorted)-1]; len(sorted) > 1 && last.URI != sorted[0].URI {
		sub = WithCredentials(last.URI, user, pass)
	}
	return main, sub
}

// WithCredentials puts user:pass into an RTSP URI, percent-encoded (so # / ? @ are safe).
func WithCredentials(uri, user, pass string) string {
	if user == "" {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	u.User = url.UserPassword(user, pass)
	return u.String()
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/onvif/`
Expected: `ok  camorage/internal/onvif`.

- [ ] **Step 5: Commit**

```bash
git add internal/onvif
git commit -m "feat(onvif): WS-Discovery and stream URI lookup with WS-Security digest

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Setup volumes and password change API

**Files:**
- Create: `internal/web/account.go`
- Test: `internal/web/account_test.go`
- Modify: `internal/web/web.go` (Deps field, two routes), `internal/web/web_test.go` (`newEnv` takes Deps modifiers)

**Interfaces:**
- Consumes: `platform.Volume` (Task 1); M1a `auth`, `config.Store`, `s.sessions()`, `s.authMu`, `readJSON`, `fail`, `writeJSON`, `okBody`.
- Produces:
  - **New Deps field:** `Deps.Volumes func() []platform.Volume`.
  - **`GET /api/setup/volumes`:** unauthenticated; 409 once set up. Returns `[{path,label,freeMB,totalMB}]`.
  - **`POST /api/password {current, next}`:** needs a session plus same-origin. Returns 403 for a wrong current password (never 401), 400 if the new one is shorter than 8, and 429 when locked out. On success it rotates the session key (other browsers are signed out) and sets a fresh cookie.
  - **Test helper:** `newEnv(t *testing.T, mods ...func(*Deps)) *env`.

- [ ] **Step 1: Let tests customise Deps.** In `internal/web/web_test.go`, replace the whole `newEnv` function (from `func newEnv(t *testing.T) *env {` to its closing `}`) with:

```go
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
```

Run: `go test ./internal/web/`
Expected: `ok` (a pure refactor; all M1a web tests still pass).

- [ ] **Step 2: Write the failing tests** in `internal/web/account_test.go`

```go
package web

import (
	"net/http"
	"testing"

	"camorage/internal/platform"
)

func TestSetupVolumesOnlyBeforeSetup(t *testing.T) {
	e := newEnv(t, func(d *Deps) {
		d.Volumes = func() []platform.Volume { return []platform.Volume{{Path: "/sd/camorage-rec", Label: "SD card X", FreeMB: 5000}} }
	})
	w := e.do("GET", "/api/setup/volumes", "")
	vols := decode[[]platform.Volume](t, w)
	if w.Code != http.StatusOK || len(vols) != 1 || vols[0].Label != "SD card X" {
		t.Fatalf("volumes: %d %+v", w.Code, vols)
	}
	e.setUp()
	if w := e.do("GET", "/api/setup/volumes", ""); w.Code != http.StatusConflict {
		t.Fatalf("after setup: %d, want 409", w.Code)
	}
}

func TestChangePasswordRotatesSessions(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	old := e.cookie
	if w := e.do("POST", "/api/password", `{"current":"wrong-horse","next":"new-password-1"}`); w.Code != http.StatusForbidden {
		t.Fatalf("wrong current: %d, want 403 (never 401, which the UI reads as signed out)", w.Code)
	}
	if w := e.do("POST", "/api/password", `{"current":"correct-horse","next":"short"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("short new password: %d", w.Code)
	}
	if w := e.do("POST", "/api/password", `{"current":"correct-horse","next":"new-password-1"}`, noOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", w.Code)
	}
	w := e.do("POST", "/api/password", `{"current":"correct-horse","next":"new-password-1"}`)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) == 0 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	fresh := w.Result().Cookies()[0]

	e.cookie = old
	if w := e.do("GET", "/api/cameras", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("old session still valid: %d", w.Code)
	}
	e.cookie = fresh
	if w := e.do("GET", "/api/cameras", ""); w.Code != http.StatusOK {
		t.Fatalf("fresh session: %d", w.Code)
	}
	e.cookie = nil
	if w := e.do("POST", "/api/login", `{"password":"new-password-1"}`); w.Code != http.StatusOK {
		t.Fatalf("login with new password: %d", w.Code)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/web/`
Expected: FAIL (`d.Volumes undefined`).

- [ ] **Step 4: Implement.** In `internal/web/web.go`:
  - Add a field to `Deps` right after `HTTP             *http.Client`:
```go
	Volumes          func() []platform.Volume // first-run recording folder choices
```
  - Insert these two routes in `New`, before the line `return mux`:
```go
	mux.HandleFunc("GET /api/setup/volumes", s.volumes)
	mux.Handle("POST /api/password", s.authed(s.changePassword))
```

Create `internal/web/account.go`:
```go
package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"camorage/internal/auth"
	"camorage/internal/config"
	"camorage/internal/platform"
)

// volumes lists candidate recording folders. It is unauthenticated, so only before setup.
func (s *server) volumes(w http.ResponseWriter, r *http.Request) {
	if s.d.Store.Get().Admin.Hash != "" {
		fail(w, http.StatusConflict, "already set up")
		return
	}
	vols := []platform.Volume{}
	if s.d.Volumes != nil {
		vols = append(vols, s.d.Volumes()...)
	}
	writeJSON(w, http.StatusOK, vols)
}

// changePassword checks the current password, stores the new one and rotates the session key,
// which signs out every other browser. A wrong current password is 403: the UI reads 401 as
// "signed out".
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Next) < 8 {
		fail(w, http.StatusBadRequest, "the new password must be at least 8 characters")
		return
	}
	ip := auth.ClientIP(r)
	if !s.d.Limiter.Allowed(ip) {
		fail(w, http.StatusTooManyRequests, "too many failed attempts; try again in 15 minutes")
		return
	}
	if !auth.Verify(in.Current, s.d.Store.Get().Admin.Hash) {
		s.d.Limiter.Fail(ip)
		fail(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	hash, err := auth.Hash(in.Next)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.d.Store.Update(func(c *config.Config) error {
		c.Admin.Hash, c.SessionKey = hash, hex.EncodeToString(key)
		return nil
	}); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	auth.SetCookie(w, r, s.sessions().Issue(), sessionTTL)
	writeJSON(w, http.StatusOK, okBody)
}
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -l internal/ ; go test -race ./internal/web/`
Expected: no gofmt output; `ok  camorage/internal/web`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/account.go internal/web/account_test.go internal/web/web.go internal/web/web_test.go
git commit -m "feat(web): setup volume choices and password change (rotates sessions)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: ONVIF API endpoints

**Files:**
- Create: `internal/web/onvif.go`
- Test: `internal/web/onvif_test.go`
- Modify: `internal/web/web.go` (Deps field, two routes)

**Interfaces:**
- Consumes: `onvif.Device`, `onvif.Profile`, `onvif.ErrAuth`, `onvif.MainAndSub` (Task 2); `newEnv(t, mods...)` (Task 3).
- Produces:
  - **Backend interface:** `web.ONVIF` `{Discover(ctx) ([]onvif.Device, error); Streams(ctx, xaddr, user, pass string) ([]onvif.Profile, error)}`, satisfied by `onvif.LAN{}`, plus the Deps field `Deps.ONVIF ONVIF`.
  - **`POST /api/onvif/discover`:** needs a session plus same-origin; returns `[Device]`.
  - **`POST /api/onvif/streams {xaddr,user,pass}`:** returns `{mainUrl, subUrl, profiles}` with the credentials percent-encoded into the URLs. A camera auth failure is **400**; any other camera error is 502.

- [ ] **Step 1: Write the failing tests** in `internal/web/onvif_test.go`

```go
package web

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"camorage/internal/onvif"
)

type fakeONVIF struct {
	devs     []onvif.Device
	profiles []onvif.Profile
	err      error
	got      [3]string
}

func (f *fakeONVIF) Discover(context.Context) ([]onvif.Device, error) { return f.devs, f.err }

func (f *fakeONVIF) Streams(_ context.Context, xaddr, user, pass string) ([]onvif.Profile, error) {
	f.got = [3]string{xaddr, user, pass}
	return f.profiles, f.err
}

func TestONVIFDiscoverAndStreams(t *testing.T) {
	f := &fakeONVIF{
		devs: []onvif.Device{{XAddr: "http://192.168.1.129:8899/onvif/device_service", IP: "192.168.1.129", Name: "IP-Camera"}},
		profiles: []onvif.Profile{
			{Token: "p0", Width: 1280, Height: 720, URI: "rtsp://192.168.1.129/live/ch00_0"},
			{Token: "p1", Width: 640, Height: 360, URI: "rtsp://192.168.1.129/live/ch00_1"},
		},
	}
	e := newEnv(t, func(d *Deps) { d.ONVIF = f })
	e.setUp()
	devs := decode[[]onvif.Device](t, e.do("POST", "/api/onvif/discover", ""))
	if len(devs) != 1 || devs[0].IP != "192.168.1.129" {
		t.Fatalf("devices = %+v", devs)
	}
	w := e.do("POST", "/api/onvif/streams", `{"xaddr":"http://192.168.1.129:8899/onvif/device_service","user":"admin","pass":"p#ss"}`)
	got := decode[map[string]any](t, w)
	if w.Code != http.StatusOK || got["mainUrl"] != "rtsp://admin:p%23ss@192.168.1.129/live/ch00_0" || got["subUrl"] != "rtsp://admin:p%23ss@192.168.1.129/live/ch00_1" {
		t.Fatalf("streams: %d %v", w.Code, got)
	}
	if f.got != [3]string{"http://192.168.1.129:8899/onvif/device_service", "admin", "p#ss"} {
		t.Fatalf("backend got %v", f.got)
	}
	if w := e.do("POST", "/api/onvif/discover", "", noOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin discover: %d", w.Code)
	}
}

func TestONVIFWrongPasswordIsNotASignOut(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.ONVIF = &fakeONVIF{err: fmt.Errorf("GetCapabilities: %w", onvif.ErrAuth)} })
	e.setUp()
	w := e.do("POST", "/api/onvif/streams", `{"xaddr":"http://1.2.3.4/onvif/device_service","user":"admin","pass":"x"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (a 401 would sign the user out of the portal UI)", w.Code)
	}
}

func TestONVIFCameraUnreachable(t *testing.T) {
	e := newEnv(t, func(d *Deps) { d.ONVIF = &fakeONVIF{err: fmt.Errorf("dial tcp 1.2.3.4:80: timeout")} })
	e.setUp()
	if w := e.do("POST", "/api/onvif/streams", `{"xaddr":"http://1.2.3.4/onvif/device_service"}`); w.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web/`
Expected: FAIL (`d.ONVIF undefined`).

- [ ] **Step 3: Implement.** In `internal/web/web.go`:
  - Add a field to `Deps` after the `Volumes` line:
```go
	ONVIF            ONVIF                    // camera discovery backend (onvif.LAN{} in production)
```
  - Insert these routes in `New`, before `return mux`:
```go
	mux.Handle("POST /api/onvif/discover", s.authed(s.onvifDiscover))
	mux.Handle("POST /api/onvif/streams", s.authed(s.onvifStreams))
```

Create `internal/web/onvif.go`:
```go
package web

import (
	"context"
	"errors"
	"net/http"

	"camorage/internal/onvif"
)

// ONVIF is the camera-discovery backend; onvif.LAN{} in production.
type ONVIF interface {
	Discover(ctx context.Context) ([]onvif.Device, error)
	Streams(ctx context.Context, xaddr, user, pass string) ([]onvif.Profile, error)
}

func (s *server) onvifDiscover(w http.ResponseWriter, r *http.Request) {
	devs, err := s.d.ONVIF.Discover(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "discovery: "+err.Error())
		return
	}
	if devs == nil {
		devs = []onvif.Device{}
	}
	writeJSON(w, http.StatusOK, devs)
}

// onvifStreams asks a camera for its streams and returns ready-to-save RTSP URLs with the
// credentials percent-encoded into them.
func (s *server) onvifStreams(w http.ResponseWriter, r *http.Request) {
	var in struct {
		XAddr string `json:"xaddr"`
		User  string `json:"user"`
		Pass  string `json:"pass"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ps, err := s.d.ONVIF.Streams(r.Context(), in.XAddr, in.User, in.Pass)
	if errors.Is(err, onvif.ErrAuth) {
		fail(w, http.StatusBadRequest, "the camera rejected this user name or password")
		return
	}
	if err != nil {
		fail(w, http.StatusBadGateway, "camera: "+err.Error())
		return
	}
	main, sub := onvif.MainAndSub(ps, in.User, in.Pass)
	writeJSON(w, http.StatusOK, map[string]any{"mainUrl": main, "subUrl": sub, "profiles": ps})
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l internal/ ; go test -race ./internal/web/`
Expected: no gofmt output; `ok  camorage/internal/web`.

- [ ] **Step 5: Commit**

```bash
git add internal/web/onvif.go internal/web/onvif_test.go internal/web/web.go
git commit -m "feat(web): ONVIF discover and stream lookup endpoints

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Live proxy (HLS and WebRTC/WHEP through the portal)

**Files:**
- Create: `internal/web/live.go`
- Test: `internal/web/live_test.go`
- Modify: `internal/web/web.go` (two Deps fields, four routes)

**Interfaces:**
- Consumes: `mediamtx.SubPath` (M1a); `newEnv(t, mods...)`, `gate` camera JSON (has a substream), `noOrigin` (web tests).
- Produces:
  - **Deps fields:** `Deps.HLSBase, Deps.WebRTCBase string` (e.g. `http://127.0.0.1:8888`, `http://127.0.0.1:8889`).
  - **Routes** (all need a session; non-GET also needs same-origin):
    - `GET /live/hls/{rest...}` → `HLSBase/<rest>`.
    - `POST /live/whep/{path}` → `WebRTCBase/<path>/whep`.
    - `PATCH|DELETE /live/whep/{path}/{session}` → `WebRTCBase/<path>/whep/<session>`.
  - **Allowed paths:** only enabled cameras (`<id>` or `<id>_sub` when it has a substream); anything else is 404.
  - **Helper:** `whepLocation(loc string) string`.

- [ ] **Step 1: Write the failing tests** in `internal/web/live_test.go`

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web/`
Expected: FAIL (`d.HLSBase undefined`, `whepLocation undefined`).

- [ ] **Step 3: Implement.** In `internal/web/web.go`:
  - Add a field to `Deps` after the `ONVIF` line:
```go
	HLSBase, WebRTCBase string // MediaMTX's loopback HLS and WebRTC servers
```
  - Insert these routes in `New`, before `return mux`:
```go
	mux.Handle("GET /live/hls/{rest...}", s.authed(s.hls))
	mux.Handle("POST /live/whep/{path}", s.authed(s.whepOffer))
	mux.Handle("PATCH /live/whep/{path}/{session}", s.authed(s.whepSession))
	mux.Handle("DELETE /live/whep/{path}/{session}", s.authed(s.whepSession))
```

Create `internal/web/live.go`:
```go
package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"camorage/internal/mediamtx"
)

// livePath reports whether p is the MediaMTX path of an enabled camera's main stream or
// substream; the live proxy refuses everything else.
func (s *server) livePath(p string) bool {
	cfg := s.d.Store.Get()
	for _, c := range cfg.Cameras {
		if c.Enabled && (p == c.ID || (c.SubURL != "" && p == mediamtx.SubPath(c.ID))) {
			return true
		}
	}
	return false
}

// hls proxies GET /live/hls/<path>/<file> to MediaMTX's HLS server.
func (s *server) hls(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	path, _, _ := strings.Cut(rest, "/")
	if !s.livePath(path) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.HLSBase, "/"+rest, func(loc string) string { return "/live/hls" + loc })
}

// whepOffer proxies POST /live/whep/<path> (an SDP offer) to MediaMTX's WHEP endpoint.
func (s *server) whepOffer(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	if !s.livePath(path) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.WebRTCBase, "/"+path+"/whep", whepLocation)
}

// whepSession proxies PATCH (trickle ICE) and DELETE (hang up) of a WHEP session.
func (s *server) whepSession(w http.ResponseWriter, r *http.Request) {
	path, session := r.PathValue("path"), r.PathValue("session")
	if !s.livePath(path) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.WebRTCBase, "/"+path+"/whep/"+session, whepLocation)
}

// whepLocation maps MediaMTX's session URL "/<path>/whep/<id>[?q]" to "/live/whep/<path>/<id>[?q]".
func whepLocation(loc string) string {
	path, rest, found := strings.Cut(strings.TrimPrefix(loc, "/"), "/whep/")
	if !found {
		return loc
	}
	return "/live/whep/" + path + "/" + rest
}

// proxy forwards r to base+path, keeping the query. The portal's cookies stay here (MediaMTX then
// falls back to ?session= URLs), MediaMTX's own cookies are dropped, and absolute Location headers
// are mapped back into the portal's URL space by fixLocation.
func (s *server) proxy(w http.ResponseWriter, r *http.Request, base, path string, fixLocation func(string) string) {
	target, err := url.Parse(base)
	if err != nil || target.Host == "" {
		fail(w, http.StatusInternalServerError, "live proxy is not configured")
		return
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = target.Scheme, target.Host
			pr.Out.URL.Path, pr.Out.URL.RawPath = path, ""
			pr.Out.Host = target.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Origin") // already checked by the portal; keeps MediaMTX's CORS out of it
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, "/") {
				resp.Header.Set("Location", fixLocation(loc))
			}
			return nil
		},
	}
	rp.ServeHTTP(w, r)
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l internal/ ; go test -race ./internal/web/`
Expected: no gofmt output; `ok  camorage/internal/web`.

- [ ] **Step 5: Commit**

```bash
git add internal/web/live.go internal/web/live_test.go internal/web/web.go
git commit -m "feat(web): live proxy for HLS and WHEP to loopback MediaMTX

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: UI logic module (`lib.js`) with Node tests

**Files:**
- Create: `internal/web/ui/lib.js`
- Test: `internal/web/ui/lib.test.mjs` (Node's built-in runner; `.mjs` is **not** embedded in the binary)

**Interfaces:**
- Produces (ES module exports):
  - **Constants:** `DAY_MS`, `DAY_NAMES` (`['Mon'…'Sun']`).
  - **Time in the phone's offset:** `offsetOf(rfc3339) → "+05:30"`, `dayStart(date "YYYY-MM-DD", offset) → ms`, `todayIn(offset, now?) → "YYYY-MM-DD"`, `isoAt(ms, offset) → RFC 3339 with offset`, `clock(ms, offset) → "HH:MM:SS"`.
  - **Timeline:** `toSpans(apiSpans [{start, durationSec}]) → [{from, to}]` sorted, `blocks(spans, start, len?) → [{left, width}]` (percent, clipped), `playFrom(spans, t) → ms | null`.
  - **Schedules and streams:** `scheduleSummary(windows) → string`, `cleanWindows(rows) → windows`, `streamPaths(cam) → {tile, full}`.

- [ ] **Step 1: Write the failing tests** in `internal/web/ui/lib.test.mjs`

```js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DAY_MS, offsetOf, dayStart, todayIn, isoAt, clock, toSpans, blocks, playFrom,
  scheduleSummary, cleanWindows, streamPaths,
} from './lib.js';

const IST = '+05:30';

test('offsetOf reads the offset at the end of an RFC 3339 time', () => {
  assert.equal(offsetOf('2026-09-30T14:03:00+05:30'), '+05:30');
  assert.equal(offsetOf('2026-09-30T08:33:00Z'), '+00:00');
  assert.equal(offsetOf(''), '+00:00');
});

test('days and clocks use the phone offset, not the browser timezone', () => {
  assert.equal(new Date(dayStart('2026-09-30', IST)).toISOString(), '2026-09-29T18:30:00.000Z');
  // 19:00 UTC on the 29th is already 00:30 on the 30th in IST
  assert.equal(todayIn(IST, Date.parse('2026-09-29T19:00:00Z')), '2026-09-30');
  assert.equal(isoAt(Date.parse('2026-09-30T08:32:50Z'), IST), '2026-09-30T14:02:50+05:30');
  assert.equal(clock(Date.parse('2026-09-30T08:32:50Z'), IST), '14:02:50');
});

test('blocks clips spans to the window and returns percentages', () => {
  const start = dayStart('2026-09-30', IST);
  const spans = toSpans([
    { start: '2026-10-01T01:00:00+05:30', durationSec: 60 }, // next day: dropped
    { start: '2026-09-30T12:00:00+05:30', durationSec: 3600 },
    { start: '2026-09-29T23:00:00+05:30', durationSec: 7200 }, // straddles midnight
  ]);
  assert.equal(spans[0].from, Date.parse('2026-09-29T23:00:00+05:30'), 'toSpans sorts by start');
  const b = blocks(spans, start);
  assert.equal(b.length, 2);
  assert.deepEqual(b[0], { left: 0, width: (3600000 / DAY_MS) * 100 });
  assert.deepEqual(b[1], { left: 50, width: (3600000 / DAY_MS) * 100 });
});

test('playFrom plays a recorded moment, snaps a gap forward, gives up after the last span', () => {
  const spans = [{ from: 100, to: 200 }, { from: 500, to: 600 }];
  assert.equal(playFrom(spans, 150), 150);
  assert.equal(playFrom(spans, 300), 500);
  assert.equal(playFrom(spans, 50), 100);
  assert.equal(playFrom(spans, 650), null);
  assert.equal(playFrom([], 10), null);
});

test('scheduleSummary describes windows in words', () => {
  assert.equal(scheduleSummary([]), 'Always');
  assert.equal(scheduleSummary(null), 'Always');
  assert.equal(scheduleSummary([{ days: [1, 2, 3, 4, 5], start: '20:00', end: '08:00' }]), 'Mon–Fri 20:00–08:00');
  assert.equal(scheduleSummary([{ days: [7, 6], start: '00:00', end: '00:00' }]), 'Sat, Sun 00:00–00:00');
  assert.equal(scheduleSummary([{ days: [1, 2, 3, 4, 5, 6, 7], start: '09:00', end: '17:00' }]), 'Every day 09:00–17:00');
});

test('cleanWindows drops rows without days and sorts days', () => {
  assert.deepEqual(
    cleanWindows([{ days: [3, 1, 1], start: '08:00', end: '09:00' }, { days: [], start: '10:00', end: '11:00' }]),
    [{ days: [1, 3], start: '08:00', end: '09:00' }],
  );
});

test('streamPaths uses the substream for tiles when there is one', () => {
  assert.deepEqual(streamPaths({ id: 'cam1', subUrl: 'rtsp://x/sub' }), { tile: 'cam1_sub', full: 'cam1' });
  assert.deepEqual(streamPaths({ id: 'cam2', subUrl: '' }), { tile: 'cam2', full: 'cam2' });
});
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd ~/src/camorage && node --test internal/web/ui/lib.test.mjs`
Expected: FAIL (`Cannot find module …/lib.js`).

- [ ] **Step 3: Implement** `internal/web/ui/lib.js`

```js
// Pure helpers for the camorage UI. No DOM access, so `node --test` can load this file.
// All times are shown in the phone's timezone, given as its UTC offset (e.g. "+05:30").

export const DAY_MS = 24 * 3600 * 1000;
export const DAY_NAMES = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

// offsetOf returns the "+05:30"-style offset at the end of an RFC 3339 time.
export function offsetOf(rfc3339) {
  const m = /(Z|[+-]\d\d:\d\d)$/.exec(rfc3339 || '');
  return !m || m[1] === 'Z' ? '+00:00' : m[1];
}

function offsetMs(offset) {
  const m = /^([+-])(\d\d):(\d\d)$/.exec(offset);
  if (!m) return 0;
  return (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3])) * 60000;
}

// dayStart is local midnight of date "YYYY-MM-DD" on the phone, as epoch ms.
export function dayStart(date, offset) {
  return Date.parse(`${date}T00:00:00${offset}`);
}

// todayIn is today's date on the phone.
export function todayIn(offset, now = Date.now()) {
  return new Date(now + offsetMs(offset)).toISOString().slice(0, 10);
}

// isoAt formats epoch ms as RFC 3339 in the phone's offset (what the playback API expects).
export function isoAt(ms, offset) {
  return new Date(ms + offsetMs(offset)).toISOString().slice(0, 19) + offset;
}

// clock formats epoch ms as "HH:MM:SS" on the phone's clock.
export function clock(ms, offset) {
  return new Date(ms + offsetMs(offset)).toISOString().slice(11, 19);
}

// toSpans turns API spans [{start, durationSec}] into [{from, to}] epoch ms, sorted by start.
export function toSpans(apiSpans) {
  return apiSpans
    .map((s) => {
      const from = Date.parse(s.start);
      return { from, to: from + s.durationSec * 1000 };
    })
    .sort((a, b) => a.from - b.from);
}

// blocks positions spans inside the window [start, start+len) as percentages of its width.
export function blocks(spans, start, len = DAY_MS) {
  const end = start + len;
  return spans
    .filter((s) => s.to > start && s.from < end)
    .map((s) => {
      const from = Math.max(s.from, start);
      const to = Math.min(s.to, end);
      return { left: ((from - start) / len) * 100, width: ((to - from) / len) * 100 };
    });
}

// playFrom is where playback starts for a click at t: t itself if recorded, else the start of the
// next recorded span, else null.
export function playFrom(spans, t) {
  for (const s of spans) {
    if (t >= s.from && t < s.to) return t;
    if (s.from > t) return s.from;
  }
  return null;
}

// scheduleSummary describes schedule windows, e.g. "Mon–Fri 20:00–08:00"; no windows = "Always".
export function scheduleSummary(windows) {
  if (!windows || windows.length === 0) return 'Always';
  return windows.map((w) => `${daysLabel(w.days)} ${w.start}–${w.end}`).join('; ');
}

function daysLabel(days) {
  const d = [...days].sort((a, b) => a - b);
  if (d.length === 7) return 'Every day';
  const contiguous = d.every((x, i) => i === 0 || x === d[i - 1] + 1);
  if (contiguous && d.length > 2) return `${DAY_NAMES[d[0] - 1]}–${DAY_NAMES[d[d.length - 1] - 1]}`;
  return d.map((x) => DAY_NAMES[x - 1]).join(', ');
}

// cleanWindows turns schedule-editor rows into API windows: days sorted and de-duplicated,
// rows without days or with malformed times dropped.
export function cleanWindows(rows) {
  return rows
    .map((r) => ({ days: [...new Set(r.days)].sort((a, b) => a - b), start: r.start, end: r.end }))
    .filter((w) => w.days.length > 0 && /^\d\d:\d\d$/.test(w.start) && /^\d\d:\d\d$/.test(w.end));
}

// streamPaths gives the MediaMTX paths for a camera's live tile (substream if any) and full view.
export function streamPaths(cam) {
  return { tile: cam.subUrl ? `${cam.id}_sub` : cam.id, full: cam.id };
}
```

- [ ] **Step 4: Run the tests**

Run: `node --test internal/web/ui/lib.test.mjs`
Expected: `# pass 7`, `# fail 0`.

- [ ] **Step 5: Commit**

```bash
git add internal/web/ui/lib.js internal/web/ui/lib.test.mjs
git commit -m "feat(ui): tested helpers for phone-time days, timeline math, schedules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Live view (DOM helpers, WHEP client, grid + full screen)

**Files:**
- Create: `internal/web/ui/dom.js`, `internal/web/ui/whep.js`, `internal/web/ui/live.js`

**Interfaces:**
- Consumes: `streamPaths` (Task 6); the HTTP routes `/api/cameras`, `/api/status`, `/live/hls/…`, `/live/whep/…` (M1a, Task 5).
- Produces:
  - **`dom.js`:**
    - `h(tag, attrs, ...kids) → Element`. `attrs.style` may be an object applied via CSSOM; `on*` functions become listeners; text children are never parsed as HTML.
    - `api(path, {method, body}) → Promise<json>`. On a 401 it dispatches `window` event `camorage:signed-out` and throws `Error('signed out')`.
    - `playHLS(video, url) → {close()}`, which retries 5 s after a fatal error.
    - `showError(el, err)`.
  - **`whep.js`:** `playWHEP(video, url, timeoutMs=4000) → Promise<{close()}>`. It rejects (and cleans up) if no frame arrives in time.
  - **`live.js`:** `renderLive(container) → Promise<cleanup()>`.
- **Verification:** these modules are DOM glue. This task checks syntax; behavior is verified in the browser in Task 13, steps 3–5 and 7.

- [ ] **Step 1: Write** `internal/web/ui/dom.js`

```js
// DOM and network helpers shared by the views.

// h builds an element: h('button', {class: 'primary', onclick: fn}, 'Save').
// attrs.style may be an object (applied through CSSOM, which the CSP allows).
// Children are nodes or text; text is never parsed as HTML (camera names come from users).
export function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'style' && v && typeof v === 'object') Object.assign(el.style, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else if (v !== false && v != null) el.setAttribute(k, String(v));
  }
  for (const k of kids.flat()) {
    if (k != null && k !== false) el.append(k instanceof Node ? k : String(k));
  }
  return el;
}

// api calls the portal's JSON API. A 401 means the session ended: the app shows the login screen.
export async function api(path, { method = 'GET', body } = {}) {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    window.dispatchEvent(new Event('camorage:signed-out'));
    throw new Error('signed out');
  }
  const data = (res.headers.get('Content-Type') || '').includes('json') ? await res.json() : null;
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

// playHLS plays an HLS URL with hls.js (or natively on Safari). After a fatal error it retries
// every 5 s, so a tile recovers by itself when its camera or MediaMTX comes back.
export function playHLS(video, url) {
  let hls = null;
  let timer = null;
  let closed = false;
  const retry = () => { timer = setTimeout(start, 5000); };
  function start() {
    if (closed) return;
    if (window.Hls && window.Hls.isSupported()) {
      hls = new window.Hls({ liveSyncDurationCount: 2 });
      hls.on(window.Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal) return;
        hls.destroy();
        hls = null;
        retry();
      });
      hls.loadSource(url);
      hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.onerror = retry;
      video.src = url;
    }
    video.play().catch(() => {});
  }
  start();
  return {
    close() {
      closed = true;
      clearTimeout(timer);
      if (hls) hls.destroy();
      video.onerror = null;
      video.removeAttribute('src');
      video.load();
    },
  };
}

// showError puts an error line at the top of el for a few seconds.
export function showError(el, err) {
  const line = h('p', { class: 'error', role: 'alert' }, (err && err.message) || String(err));
  el.prepend(line);
  setTimeout(() => line.remove(), 8000);
}
```

- [ ] **Step 2: Write** `internal/web/ui/whep.js`

```js
// Minimal WHEP (WebRTC-HTTP egress) client for MediaMTX without trickle ICE: gather local
// candidates, POST the offer through the portal, apply the answer, wait for the first frame.
export async function playWHEP(video, url, timeoutMs = 4000) {
  if (typeof RTCPeerConnection === 'undefined') throw new Error('WebRTC is not available');
  const pc = new RTCPeerConnection();
  const stream = new MediaStream();
  let session = null;
  const close = () => {
    pc.close();
    if (session) fetch(session, { method: 'DELETE', credentials: 'same-origin' }).catch(() => {});
  };
  try {
    pc.addTransceiver('video', { direction: 'recvonly' });
    pc.addTransceiver('audio', { direction: 'recvonly' });
    pc.ontrack = (e) => {
      stream.addTrack(e.track);
      video.srcObject = stream;
    };
    await pc.setLocalDescription(await pc.createOffer());
    await gathered(pc, 2000);
    const res = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/sdp' },
      body: pc.localDescription.sdp,
    });
    if (res.status !== 201) throw new Error(`WHEP HTTP ${res.status}`);
    session = res.headers.get('Location');
    await pc.setRemoteDescription({ type: 'answer', sdp: await res.text() });
    await playing(video, timeoutMs);
    return { close };
  } catch (err) {
    close();
    video.srcObject = null;
    throw err;
  }
}

// gathered waits for ICE gathering to finish, at most maxMs (host candidates come much sooner).
function gathered(pc, maxMs) {
  return new Promise((resolve) => {
    if (pc.iceGatheringState === 'complete') {
      resolve();
      return;
    }
    const timer = setTimeout(resolve, maxMs);
    pc.addEventListener('icegatheringstatechange', () => {
      if (pc.iceGatheringState === 'complete') {
        clearTimeout(timer);
        resolve();
      }
    });
  });
}

function playing(video, ms) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('no video over WebRTC')), ms);
    video.addEventListener('playing', () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
    video.play().catch(() => {});
  });
}
```

- [ ] **Step 3: Write** `internal/web/ui/live.js`

```js
import { h, api, playHLS } from './dom.js';
import { streamPaths } from './lib.js';
import { playWHEP } from './whep.js';

// renderLive shows every enabled camera as a tile (substream over HLS) with a status badge.
// Clicking a tile opens the main stream full screen: WebRTC when the phone is directly reachable
// (home Wi-Fi, Tailscale), otherwise HLS.
export async function renderLive(root) {
  const cams = (await api('/api/cameras')).filter((c) => c.enabled);
  if (cams.length === 0) {
    root.append(h('p', { class: 'empty' }, 'No cameras yet. ', h('a', { href: '#/cameras' }, 'Add one')));
    return () => {};
  }

  const players = [];
  const badges = new Map();
  const grid = h('div', { class: 'grid' });
  for (const cam of cams) {
    const video = h('video', { muted: true, autoplay: true, playsinline: true });
    video.muted = true; // the attribute alone does not satisfy autoplay policies
    const badge = h('span', { class: 'badge' }, '…');
    badges.set(cam.id, badge);
    grid.append(h('figure', {
      class: 'tile',
      tabindex: 0,
      onclick: () => openFull(cam),
      onkeydown: (e) => { if (e.key === 'Enter') openFull(cam); },
    }, video, h('figcaption', {}, h('span', {}, cam.name), badge)));
    players.push(playHLS(video, `/live/hls/${streamPaths(cam).tile}/index.m3u8`));
  }
  root.append(grid);

  async function refresh() {
    try {
      const st = await api('/api/status');
      for (const c of st.cameras) {
        const badge = badges.get(c.id);
        if (!badge) continue;
        const [text, cls] = !c.available ? ['offline', 'bad'] : c.recording ? ['● REC', 'rec'] : ['live', 'ok'];
        badge.textContent = text;
        badge.className = `badge ${cls}`;
      }
    } catch {
      // a sign-out is handled by the app; a failed poll just waits for the next one
    }
  }
  refresh();
  const timer = setInterval(refresh, 3000);

  let full = null;
  async function openFull(cam) {
    closeFull();
    const video = h('video', { muted: true, autoplay: true, playsinline: true, controls: true });
    video.muted = true;
    const mode = h('span', { class: 'badge' }, 'connecting…');
    const overlay = h('div', { class: 'overlay', onclick: (e) => { if (e.target === overlay) closeFull(); } },
      h('div', { class: 'full' },
        h('div', { class: 'full-bar' }, h('strong', {}, cam.name), mode, h('button', { onclick: closeFull }, 'Close')),
        video));
    document.body.append(overlay);
    const me = { overlay, player: null };
    full = me;
    const path = streamPaths(cam).full;
    let player;
    try {
      player = await playWHEP(video, `/live/whep/${path}`);
      mode.textContent = 'WebRTC';
    } catch {
      player = playHLS(video, `/live/hls/${path}/index.m3u8`);
      mode.textContent = 'HLS';
    }
    if (full !== me) {
      player.close(); // closed while connecting
      return;
    }
    me.player = player;
  }

  function closeFull() {
    if (!full) return;
    if (full.player) full.player.close();
    full.overlay.remove();
    full = null;
  }

  const onKey = (e) => { if (e.key === 'Escape') closeFull(); };
  document.addEventListener('keydown', onKey);

  return () => {
    clearInterval(timer);
    document.removeEventListener('keydown', onKey);
    closeFull();
    players.forEach((p) => p.close());
  };
}
```

- [ ] **Step 4: Syntax check and keep the other suites green**

Run: `cd ~/src/camorage && for f in internal/web/ui/dom.js internal/web/ui/whep.js internal/web/ui/live.js; do node --check "$f" && echo "$f ok"; done && node --test internal/web/ui/lib.test.mjs 2>&1 | grep -E '^# (pass|fail)'`
Expected: three `ok` lines, `# pass 7`, `# fail 0`.

- [ ] **Step 5: Commit**

```bash
git add internal/web/ui/dom.js internal/web/ui/whep.js internal/web/ui/live.js
git commit -m "feat(ui): live grid over HLS and full-screen WebRTC with HLS fallback

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Playback view (24 h timeline, 1 h zoom, chunked playback, download)

**Files:**
- Create: `internal/web/ui/playback.js`

**Interfaces:**
- Consumes: `h`, `api`, `showError` (Task 7); `DAY_MS`, `blocks`, `clock`, `dayStart`, `isoAt`, `offsetOf`, `playFrom`, `toSpans`, `todayIn` (Task 6); `GET /api/playback/spans`, `GET /api/playback/video` (M1a).
- Produces: `renderPlayback(container) → Promise<cleanup()>`.
- **Verification:** in the browser in Task 13, step 4.

- [ ] **Step 1: Write** `internal/web/ui/playback.js`

```js
import { h, api, showError } from './dom.js';
import { DAY_MS, blocks, clock, dayStart, isoAt, offsetOf, playFrom, toSpans, todayIn } from './lib.js';

const CHUNK_S = 600; // each <video> source is 10 minutes; the next one loads when it ends
const HOUR_MS = 3600 * 1000;

// renderPlayback shows a 24 h timeline (zoomable to 1 h) of what a camera recorded on a day, in
// the phone's local time. Clicking plays from that moment and continues chunk by chunk.
export async function renderPlayback(root) {
  const [cams, st] = await Promise.all([api('/api/cameras'), api('/api/status')]);
  if (cams.length === 0) {
    root.append(h('p', { class: 'empty' }, 'No cameras yet.'));
    return () => {};
  }
  const off = offsetOf(st.time);
  const state = { cam: cams[0].id, date: todayIn(off), spans: [], winStart: 0, winLen: DAY_MS, playhead: null };
  let chunkStart = null;

  const camSel = h('select', { onchange: () => { state.cam = camSel.value; load(); } },
    cams.map((c) => h('option', { value: c.id }, c.name)));
  const dateIn = h('input', { type: 'date', value: state.date, onchange: () => { state.date = dateIn.value; load(); } });
  const prevBtn = h('button', { onclick: () => shift(-1), hidden: true, title: 'Previous hour' }, '◀');
  const zoomBtn = h('button', { onclick: () => zoom() }, 'Zoom to 1 h');
  const nextBtn = h('button', { onclick: () => shift(1), hidden: true, title: 'Next hour' }, '▶');
  const bar = h('div', { class: 'timeline', onclick: (e) => click(e) });
  const ticks = h('div', { class: 'ticks' });
  const label = h('p', { class: 'muted' }, 'Click the timeline to play.');
  const video = h('video', { class: 'player', controls: true, playsinline: true });
  const download = h('a', { class: 'button', hidden: true, download: '' }, 'Download these 10 minutes');
  root.append(h('div', { class: 'toolbar' }, camSel, dateIn, prevBtn, zoomBtn, nextBtn), bar, ticks, label, video, download);

  async function load() {
    state.winStart = dayStart(state.date, off);
    state.winLen = DAY_MS;
    try {
      state.spans = toSpans(await api(`/api/playback/spans?cam=${encodeURIComponent(state.cam)}&date=${state.date}`));
    } catch (err) {
      state.spans = [];
      showError(root, err);
    }
    label.textContent = state.spans.length ? 'Click the timeline to play.' : 'No recordings on this day.';
    draw();
  }

  function draw() {
    const parts = blocks(state.spans, state.winStart, state.winLen).map((b) =>
      h('div', { class: 'span', style: { left: `${b.left}%`, width: `${Math.max(b.width, 0.2)}%` } }));
    const p = state.playhead;
    if (p != null && p >= state.winStart && p < state.winStart + state.winLen) {
      parts.push(h('div', { class: 'playhead', style: { left: `${((p - state.winStart) / state.winLen) * 100}%` } }));
    }
    bar.replaceChildren(...parts);
    const step = state.winLen === DAY_MS ? 3 * HOUR_MS : 10 * 60000;
    const labels = [];
    for (let t = state.winStart; t <= state.winStart + state.winLen; t += step) labels.push(h('span', {}, clock(t, off).slice(0, 5)));
    ticks.replaceChildren(...labels);
    const zoomed = state.winLen !== DAY_MS;
    zoomBtn.textContent = zoomed ? 'Show 24 h' : 'Zoom to 1 h';
    prevBtn.hidden = !zoomed;
    nextBtn.hidden = !zoomed;
  }

  function clampHour(start) {
    const day = dayStart(state.date, off);
    return Math.min(Math.max(start, day), day + DAY_MS - HOUR_MS);
  }

  function zoom() {
    if (state.winLen !== DAY_MS) {
      state.winStart = dayStart(state.date, off);
      state.winLen = DAY_MS;
    } else {
      const last = state.spans[state.spans.length - 1];
      const center = state.playhead ?? (last ? last.to : state.winStart + DAY_MS / 2);
      state.winLen = HOUR_MS;
      state.winStart = clampHour(center - HOUR_MS / 2);
    }
    draw();
  }

  function shift(dir) {
    state.winStart = clampHour(state.winStart + dir * HOUR_MS);
    draw();
  }

  function click(e) {
    const rect = bar.getBoundingClientRect();
    const t = state.winStart + ((e.clientX - rect.left) / rect.width) * state.winLen;
    const from = playFrom(state.spans, t);
    if (from == null) {
      label.textContent = 'Nothing was recorded after this point.';
      return;
    }
    play(from);
  }

  function play(from) {
    chunkStart = from;
    const q = `cam=${encodeURIComponent(state.cam)}&start=${encodeURIComponent(isoAt(from, off))}&duration=${CHUNK_S}`;
    video.src = `/api/playback/video?${q}&format=fmp4`;
    video.play().catch(() => {});
    download.href = `/api/playback/video?${q}&format=mp4`;
    download.hidden = false;
    label.textContent = `Playing from ${clock(from, off)}`;
  }

  video.addEventListener('timeupdate', () => {
    if (chunkStart == null) return;
    state.playhead = chunkStart + video.currentTime * 1000;
    label.textContent = `Playing ${clock(state.playhead, off)}`;
    draw();
  });
  video.addEventListener('ended', () => {
    if (chunkStart == null) return;
    const next = playFrom(state.spans, chunkStart + video.currentTime * 1000);
    if (next != null && next < dayStart(state.date, off) + DAY_MS) play(next);
    else label.textContent = 'End of the recordings for this day.';
  });

  await load();
  return () => {
    video.removeAttribute('src');
    video.load();
  };
}
```

- [ ] **Step 2: Syntax check**

Run: `node --check internal/web/ui/playback.js && echo ok`
Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add internal/web/ui/playback.js
git commit -m "feat(ui): playback timeline with 1 h zoom, chunked fMP4 playback, download

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Cameras view (list, add/edit with schedule editor, delete, ONVIF scan)

**Files:**
- Create: `internal/web/ui/cameras.js`

**Interfaces:**
- Consumes: `h`, `api`, `showError` (Task 7); `DAY_NAMES`, `cleanWindows`, `scheduleSummary` (Task 6); `GET/POST /api/cameras`, `PUT/DELETE /api/cameras/{id}` (M1a); `POST /api/onvif/discover`, `POST /api/onvif/streams` (Task 4).
- Produces: `renderCameras(container) → Promise<cleanup()>`.
- **Editing and secrets:** an edit sends the camera object exactly as the API returned it, with only the edited fields changed. The masked `********` passwords therefore round-trip and the server keeps the stored ones (M1a behavior).
- **Verification:** in the browser in Task 13, step 6.

- [ ] **Step 1: Write** `internal/web/ui/cameras.js`

```js
import { h, api, showError } from './dom.js';
import { DAY_NAMES, cleanWindows, scheduleSummary } from './lib.js';

// renderCameras lists cameras with edit/delete, and adds cameras by RTSP URL or by ONVIF scan.
export async function renderCameras(root) {
  const panel = h('div');
  const list = h('div');
  root.append(h('div', { class: 'toolbar' },
    h('button', { class: 'primary', onclick: () => edit(null) }, 'Add camera'),
    h('button', { onclick: () => scan() }, 'Scan network')), panel, list);

  async function refresh() {
    const cams = await api('/api/cameras');
    list.replaceChildren(...(cams.length ? cams.map(row) : [h('p', { class: 'empty' }, 'No cameras yet.')]));
  }

  function row(cam) {
    return h('div', { class: 'card' },
      h('div', {}, h('strong', {}, cam.name), ' ', h('span', { class: 'muted' }, cam.id), ' ',
        cam.enabled ? '' : h('span', { class: 'badge bad' }, 'disabled')),
      h('div', { class: 'muted' },
        `Records: ${scheduleSummary(cam.schedule)} · keeps ${cam.localDays} day${cam.localDays === 1 ? '' : 's'} on the phone`),
      h('div', { class: 'actions' },
        h('button', { onclick: () => edit(cam) }, 'Edit'),
        h('button', { class: 'danger', onclick: () => remove(cam) }, 'Delete')));
  }

  async function remove(cam) {
    if (!confirm(`Delete camera "${cam.name}"? Its recordings stay until they age out.`)) return;
    try {
      await api(`/api/cameras/${encodeURIComponent(cam.id)}`, { method: 'DELETE' });
      await refresh();
    } catch (err) {
      showError(root, err);
    }
  }

  // edit shows the camera form: cam is null for a new camera; prefill seeds a new camera's fields.
  function edit(cam, prefill = {}) {
    const base = cam || { name: '', enabled: true, mainUrl: '', subUrl: '', localDays: 1, schedule: [], ...prefill };
    const name = h('input', { value: base.name, required: true, maxlength: 60 });
    const enabled = h('input', { type: 'checkbox' });
    enabled.checked = base.enabled;
    const mainUrl = h('input', { value: base.mainUrl, required: true, placeholder: 'rtsp://user:pass@192.168.1.10/stream' });
    const subUrl = h('input', { value: base.subUrl, placeholder: 'optional low-resolution stream' });
    const localDays = h('input', { type: 'number', min: 1, max: 365, value: base.localDays });
    const rows = h('div');
    const addRow = (w = { days: [1, 2, 3, 4, 5, 6, 7], start: '00:00', end: '00:00' }) => rows.append(windowRow(w));
    (base.schedule || []).forEach((w) => addRow(w));

    const form = h('form', { class: 'card form', onsubmit: (e) => { e.preventDefault(); save(); } },
      h('h3', {}, cam ? `Edit ${cam.name}` : 'Add camera'),
      h('label', {}, 'Name', name),
      h('label', { class: 'inline' }, enabled, 'Enabled'),
      h('label', {}, 'Main stream (RTSP URL)', mainUrl),
      h('label', {}, 'Substream (RTSP URL)', subUrl),
      h('label', {}, 'Days to keep on the phone', localDays),
      h('fieldset', {}, h('legend', {}, 'Recording schedule'),
        h('p', { class: 'muted' }, 'No windows means record all the time. A window ending at or before its start runs past midnight.'),
        rows,
        h('button', { type: 'button', onclick: () => addRow() }, 'Add window')),
      h('div', { class: 'actions' },
        h('button', { class: 'primary', type: 'submit' }, 'Save'),
        h('button', { type: 'button', onclick: () => panel.replaceChildren() }, 'Cancel')));
    panel.replaceChildren(form);
    name.focus();

    async function save() {
      const body = {
        ...(cam || {}),
        name: name.value.trim(),
        enabled: enabled.checked,
        mainUrl: mainUrl.value.trim(),
        subUrl: subUrl.value.trim(),
        localDays: Number(localDays.value) || 1,
        schedule: cleanWindows([...rows.children].map((r) => r.readWindow())),
      };
      try {
        if (cam) await api(`/api/cameras/${encodeURIComponent(cam.id)}`, { method: 'PUT', body });
        else await api('/api/cameras', { method: 'POST', body });
        panel.replaceChildren();
        await refresh();
      } catch (err) {
        showError(panel, err);
      }
    }
  }

  function windowRow(w) {
    const boxes = DAY_NAMES.map((d, i) => {
      const box = h('input', { type: 'checkbox' });
      box.checked = w.days.includes(i + 1);
      return { box, label: h('label', { class: 'day' }, box, d) };
    });
    const start = h('input', { type: 'time', value: w.start, required: true });
    const end = h('input', { type: 'time', value: w.end, required: true });
    const row = h('div', { class: 'window' }, boxes.map((b) => b.label), start, '–', end,
      h('button', { type: 'button', class: 'danger', title: 'Remove window', onclick: () => row.remove() }, '✕'));
    row.readWindow = () => ({
      days: boxes.flatMap((b, i) => (b.box.checked ? [i + 1] : [])),
      start: start.value,
      end: end.value,
    });
    return row;
  }

  async function scan() {
    const box = h('div', { class: 'card' }, h('p', {}, 'Looking for ONVIF cameras on this network…'));
    panel.replaceChildren(box);
    let devs;
    try {
      devs = await api('/api/onvif/discover', { method: 'POST' });
    } catch (err) {
      box.replaceChildren(h('p', { class: 'error' }, err.message));
      return;
    }
    if (devs.length === 0) {
      box.replaceChildren(h('p', {}, 'No ONVIF camera answered. You can still add one by its RTSP URL.'));
      return;
    }
    box.replaceChildren(h('h3', {}, 'Cameras found'), ...devs.map((d) => h('div', { class: 'found' },
      h('span', {}, `${d.name || 'Camera'} · ${d.ip}${d.hardware ? ` · ${d.hardware}` : ''}`),
      h('button', { onclick: () => connect(d) }, 'Use this camera'))));
  }

  function connect(dev) {
    const user = h('input', { placeholder: 'admin', autocomplete: 'off' });
    const pass = h('input', { type: 'password', autocomplete: 'off' });
    const msg = h('p', { class: 'muted' }, 'Leave both empty if the camera has no password.');
    panel.replaceChildren(h('form', {
      class: 'card form',
      onsubmit: async (e) => {
        e.preventDefault();
        msg.className = 'muted';
        msg.textContent = 'Asking the camera for its streams…';
        try {
          const r = await api('/api/onvif/streams', { method: 'POST', body: { xaddr: dev.xaddr, user: user.value, pass: pass.value } });
          edit(null, { name: dev.name || `Camera ${dev.ip}`, mainUrl: r.mainUrl, subUrl: r.subUrl });
        } catch (err) {
          msg.className = 'error';
          msg.textContent = err.message;
        }
      },
    }, h('h3', {}, `Connect to ${dev.ip}`), h('label', {}, 'User name', user), h('label', {}, 'Password', pass), msg,
    h('div', { class: 'actions' },
      h('button', { class: 'primary', type: 'submit' }, 'Get streams'),
      h('button', { type: 'button', onclick: () => panel.replaceChildren() }, 'Cancel'))));
    user.focus();
  }

  await refresh();
  return () => {};
}
```

- [ ] **Step 2: Syntax check**

Run: `node --check internal/web/ui/cameras.js && echo ok`
Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add internal/web/ui/cameras.js
git commit -m "feat(ui): cameras page with schedule editor and ONVIF scan

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Settings view (health, background programs, password, sign out)

**Files:**
- Create: `internal/web/ui/settings.js`

**Interfaces:**
- Consumes: `h`, `api`, `showError` (Task 7); `GET /api/status` (M1a), `POST /api/password` (Task 3), `POST /api/logout` (M1a).
- Produces: `renderSettings(container) → Promise<cleanup()>`. It polls status every 5 s.
- **Verification:** in the browser in Task 13, step 6.

- [ ] **Step 1: Write** `internal/web/ui/settings.js`

```js
import { h, api, showError } from './dom.js';

// renderSettings shows phone health and the background programs, and lets you change the admin
// password or sign out.
export async function renderSettings(root) {
  const healthBox = h('div', { class: 'card' });
  const procBox = h('div', { class: 'card' });
  const cur = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const next = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const msg = h('p', { class: 'muted' });
  const pwForm = h('form', {
    class: 'card form',
    onsubmit: async (e) => {
      e.preventDefault();
      if (next.value !== again.value) {
        msg.className = 'error';
        msg.textContent = 'The new passwords do not match.';
        return;
      }
      try {
        await api('/api/password', { method: 'POST', body: { current: cur.value, next: next.value } });
        pwForm.reset();
        msg.className = 'ok';
        msg.textContent = 'Password changed. Other browsers have been signed out.';
      } catch (err) {
        msg.className = 'error';
        msg.textContent = err.message;
      }
    },
  }, h('h3', {}, 'Change password'),
  h('label', {}, 'Current password', cur), h('label', {}, 'New password (8+ characters)', next), h('label', {}, 'New password again', again),
  msg, h('button', { class: 'primary', type: 'submit' }, 'Change password'));
  const signOut = h('button', {
    class: 'danger',
    onclick: async () => {
      await api('/api/logout', { method: 'POST' }).catch(() => {});
      location.hash = '';
      location.reload();
    },
  }, 'Sign out');
  root.append(h('h3', {}, 'Phone'), healthBox, h('h3', {}, 'Background programs'), procBox, pwForm, signOut);

  async function refresh() {
    try {
      const st = await api('/api/status');
      const hl = st.health;
      healthBox.replaceChildren(dl([
        ['Phone time', st.time.replace('T', ' ')],
        ['Battery', hl.batteryPct < 0 ? 'unknown' : `${hl.batteryPct}% (${hl.charging || '?'}), ${hl.batteryTempC.toFixed(1)} °C`],
        ['Free memory', hl.memFreeMB < 0 ? 'unknown' : `${hl.memFreeMB} MB`],
        ['Recordings storage', hl.diskErr ? `error: ${hl.diskErr}`
          : `${(hl.diskFreeMB / 1024).toFixed(1)} GB free of ${(hl.diskTotalMB / 1024).toFixed(1)} GB`],
      ]));
      const procs = (st.processes || []).map((p) => dl([
        ['Program', p.name],
        ['State', p.running ? `running (pid ${p.pid})` : 'stopped'],
        ['Restarts', String(p.restarts)],
        ['Last exit', p.lastExit || '—'],
      ]));
      procBox.replaceChildren(...(procs.length ? procs : [h('p', { class: 'muted' }, 'Nothing running yet.')]));
      if (st.mediamtxError) procBox.append(h('p', { class: 'error' }, `MediaMTX: ${st.mediamtxError}`));
    } catch (err) {
      if (err.message !== 'signed out') showError(root, err);
    }
  }
  refresh();
  const timer = setInterval(refresh, 5000);
  return () => clearInterval(timer);
}

function dl(pairs) {
  return h('dl', {}, pairs.flatMap(([k, v]) => [h('dt', {}, k), h('dd', {}, v)]));
}
```

- [ ] **Step 2: Syntax check**

Run: `node --check internal/web/ui/settings.js && echo ok`
Expected: `ok`.

- [ ] **Step 3: Commit**

```bash
git add internal/web/ui/settings.js
git commit -m "feat(ui): settings page with phone health, programs, password change

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: App shell (setup/login, router, styles, vendored hls.js, embedded + CSP)

**Files:**
- Create: `internal/web/ui/auth.js`, `internal/web/ui/app.js`, `internal/web/ui/index.html`, `internal/web/ui/app.css`, `internal/web/ui/vendor/hls.min.js` (downloaded), `internal/web/ui.go`
- Test: `internal/web/ui_test.go`
- Modify: `internal/web/web.go` (`New`: serve the UI, wrap everything in security headers)

**Interfaces:**
- Consumes: the four views (Tasks 7–10), `h` (Task 7), `GET /api/health`, `GET /api/status`, `GET /api/setup/volumes`, `POST /api/setup`, `POST /api/login` (M1a, Task 3).
- Produces:
  - **The app:** `GET /` serves the single-page app, plus its modules, CSS and `vendor/hls.min.js`. `*.mjs` test files are not embedded.
  - **Headers on every response (UI and API):** the CSP from Global Constraints, plus `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.
  - **`auth.js`:** `renderSetup(container, done)`, `renderLogin(container, done)`.
  - **`app.js`:** hash router (`#/live`, `#/playback`, `#/cameras`, `#/settings`), boot (setup → login → app), and re-boot on `camorage:signed-out`.

- [ ] **Step 1: Write the failing test** in `internal/web/ui_test.go`

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run TestUIServedWithSecurityHeaders`
Expected: FAIL (`index: 404`).

- [ ] **Step 3: Vendor hls.js 1.7.3**

Run:
```sh
cd ~/src/camorage && mkdir -p internal/web/ui/vendor
curl -fsSL -o internal/web/ui/vendor/hls.min.js https://cdn.jsdelivr.net/npm/hls.js@1.7.3/dist/hls.min.js
sha256sum internal/web/ui/vendor/hls.min.js; wc -c internal/web/ui/vendor/hls.min.js; grep -o '1\.7\.3' internal/web/ui/vendor/hls.min.js | head -1
```
Expected: a SHA-256 line (put it in this task's commit message), a size of several hundred KB, and `1.7.3`.

- [ ] **Step 4: Write** `internal/web/ui/index.html`

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>camorage</title>
<link rel="icon" href="data:,">
<link rel="stylesheet" href="/app.css">
<script src="/vendor/hls.min.js" defer></script>
<script type="module" src="/app.js"></script>
</head>
<body>
<header id="bar" hidden>
  <span class="brand">camorage</span>
  <nav>
    <a href="#/live">Live</a>
    <a href="#/playback">Playback</a>
    <a href="#/cameras">Cameras</a>
    <a href="#/settings">Settings</a>
  </nav>
</header>
<main id="view"><p class="muted">Loading…</p></main>
</body>
</html>
```

- [ ] **Step 5: Write** `internal/web/ui/app.css`

```css
:root { --bg: #f6f7f9; --fg: #1b1f24; --muted: #667085; --card: #fff; --line: #e3e6ea; --accent: #2563eb; --bad: #dc2626; --ok: #16a34a; --rec: #e11d48; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #0f1115; --fg: #e7e9ee; --muted: #9aa3b2; --card: #181b21; --line: #2a2f38; --accent: #60a5fa; }
}
* { box-sizing: border-box; }
[hidden] { display: none !important; }
body { margin: 0; font: 15px/1.45 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--fg); }
header { position: sticky; top: 0; z-index: 5; display: flex; flex-wrap: wrap; gap: .5rem 1rem; align-items: center; padding: .6rem 1rem; background: var(--card); border-bottom: 1px solid var(--line); }
.brand { font-weight: 700; letter-spacing: .02em; }
nav { display: flex; flex-wrap: wrap; gap: .25rem; }
nav a { color: var(--muted); text-decoration: none; padding: .35rem .7rem; border-radius: .5rem; }
nav a.active { color: var(--fg); background: var(--bg); }
main { padding: 1rem; max-width: 1200px; margin: 0 auto; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(280px, 100%), 1fr)); gap: .75rem; }
.tile { margin: 0; position: relative; background: #000; border-radius: .6rem; overflow: hidden; cursor: pointer; }
.tile video { display: block; width: 100%; aspect-ratio: 16 / 9; background: #000; }
.tile figcaption { position: absolute; left: 0; right: 0; bottom: 0; display: flex; justify-content: space-between; align-items: center; gap: .5rem; padding: .4rem .6rem; color: #fff; background: linear-gradient(transparent, rgba(0, 0, 0, .7)); }
.badge { font-size: .75rem; padding: .1rem .45rem; border-radius: 999px; background: rgba(127, 127, 127, .25); white-space: nowrap; }
.badge.rec { background: var(--rec); color: #fff; }
.badge.bad { background: var(--bad); color: #fff; }
.badge.ok { background: var(--ok); color: #fff; }
.overlay { position: fixed; inset: 0; z-index: 10; display: flex; align-items: center; justify-content: center; padding: 1rem; background: rgba(0, 0, 0, .85); }
.full { width: min(100%, 1280px); }
.full video { display: block; width: 100%; max-height: 80vh; background: #000; border-radius: .5rem; }
.full-bar { display: flex; align-items: center; gap: .75rem; margin-bottom: .5rem; color: #fff; }
.full-bar button { margin-left: auto; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: .7rem; padding: 1rem; margin-bottom: .75rem; overflow-wrap: anywhere; }
.form label { display: block; margin: .6rem 0; }
.form label.inline { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem; }
.form input:not([type=checkbox]):not([type=radio]):not([type=time]) { display: block; width: 100%; margin-top: .25rem; }
.narrow { max-width: 440px; margin: 3rem auto; }
input, select, button, .button { font: inherit; padding: .45rem .7rem; border-radius: .45rem; border: 1px solid var(--line); background: var(--card); color: var(--fg); max-width: 100%; }
button, .button { cursor: pointer; display: inline-block; text-decoration: none; }
button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
button.danger { color: var(--bad); }
.toolbar, .actions { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; margin-bottom: .75rem; }
.muted { color: var(--muted); }
.error { color: var(--bad); }
.ok { color: var(--ok); }
.empty { color: var(--muted); padding: 2rem 0; text-align: center; }
.timeline { position: relative; height: 44px; background: var(--card); border: 1px solid var(--line); border-radius: .5rem; overflow: hidden; cursor: crosshair; }
.timeline .span { position: absolute; top: 6px; bottom: 6px; background: var(--accent); opacity: .75; border-radius: 3px; }
.timeline .playhead { position: absolute; top: 0; bottom: 0; width: 2px; background: var(--rec); }
.ticks { display: flex; justify-content: space-between; margin: .2rem 0 .6rem; font-size: .75rem; color: var(--muted); }
.player { display: block; width: 100%; max-height: 70vh; margin-bottom: .5rem; background: #000; border-radius: .5rem; }
fieldset { border: 1px solid var(--line); border-radius: .5rem; margin: .6rem 0; min-width: 0; }
.window { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem; margin: .3rem 0; }
.form label.day { display: inline-flex; align-items: center; gap: .15rem; margin: 0; font-size: .85rem; }
.found { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: .5rem; padding: .4rem 0; border-top: 1px solid var(--line); }
dl { display: grid; grid-template-columns: max-content 1fr; gap: .3rem 1rem; margin: 0 0 .5rem; }
dt { color: var(--muted); }
dd { margin: 0; }
code { font-size: .85em; overflow-wrap: anywhere; }
```

- [ ] **Step 6: Write** `internal/web/ui/auth.js`

```js
import { h } from './dom.js';

async function post(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

// renderSetup is the first-run screen: the admin password and where recordings go.
export async function renderSetup(root, done) {
  const vols = await fetch('/api/setup/volumes').then((r) => r.json()).catch(() => []);
  const pw = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const custom = h('input', { placeholder: '/absolute/path/for/recordings' });
  const choices = vols.map((v, i) => {
    const radio = h('input', { type: 'radio', name: 'vol', value: v.path });
    radio.checked = i === 0;
    return h('label', { class: 'inline' }, radio, `${v.label} — ${(v.freeMB / 1024).toFixed(1)} GB free`, h('code', {}, v.path));
  });
  const other = h('input', { type: 'radio', name: 'vol', value: '' });
  other.checked = vols.length === 0;
  const msg = h('p', { class: 'muted' });
  const form = h('form', {
    class: 'card form narrow',
    onsubmit: async (e) => {
      e.preventDefault();
      if (pw.value !== again.value) {
        msg.className = 'error';
        msg.textContent = 'The passwords do not match.';
        return;
      }
      const picked = form.querySelector('input[name=vol]:checked');
      const recDir = picked && picked.value ? picked.value : custom.value.trim();
      try {
        await post('/api/setup', { password: pw.value, recDir });
        done();
      } catch (err) {
        msg.className = 'error';
        msg.textContent = err.message;
      }
    },
  },
  h('h2', {}, 'Welcome to camorage'),
  h('p', {}, 'Choose the admin password. You will use it to sign in to this portal.'),
  h('label', {}, 'Password (8+ characters)', pw),
  h('label', {}, 'Password again', again),
  h('fieldset', {}, h('legend', {}, 'Where should recordings go?'), ...choices,
    h('label', { class: 'inline' }, other, 'Another folder:', custom)),
  msg,
  h('button', { class: 'primary', type: 'submit' }, 'Start recording'));
  root.replaceChildren(form);
  pw.focus();
}

// renderLogin asks for the admin password.
export function renderLogin(root, done) {
  const pw = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const msg = h('p', { class: 'muted' });
  const form = h('form', {
    class: 'card form narrow',
    onsubmit: async (e) => {
      e.preventDefault();
      try {
        await post('/api/login', { password: pw.value });
        done();
      } catch (err) {
        msg.className = 'error';
        msg.textContent = err.message;
        pw.select();
      }
    },
  }, h('h2', {}, 'camorage'), h('label', {}, 'Password', pw), msg, h('button', { class: 'primary', type: 'submit' }, 'Sign in'));
  root.replaceChildren(form);
  pw.focus();
}
```

- [ ] **Step 7: Write** `internal/web/ui/app.js`

```js
import { h } from './dom.js';
import { renderSetup, renderLogin } from './auth.js';
import { renderLive } from './live.js';
import { renderPlayback } from './playback.js';
import { renderCameras } from './cameras.js';
import { renderSettings } from './settings.js';

const views = { live: renderLive, playback: renderPlayback, cameras: renderCameras, settings: renderSettings };
const main = document.getElementById('view');
const bar = document.getElementById('bar');
let cleanup = null;
let generation = 0; // a view that finishes rendering after the user moved on is discarded

function teardown() {
  if (cleanup) cleanup();
  cleanup = null;
}

async function route() {
  const mine = ++generation;
  teardown();
  const name = location.hash.replace(/^#\/?/, '').split('/')[0] || 'live';
  for (const a of bar.querySelectorAll('nav a')) a.classList.toggle('active', a.getAttribute('href') === `#/${name}`);
  const container = h('div');
  main.replaceChildren(container);
  try {
    const done = (await (views[name] || views.live)(container)) || null;
    if (mine !== generation) {
      if (done) done();
      return;
    }
    cleanup = done;
  } catch (err) {
    if (mine === generation && err.message !== 'signed out') container.append(h('p', { class: 'error' }, err.message));
  }
}

async function boot() {
  generation++;
  teardown();
  bar.hidden = true;
  const health = await fetch('/api/health').then((r) => r.json());
  if (!health.setupDone) return renderSetup(main, boot);
  const probe = await fetch('/api/status', { credentials: 'same-origin' });
  if (probe.status === 401) return renderLogin(main, boot);
  bar.hidden = false;
  return route();
}

window.addEventListener('hashchange', () => { if (!bar.hidden) route(); });
window.addEventListener('camorage:signed-out', () => { if (!bar.hidden) boot(); });
boot();
```

- [ ] **Step 8: Implement** `internal/web/ui.go`, then change the end of `New` in `internal/web/web.go`

`internal/web/ui.go`:
```go
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// The single-page app. vendor/hls.min.js is hls.js 1.7.3 (Apache-2.0). *.mjs tests are not embedded.
//
//go:embed ui/*.html ui/*.css ui/*.js ui/vendor/*.js
var uiFiles embed.FS

func uiHandler() http.Handler {
	sub, _ := fs.Sub(uiFiles, "ui")
	return http.FileServerFS(sub)
}

// securityHeaders sets a strict CSP (scripts and styles only from this origin; media and hls.js
// workers from blob:) and anti-sniffing/anti-framing headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
```

In `internal/web/web.go`, replace the last line of `New`, `	return mux`, with:
```go
	mux.Handle("GET /", uiHandler())
	return securityHeaders(mux)
```

- [ ] **Step 9: Run every check**

Run:
```sh
cd ~/src/camorage && for f in internal/web/ui/*.js; do node --check "$f" || echo "SYNTAX $f"; done; node --test internal/web/ui/lib.test.mjs 2>&1 | grep -E '^# (pass|fail)'; gofmt -l internal/ cmd/; go vet ./... && go test -race ./... 2>&1 | tail -10
```
Expected: no `SYNTAX` lines; `# pass 7`, `# fail 0`; no gofmt output; every package `ok` (now including `camorage/internal/onvif`).

- [ ] **Step 10: Commit** (put the SHA-256 from Step 3 in the message)

```bash
git add internal/web/ui internal/web/ui.go internal/web/ui_test.go internal/web/web.go
git commit -m "feat(ui): app shell with setup/login, router, styles; embedded with strict CSP

Vendors hls.js 1.7.3 (sha256 <SHA from Step 3>).

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Wire the new dependencies in `main` and extend the laptop end-to-end test

**Files:**
- Modify: `cmd/camorage/main.go` (`web.Deps` literal, one import), `scripts/itest.sh` (one block)

**Interfaces:**
- Consumes: `onvif.LAN{}` (Task 2), `platform.Volumes` (Task 1), `mediamtx.HLSAddr`, `mediamtx.WebRTCAddr` (M1a), the Deps fields from Tasks 3–5.
- Produces: a `camorage` binary serving the UI and the live proxy; `scripts/itest.sh` checks both.

- [ ] **Step 1: Wire `main`.** In `cmd/camorage/main.go`:
  - Add `"camorage/internal/onvif"` to the import block (keeping imports sorted).
  - In the `web.Deps{…}` literal, after the line `OnSetup: restartMediaMTX, OnCamerasChanged: restartMediaMTX,`, add:
```go
			HLSBase: "http://" + mediamtx.HLSAddr, WebRTCBase: "http://" + mediamtx.WebRTCAddr,
			Volumes: platform.Volumes, ONVIF: onvif.LAN{},
```

Run: `gofmt -l cmd/ ; go vet ./cmd/... && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /dev/null ./cmd/camorage && echo BUILD-OK`
Expected: no gofmt output, `BUILD-OK`.

- [ ] **Step 2: Extend the end-to-end test.** In `scripts/itest.sh`, right after the line `echo "ok: camera available and recording"`, insert:

```bash
# M1b: the UI is served and live HLS works through the portal's proxy (incl. MediaMTX's redirect)
UI=$(curl -fsS "$B/")
[[ $UI == *'<title>camorage</title>'* ]] || fail "UI not served"
PL=""
for _ in $(seq 1 20); do PL=$(api GET /live/hls/test-cam_sub/index.m3u8 -L); [[ $PL == *'#EXTM3U'* ]] && break; sleep 1; done
[[ $PL == *'#EXTM3U'* ]] || fail "HLS through the portal: $PL"
echo "ok: UI served; HLS playlist through the portal proxy"
```

- [ ] **Step 3: Run the end-to-end test** (a background command, ~5 min)

Run: `cd ~/src/camorage && scripts/itest.sh`
Expected: seven `ok:` lines (including `ok: UI served; HLS playlist through the portal proxy`), then `ITEST PASS`.

- [ ] **Step 4: Commit**

```bash
git add cmd/camorage/main.go scripts/itest.sh
git commit -m "feat: wire UI, live proxy, ONVIF and volumes into camorage; e2e checks them

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Deploy and accept in a real browser against the phone

**Files:** none (acceptance only)

**Interfaces:**
- Consumes: `scripts/deploy.sh`, `.cache/admin-password`, `.cache/jar` (M1a), `spike/env.sh`; the chrome-devtools MCP (load with ToolSearch `select:mcp__plugin_chrome-devtools-mcp_chrome-devtools__new_page,mcp__plugin_chrome-devtools-mcp_chrome-devtools__navigate_page,mcp__plugin_chrome-devtools-mcp_chrome-devtools__evaluate_script,mcp__plugin_chrome-devtools-mcp_chrome-devtools__take_screenshot,mcp__plugin_chrome-devtools-mcp_chrome-devtools__resize_page,mcp__plugin_chrome-devtools-mcp_chrome-devtools__list_console_messages`).
- Produces: M1b running on the phone and verified end to end, plus screenshots in `.cache/ui-*.png`.
- **Convention:** `PHONE` below means `$PHONE_IP` from `spike/env.sh` (currently 192.168.1.29). In every `evaluate_script`, `<ADMIN_PASSWORD>` means the contents of `.cache/admin-password`.

- [ ] **Step 1: Deploy**

Run: `cd ~/src/camorage && scripts/deploy.sh 2>&1 | tail -2`
Expected: `{"ok":true,"setupDone":true}`.

- [ ] **Step 2: Sign in through the UI.** Open `http://PHONE:8080/` with `new_page`, then run `evaluate_script`:
```js
async () => {
  const pw = document.querySelector('input[type=password]');
  pw.value = '<ADMIN_PASSWORD>';
  pw.form.requestSubmit();
  await new Promise((r) => setTimeout(r, 3000));
  return { signedIn: !document.getElementById('bar').hidden, hash: location.hash || '(none)' };
}
```
Expected: `signedIn: true`.

- [ ] **Step 3: The live tile plays the substream over HLS** (`evaluate_script`):
```js
async () => {
  await new Promise((r) => setTimeout(r, 8000));
  const v = document.querySelector('.tile video');
  const a = v.currentTime;
  await new Promise((r) => setTimeout(r, 3000));
  return { a, b: v.currentTime, w: v.videoWidth, h: v.videoHeight, badge: document.querySelector('.tile .badge').textContent };
}
```
Expected: `b > a`, `w: 640, h: 360`, `badge: "● REC"`.

- [ ] **Step 4: Playback timeline** (`evaluate_script`):
```js
async () => {
  location.hash = '#/playback';
  await new Promise((r) => setTimeout(r, 3000));
  const spans = [...document.querySelectorAll('.timeline .span')];
  const last = spans[spans.length - 1].getBoundingClientRect();
  document.querySelector('.timeline').dispatchEvent(new MouseEvent('click', { clientX: last.left + last.width / 2, clientY: last.top + 5, bubbles: true }));
  await new Promise((r) => setTimeout(r, 8000));
  const v = document.querySelector('video.player');
  return { spans: spans.length, t: v.currentTime, w: v.videoWidth, label: document.querySelector('.timeline ~ .ticks + p').textContent };
}
```
Expected: `spans ≥ 1`, `t > 0`, `w: 1280`, `label` starts with `Playing`.

- [ ] **Step 5: Full screen over WebRTC, then the HLS fallback (Review Focus 5).** Run `evaluate_script`:
```js
async () => {
  location.hash = '#/live';
  await new Promise((r) => setTimeout(r, 3000));
  document.querySelector('.tile').click();
  await new Promise((r) => setTimeout(r, 7000));
  const v = document.querySelector('.overlay video');
  return { mode: document.querySelector('.overlay .badge').textContent, t: v.currentTime, w: v.videoWidth };
}
```
Expected: `mode: "WebRTC"`, `w: 1280`, `t > 0`. Confirm on the phone with `. spike/env.sh && $P 'grep -i "peer connection established" ~/.camorage/logs/mediamtx.log | tail -1'`, which should print one line.

Then simulate a browser/network without WebRTC:
```js
async () => {
  document.querySelector('.overlay .full-bar button').click();
  window.RTCPeerConnection = undefined;
  document.querySelector('.tile').click();
  await new Promise((r) => setTimeout(r, 9000));
  const v = document.querySelector('.overlay video');
  const out = { mode: document.querySelector('.overlay .badge').textContent, t: v.currentTime, w: v.videoWidth };
  location.reload(); // restore RTCPeerConnection
  return out;
}
```
Expected: `mode: "HLS"`, `w: 1280`, `t > 0`.

- [ ] **Step 6: Cameras page, ONVIF scan, HTML-in-name safety (Review Focus 3), settings.** First create a camera with a hostile name from the laptop:
```sh
cd ~/src/camorage && . spike/env.sh && B=http://$PHONE_IP:8080
XSS=$(curl -sS -b .cache/jar -H "Origin: $B" -H 'Content-Type: application/json' -d '{"name":"<img src=x onerror=\"window.__xss=1\">","enabled":false,"mainUrl":"rtsp://127.0.0.1/none"}' "$B/api/cameras"); echo "$XSS"
```
Expected: `{"id":"…"}`. Then run `evaluate_script`:
```js
async () => {
  location.hash = '#/cameras';
  await new Promise((r) => setTimeout(r, 3000));
  const text = document.querySelector('main').textContent;
  const out = { xssRan: window.__xss === 1, injectedImg: !!document.querySelector('main img[src="x"]'), shownAsText: text.includes('<img src=x') };
  [...document.querySelectorAll('button')].find((b) => b.textContent === 'Scan network').click();
  await new Promise((r) => setTimeout(r, 6000));
  out.found = [...document.querySelectorAll('.found span')].map((s) => s.textContent);
  location.hash = '#/settings';
  await new Promise((r) => setTimeout(r, 3000));
  const s = document.querySelector('main').textContent;
  out.settings = { battery: s.includes('Battery'), mediamtx: s.includes('mediamtx') };
  return out;
}
```
Expected: `xssRan: false`, `injectedImg: false`, `shownAsText: true`; `found` includes an entry containing `192.168.1.129`; `settings.battery` and `settings.mediamtx` are true. Delete the test camera:
```sh
ID=$(echo "$XSS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'); curl -sS -b .cache/jar -H "Origin: $B" -X DELETE "$B/api/cameras/$ID"; echo
```
Expected: `{"ok":true}`.

- [ ] **Step 7: Tiles recover after MediaMTX dies (Review Focus 1).** Run `evaluate_script`: `async () => { location.hash = '#/live'; await new Promise((r) => setTimeout(r, 8000)); return document.querySelector('.tile video').currentTime; }`. Expected: `> 0`. Then run:
```sh
cd ~/src/camorage && . spike/env.sh && $P 'pkill -x mediamtx; sleep 3; echo "mediamtx back: $(pgrep -x mediamtx)"'
```
Expected: a new PID (the supervisor restarted it). Then `evaluate_script`:
```js
async () => {
  await new Promise((r) => setTimeout(r, 20000));
  const v = document.querySelector('.tile video');
  const a = v.currentTime;
  await new Promise((r) => setTimeout(r, 3000));
  return { a, b: v.currentTime };
}
```
Expected: `b > a` (the tile is playing again without a reload).

- [ ] **Step 8: The session ends while a page is open (Review Focus 2).** Run `evaluate_script`:
```js
async () => {
  await fetch('/api/logout', { method: 'POST', credentials: 'same-origin' });
  await new Promise((r) => setTimeout(r, 5000));
  return { loginShown: document.getElementById('bar').hidden && !!document.querySelector('form input[type=password]'), tiles: document.querySelectorAll('.tile').length };
}
```
Expected: `loginShown: true`, `tiles: 0`. Then `list_console_messages`: there must be no `Uncaught` errors. Browser "Failed to load resource … 401" lines are expected.

- [ ] **Step 9: Phone-sized screen (Review Focus 4).** Sign in again with the Step 2 script, then `resize_page` to 390×844. For each of `#/live`, `#/playback`, `#/cameras`, `#/settings`, run:
```js
async () => { location.hash = '#/cameras'; await new Promise((r) => setTimeout(r, 3000)); return { fits: document.documentElement.scrollWidth <= innerWidth + 1, scrollWidth: document.documentElement.scrollWidth, innerWidth }; }
```
(Change the hash for each page.) Take a screenshot to `.cache/ui-<page>-mobile.png` after each. Expected: `fits: true` on all four. Read the cameras and settings screenshots to confirm the layout looks right.

- [ ] **Step 10: Report to the user:**
  - **Where:** the portal is at `http://PHONE:8080` on home Wi-Fi, and what was verified.
  - **Change the admin password in Settings now.** The generated one was used by this automated test.
  - **Next:** remote access (M1c).
