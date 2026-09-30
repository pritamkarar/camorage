# M1c Remote Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reach the portal from anywhere. Two routes:
- **Tailscale:** `https://camorage-phone.tail0a1b2c.ts.net`, for the owner's devices; WebRTC works here.
- **Cloudflare Tunnel:** `https://cams.example.com`, a public domain; video uses HLS.

Both are switched on and watched from Settings. First, close the live-proxy path bypass that M1b deferred.

**Architecture:**
- **New package `internal/tunnel`:**
  - It runs cloudflared and tailscaled as supervised children. On Android they run under `proot`, with a generated `resolv.conf` and a CA bundle.
  - It watches tailscaled through its LocalAPI socket. It asks for a Tailscale sign-in link when one is needed, and sets up `tailscale serve` once Tailscale is running.
  - When the phone's Tailscale IP appears, MediaMTX is restarted so WebRTC can offer that IP.
- **Supervisor:** gets process groups, because proot ignores SIGTERM.
- **Auth:** trusts only the header each tunnel actually sets.
- **Web:** gains `/api/tunnels`. Settings gets a "Remote access" section.

**Tech Stack:**
- Go 1.27 stdlib (+ `golang.org/x/crypto`, already present).
- cloudflared 2026.9.3, Tailscale 1.102.4, MediaMTX v1.21.1, proot (Termux).
- Vanilla ES modules; Node 24 `node --test`.
- Chrome via the chrome-devtools MCP for acceptance.

**Spec:** `docs/superpowers/specs/2026-09-30-camorage-portal-design.md`. Relevant sections:
- §7 Remote access & security.
- §3 components (`internal/tunnel`, `internal/platform`, `internal/supervisor`).
- §6 Settings.
- The M1 row of §10.

Builds on M1a and M1b, merged on `main`.

## Global Constraints

**Branch and code**
- Work on branch `m1c-remote`, created from `main`. Commit after each task with the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Module `camorage`, Go 1.27. **No new Go dependencies:** the spec limits external deps to `golang.org/x/crypto`.
- UI: no npm packages and no build step.
- Phone build: `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`.

**UI and CSP**
- The CSP is unchanged: `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; worker-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`.
- No inline scripts or styles.
- User-visible text goes in via `h()` / `textContent` only.
- External links are plain `<a href target="_blank" rel="noopener">`.

**How the tunnels reach the portal:** both connect from loopback.
- **cloudflared:** the tunnel's public hostname maps to `http://127.0.0.1:8080` in the Cloudflare dashboard. This deployment uses **`cams.example.com`**.
- **`tailscale serve`:** serves `https://<machine>.<tailnet>.ts.net` from `http://127.0.0.1:<listen port>`.

**Android wrapper**
- When the system has no `/etc/resolv.conf` (Android), cloudflared and tailscaled run as `proot -b <data>/resolv.conf:/etc/resolv.conf <program> …`.
- `SSL_CERT_FILE=<data>/certs.pem` is inherited from the portal's environment.
- Elsewhere (the laptop) they run directly.

**Binaries:** cloudflared 2026.9.3, `tailscale` and `tailscaled` 1.102.4, next to the camorage binary. On the phone that is `~/camorage/bin/`, copied from `~/spike/`.

**The Cloudflare token is a secret:**
- Stored `0600` in `config.json`.
- Never returned by the API (only `tokenSet`).
- Handed to cloudflared in `TUNNEL_TOKEN`, never on its command line.
- The user pastes it into the portal, never into the chat.

**Tunnel routes:**
- Every `/api/tunnels*` route requires a session, so tunnels cannot be enabled before first-run setup (spec §7).
- The mutating routes also pass the existing Origin check.

**Tailscale identity:**
- New sign-ins use the machine name `camorage`.
- This phone keeps its M0 node, `camorage-phone` (100.64.0.7, `https://camorage-phone.tail0a1b2c.ts.net`), by reusing the M0 state (Task 10).

**Phone dev access (unchanged):**
- Connect with `. spike/env.sh; $P '<cmd>'` and deploy with `scripts/deploy.sh`.
- Start background processes as `( nohup … </dev/null & )`.
- Never `pkill -f` a pattern that also appears in the same SSH command.
- The admin password is in `.cache/admin-password`.

## Facts established before this plan (2026-09-30 probes)

**proot (on the phone)**
- **proot ignores SIGTERM.** `kill -TERM <proot>` left both proot and its program running.
- **`kill -KILL <proot>` orphans the program.** The program was re-parented to init and kept running.
- **`kill -TERM -<pgid>` stopped both.** Signalling the whole process group works.

**tailscaled (laptop, 1.102.4)**
- **LocalAPI status:** `GET http://local-tailscaled.sock/localapi/v0/status` over the unix socket answers without special headers. It returns `BackendState`, `AuthURL` and `Self{DNSName, TailscaleIPs}`.
- **The sign-in link survives the CLI:** on a fresh node, `tailscale up --timeout=8s` exits 1 with `timeout waiting for Tailscale service to enter a Running state`. The sign-in link stays in `AuthURL` afterwards.
- **A second daemon is fine:** another userspace `tailscaled` runs next to the system one without `--port`.

**Proxy headers**
- **`tailscale serve`** (source v1.102.4, `ipn/ipnlocal/serve.go`):
  - It keeps `Host` and sets `X-Forwarded-Host` and `X-Forwarded-Proto: https`.
  - It **replaces** `X-Forwarded-For` with the tailnet peer's address.
  - `serve --bg <target>` overwrites the handler at the same port and mount. The M0 state still points at `:8888`, and the manager re-points it.
- **Cloudflare:**
  - The edge appends the address it saw to `X-Forwarded-For`, after anything the client sent.
  - cloudflared forwards the original `Host`. It adds no `X-Forwarded-For` of its own.

**Other**
- **Phone DNS:** `net.dns1` = `2409:40e0:11ae:47e6:ba3b:abff:fee2:9109` (router, IPv6), `net.dns2` = `192.168.1.1`.
- **Existing bypass:** `/live/hls/cam%2F..%2Fother/...` passes today's allow-list. Go's mux cleans only the *escaped* path, and `PathValue` returns it decoded.

## Review Focus

1. **Pasting the dashboard's whole install command instead of the bare token.** Examples: `cloudflared service install eyJ…`, with `sudo`, `.exe`, stray spaces or a trailing newline. The portal must extract the token. → Task 5, `TestParseCloudflareToken`.
2. **Opening the tailnet IP directly (`http://100.64.0.7:8080`), as done in M0.** Userspace tailscaled hands this over from loopback, but over plain HTTP. Sign-in must still work: the cookie must not be `Secure` there. → Task 4, `TestCookieSecureOnlyViaTunnel`.
3. **Tailscale turned on while tailscaled is missing, crashed or still starting.** Settings must say why, not "Starting…" forever. A momentarily missing IP must not restart MediaMTX. → Task 6, `TestPollShowsWhyTailscaleIsNotUp` and `TestPollRunningServesOnceAndReportsIP`.
4. **camorage killed with `kill -9`, or crashing, while cloudflared and tailscaled run under proot.** The next start must leave exactly one of each, never a second tunnel. → Task 2, `TestKillsOrphanGroupFromPreviousRun`; Task 10, step 9.
5. **Saving the Cloudflare hostname, or an unrelated setting, while connected through a tunnel.** The tunnel must not restart under you. → Task 6, `TestApplyActsOnlyOnChanges`.

---

### Task 1: Close the live-proxy path escape

**Files:**
- Modify: `internal/web/live.go`
- Test: `internal/web/live_test.go`

**Interfaces:**
- Consumes (M1b):
  - `(*server).livePath(p string) bool`
  - `(*server).proxy(w, r, base, path string, fixLocation func(string) string)`
  - `whepLocation`
  - Test helpers: `newEnv(t, mods...)`, `e.do(method, path, body, mods...)`, `e.setUp()`, `e.waitChanged()`, the `gate` camera JSON (`front-gate` with a substream).
- Produces:
  - `/live/hls/{rest...}` refuses any `rest` that is not already clean.
  - `/live/whep/{path}/{session}` refuses sessions that are not UUIDs.
  - Proxied responses carry no `Access-Control-*` headers.

- [ ] **Step 1: Create the branch**

Run: `cd ~/src/camorage && git switch -c m1c-remote && git branch --show-current`
Expected: `m1c-remote`

- [ ] **Step 2: Write the failing tests** (append to `internal/web/live_test.go`)

```go
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
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'TestLiveProxy(RefusesEscapes|DropsCORSHeaders)' -v`
Expected: FAIL.
- The escape paths get `200`, not `404`, and `reached MediaMTX:` lists them.
- `Access-Control-Allow-Origin reached the browser`.

- [ ] **Step 4: Implement** (in `internal/web/live.go`)

Add `"path"` and `"regexp"` to the imports, and this variable below them:

```go
// whepSessionRe matches MediaMTX's WHEP session ids (UUIDs).
var whepSessionRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
```

Replace `hls`, `whepOffer` and `whepSession` with the versions below. The local `path` variables become `stream`, which frees the name for the `path` package.

```go
// hls proxies GET /live/hls/<path>/<file> to MediaMTX's HLS server.
func (s *server) hls(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	stream, _, _ := strings.Cut(rest, "/")
	// PathValue is decoded: "cam%2F..%2Fother" arrives as "cam/../other", which MediaMTX would
	// resolve to another path. Only paths that are already clean go through.
	if path.Clean("/"+rest) != "/"+rest || !s.livePath(stream) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.HLSBase, "/"+rest, func(loc string) string { return "/live/hls" + loc })
}

// whepOffer proxies POST /live/whep/<path> (an SDP offer) to MediaMTX's WHEP endpoint.
func (s *server) whepOffer(w http.ResponseWriter, r *http.Request) {
	stream := r.PathValue("path")
	if !s.livePath(stream) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.WebRTCBase, "/"+stream+"/whep", whepLocation)
}

// whepSession proxies PATCH (trickle ICE) and DELETE (hang up) of a WHEP session.
func (s *server) whepSession(w http.ResponseWriter, r *http.Request) {
	stream, session := r.PathValue("path"), r.PathValue("session")
	if !s.livePath(stream) || !whepSessionRe.MatchString(session) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.WebRTCBase, "/"+stream+"/whep/"+session, whepLocation)
}
```

In `proxy`, extend `ModifyResponse` so it reads:

```go
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			for k := range resp.Header {
				if strings.HasPrefix(k, "Access-Control-") { // MediaMTX allows any origin; the portal does not
					resp.Header.Del(k)
				}
			}
			if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, "/") {
				resp.Header.Set("Location", fixLocation(loc))
			}
			return nil
		},
```

- [ ] **Step 5: Run the web tests**

Run: `gofmt -l internal/ ; go test -race ./internal/web/`
Expected: no gofmt output, then `ok  camorage/internal/web`. The existing `TestLiveWHEPProxy` still passes: its session id is a UUID.

- [ ] **Step 6: Commit**

```bash
git add internal/web/live.go internal/web/live_test.go
git commit -m "fix(web): live proxy refuses encoded ../ escapes and non-UUID WHEP sessions, drops CORS headers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Supervisor signals whole process groups

**Files:**
- Modify: `internal/supervisor/supervisor.go`
- Test: `internal/supervisor/supervisor_test.go`
- Modify: `docs/superpowers/specs/2026-09-30-camorage-portal-design.md` (§3 supervisor row)

**Interfaces:**
- Consumes: `Spec`, `New`, `Start`, `Stop`, `StopAll`, `killOrphan`, `waitRunning` (test helper).
- Produces:
  - Every child runs in its own process group (`Setpgid`).
  - `Stop` and orphan cleanup signal the group.
  - Tasks 6 and 9 rely on this: stopping the `proot` wrapper must stop the program it runs.

- [ ] **Step 1: Write the failing tests** (append to `internal/supervisor/supervisor_test.go`)

```go
// prootLike behaves like proot: it ignores SIGTERM while its child runs. The child's PID goes to file.
func prootLike(file string) Spec {
	return Spec{Name: "tracer", Path: "sh", Args: []string{"-c", "sleep 30 & echo $! > " + file + "; trap '' TERM; wait"}}
}

func readPID(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(file); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("grandchild never started")
	return 0
}

func TestStopReachesGrandchildren(t *testing.T) {
	dir := t.TempDir()
	gc := filepath.Join(dir, "gc.pid")
	s := New(dir)
	s.Start(prootLike(gc))
	pid := readPID(t, gc)
	began := time.Now()
	s.Stop("tracer")
	if d := time.Since(began); d > 3*time.Second {
		t.Fatalf("Stop took %v: SIGTERM missed the child's process group", d)
	}
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("grandchild survived Stop")
	}
}

func TestKillsOrphanGroupFromPreviousRun(t *testing.T) {
	dir := t.TempDir()
	gc := filepath.Join(dir, "gc.pid")
	spec := prootLike(gc)
	orphan := exec.Command(spec.Path, spec.Args...)
	orphan.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // as the supervisor starts children
	if err := orphan.Start(); err != nil {
		t.Fatal(err)
	}
	go orphan.Wait()
	pid := readPID(t, gc)
	os.WriteFile(filepath.Join(dir, "tracer.pid"), []byte(strconv.Itoa(orphan.Process.Pid)), 0o600)
	os.Remove(gc)
	s := New(dir)
	defer s.StopAll()
	s.Start(spec)
	readPID(t, gc) // the new child is up, so the orphan was dealt with before it started
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(-orphan.Process.Pid, syscall.SIGKILL)
		t.Fatal("the orphan's child survived: a second tunnel would be running")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/supervisor/ -run 'TestStopReachesGrandchildren|TestKillsOrphanGroupFromPreviousRun' -v`
Expected: FAIL.
- `Stop took 5.0…s: SIGTERM missed the child's process group`.
- `the orphan's child survived: a second tunnel would be running`.

- [ ] **Step 3: Implement** (in `internal/supervisor/supervisor.go`)

In `startOnce`, right after `cmd.Stdout, cmd.Stderr = logf, logf`, add:

```go
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // its own process group: see signal()
```

Replace `signal`:

```go
// signal signals the child's whole process group. proot (the Android wrapper of cloudflared and
// tailscaled) ignores SIGTERM, and when killed it leaves the program it runs behind (M1c).
func (p *proc) signal(sig syscall.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil && p.st.Running {
		_ = syscall.Kill(-p.cmd.Process.Pid, sig)
	}
}
```

In `killOrphan`, replace `_ = syscall.Kill(pid, syscall.SIGTERM)` with `killGroup(pid, syscall.SIGTERM)`, and replace the final `_ = syscall.Kill(pid, syscall.SIGKILL)` with `killGroup(pid, syscall.SIGKILL)`. Add below `killOrphan`:

```go
// killGroup signals pid's process group, or pid alone when it leads none (a child left by a
// camorage from before children got their own groups).
func killGroup(pid int, sig syscall.Signal) {
	if syscall.Kill(-pid, sig) != nil {
		_ = syscall.Kill(pid, sig)
	}
}
```

- [ ] **Step 4: Run the supervisor tests**

Run: `gofmt -l internal/ ; go test -race ./internal/supervisor/`
Expected: `ok  camorage/internal/supervisor`. The whole package passes, including `TestStopRightAfterStartIsPrompt` and `TestConcurrentStartKeepsOneChild`.

- [ ] **Step 5: Amend the spec** (§3 components table, `internal/supervisor` row)

Replace `start/stop/restart child processes with backoff (1 s → 60 s), capture logs (rotating), expose status` with:
`start/stop/restart child processes with backoff (1 s → 60 s), capture logs (rotating), expose status; each child gets its own process group and stops signal the group (proot ignores SIGTERM and orphans its program when killed — M1c)`

- [ ] **Step 6: Commit**

```bash
git add internal/supervisor/ docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "fix(supervisor): run children in their own process group and signal the group

proot ignores SIGTERM and orphans its program on SIGKILL (probed on the phone).

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: CA bundle and resolv.conf for proot-wrapped children

**Files:**
- Create: `internal/platform/netconf.go`
- Create: `internal/platform/cacert.pem` (copied from `/etc/ssl/certs/ca-certificates.crt`)
- Test: `internal/platform/netconf_test.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: the spec (§3 platform row)

**Interfaces:**
- Consumes: `config.WriteFileAtomic(path string, b []byte) error` (M1a).
- Produces (Task 9 uses these):
  - `platform.CACerts []byte`
  - `platform.ResolvConf(servers ...string) []byte`
  - `config.WriteFileIfChanged(path string, b []byte) error`
- Not in M1c: the spec's DNS dialer for the portal itself. The portal resolves no names until M3 (cloud storage).

- [ ] **Step 1: Write the failing tests**

`internal/platform/netconf_test.go`:

```go
package platform

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestResolvConf(t *testing.T) {
	got := string(ResolvConf("2409:40e0:11ae:47e6:ba3b:abff:fee2:9109", "192.168.1.1"))
	want := "nameserver 2409:40e0:11ae:47e6:ba3b:abff:fee2:9109\nnameserver 192.168.1.1\nnameserver 1.1.1.1\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// off Android getprop returns nothing; junk and repeats are skipped
	if got := string(ResolvConf("", " junk ", "1.1.1.1")); got != "nameserver 1.1.1.1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestCACertsIncludeLetsEncrypt(t *testing.T) {
	rest, n, isrg := CACerts, 0, false
	for {
		var blk *pem.Block
		if blk, rest = pem.Decode(rest); blk == nil {
			break
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		n++
		isrg = isrg || c.Subject.CommonName == "ISRG Root X1"
	}
	if n < 100 || !isrg {
		t.Fatalf("%d certificates, ISRG Root X1: %v", n, isrg)
	}
}
```

Append to `internal/config/config_test.go`:

```go
func TestWriteFileIfChangedLeavesSameContentAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "resolv.conf")
	if err := WriteFileIfChanged(p, []byte("a\n")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	if err := WriteFileIfChanged(p, []byte("a\n")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if !os.SameFile(before, after) {
		t.Fatal("rewritten although unchanged")
	}
	if err := WriteFileIfChanged(p, []byte("b\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "b\n" {
		t.Fatalf("got %q", b)
	}
}
```

If `config_test.go` does not import `os` or `path/filepath` yet, add them.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/platform/ ./internal/config/ 2>&1 | tail -6`
Expected: build failures, `undefined: ResolvConf`, `undefined: CACerts` and `undefined: WriteFileIfChanged`.

- [ ] **Step 3: Implement**

Run: `cp /etc/ssl/certs/ca-certificates.crt internal/platform/cacert.pem && grep -c 'BEGIN CERTIFICATE' internal/platform/cacert.pem`
Expected: `121` (any number ≥ 100 is fine).

`internal/platform/netconf.go`:

```go
package platform

import (
	_ "embed"
	"net/netip"
	"slices"
	"strings"
)

// CACerts is the build machine's Mozilla CA bundle (/etc/ssl/certs/ca-certificates.crt). Android 6
// predates Let's Encrypt's ISRG Root X1, which Tailscale's HTTPS certificates chain to; the portal
// writes this to <data>/certs.pem and points SSL_CERT_FILE at it.
//
//go:embed cacert.pem
var CACerts []byte

// ResolvConf renders the resolv.conf that proot shows cloudflared and tailscaled: the phone's DNS
// servers (getprop net.dns1, net.dns2; IPv4 or IPv6), then 1.1.1.1. Blanks, junk and repeats are
// skipped.
func ResolvConf(servers ...string) []byte {
	var b strings.Builder
	seen := map[netip.Addr]bool{}
	for _, s := range slices.Concat(servers, []string{"1.1.1.1"}) {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || seen[a] {
			continue
		}
		seen[a] = true
		b.WriteString("nameserver " + a.String() + "\n")
	}
	return []byte(b.String())
}
```

In `internal/config/config.go`, add `"bytes"` to the imports and this function below `WriteFileAtomic`:

```go
// WriteFileIfChanged is WriteFileAtomic, skipped when path already holds b, so readers that
// watch the file (Go's resolver re-reads resolv.conf when it changes) are left alone.
func WriteFileIfChanged(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	return WriteFileAtomic(path, b)
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l internal/ ; go test -race ./internal/platform/ ./internal/config/`
Expected: two `ok` lines.

- [ ] **Step 5: Amend the spec** (§3 components table, `internal/platform` row)

Replace ``(`nameserver <net.dns1>` + `nameserver 1.1.1.1`, rewritten whenever `net.dns1` changes;`` with:
``(`nameserver <net.dns1>` + `nameserver <net.dns2>` + `nameserver 1.1.1.1`, re-checked every minute and rewritten when they change;``

- [ ] **Step 6: Commit**

```bash
git add internal/platform/ internal/config/ docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "feat(platform): embedded CA bundle and generated resolv.conf for proot-wrapped children

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Trust only what each tunnel sets (client IP, Secure cookie)

**Files:**
- Modify: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`
- Modify: the spec (§7 Auth and Lockout bullets)

**Interfaces:**
- Consumes: `FromLoopback(r)`; the test helper `req(remote string, hdr map[string]string) *http.Request`.
- Produces:
  - `auth.ViaTunnel(r *http.Request) bool`: a loopback peer **and** an `X-Forwarded-For` header.
  - `auth.ClientIP(r)` returns the last `X-Forwarded-For` entry through a tunnel. It never trusts `CF-Connecting-IP`.
  - `SetCookie` and `ClearCookie` set `Secure` iff `ViaTunnel(r)`.

- [ ] **Step 1: Write the failing tests**

In `internal/auth/auth_test.go`, replace `TestClientIP` and `TestCookieSecureOnlyViaTunnel`:

```go
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
		{"[::1]:5555", map[string]string{"X-Forwarded-For": " 2001:db8::7 "}, "2001:db8::7"},
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
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/auth/ -run 'TestClientIP|TestCookieSecureOnlyViaTunnel' -v`
Expected: FAIL.
- `got 6.6.6.6, want 100.70.24.39`.
- `got 6.6.6.6, want 127.0.0.1`.
- The `127.0.0.1:1 map[]` cookie case is `Secure`.

- [ ] **Step 3: Implement** (in `internal/auth/auth.go`)

Replace `ClientIP` with this, and add `ViaTunnel` above it:

```go
// ViaTunnel reports whether r came through cloudflared or `tailscale serve`: both connect from
// loopback and set X-Forwarded-For. Userspace tailscaled also hands a tailnet device's plain
// http://<tailscale-ip>:8080 request over from loopback, but without that header (M1c).
func ViaTunnel(r *http.Request) bool {
	return FromLoopback(r) && r.Header.Get("X-Forwarded-For") != ""
}

// ClientIP is the address login lockout counts. Through a tunnel it is the last X-Forwarded-For
// entry: Cloudflare appends the address it saw and `tailscale serve` replaces the header with the
// tailnet peer, so that entry is never the client's own claim. CF-Connecting-IP is ignored, since
// through Tailscale anyone could send one. Anyone on the LAN could forge any header, so every other
// request counts by its peer address.
func ClientIP(r *http.Request) string {
	if ViaTunnel(r) {
		xff := r.Header.Values("X-Forwarded-For")
		parts := strings.Split(xff[len(xff)-1], ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}
```

In `SetCookie` and `ClearCookie`, change `Secure: FromLoopback(r)` to `Secure: ViaTunnel(r)`. Update the `SetCookie` comment to: `// SetCookie sets the session cookie; Secure when the request came through an HTTPS tunnel.`

- [ ] **Step 4: Run auth and web tests** (login lockout uses `ClientIP`)

Run: `gofmt -l internal/ ; go test -race ./internal/auth/ ./internal/web/`
Expected: two `ok` lines.

- [ ] **Step 5: Amend the spec (§7)**

1. In the **Auth** bullet, replace `` `Secure` when the TCP peer is loopback (both tunnels terminate HTTPS and connect via loopback; plain-HTTP LAN requests come from non-loopback peers) `` with:
   `` `Secure` when the request came through a tunnel: a loopback peer **with** `X-Forwarded-For` (both tunnels terminate HTTPS, connect via loopback and set that header; a tailnet device opening `http://<tailscale-ip>:8080` is also handed over from loopback by userspace tailscaled, but over plain HTTP and without the header) ``
2. Replace the whole **Lockout** bullet with:
   ``- **Lockout:** 5 failed logins → 15-min lockout per client IP. Client IP = the **last** `X-Forwarded-For` entry when the request came through a tunnel (Cloudflare appends the address it saw; `tailscale serve` replaces the header with the tailnet peer), otherwise the peer address. `CF-Connecting-IP` is not trusted: anyone on the tailnet could send it (M1c).``

- [ ] **Step 6: Commit**

```bash
git add internal/auth/ docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "fix(auth): lockout counts the address the tunnel saw; Secure cookie only through a tunnel

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Tunnel process specs and Cloudflare inputs

**Files:**
- Create: `internal/tunnel/tunnel.go`
- Test: `internal/tunnel/tunnel_test.go`
- Modify: `internal/config/config.go` (a `CloudflareHostname` field plus validation)
- Test: `internal/config/config_test.go`
- Modify: the spec (§4 config row, §7 Cloudflare bullet)

**Interfaces:**
- Consumes: `supervisor.Spec{Name, Path string; Args, Env []string}`; `config.ValidationError`, `invalid(...)` (config package).
- Produces (Tasks 6, 7 and 9 use these):
  - `config.Tunnels{CloudflareToken string; CloudflareHostname string; TailscaleEnabled bool}`. The JSON keys are `cloudflareToken`, `cloudflareHostname`, `tailscaleEnabled`.
  - `Config.Validate` rejects a non-empty hostname that is not a lower-case DNS name.
  - `tunnel.CloudflaredMetrics = "127.0.0.1:20241"`
  - `tunnel.Bins{Cloudflared, Tailscaled, Tailscale, Proot, ResolvConf, StateDir string}`, with these methods:
    - `(Bins) Socket() string`
    - `(Bins) CloudflaredSpec(token string) supervisor.Spec`
    - `(Bins) TailscaledSpec() supervisor.Spec`
    - `(Bins) RunTailscale(ctx context.Context, args ...string) ([]byte, error)`
  - `tunnel.ParseCloudflareToken(s string) (string, error)`

- [ ] **Step 1: Write the failing tests**

`internal/tunnel/tunnel_test.go`:

```go
package tunnel

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSpecs(t *testing.T) {
	b := Bins{Cloudflared: "/bin/cloudflared", Tailscaled: "/bin/tailscaled", StateDir: "/d/tailscale", ResolvConf: "/d/resolv.conf"}
	cf := b.CloudflaredSpec("SECRET")
	if cf.Name != "cloudflared" || cf.Path != "/bin/cloudflared" || strings.Join(cf.Args, " ") != "tunnel --no-autoupdate --metrics 127.0.0.1:20241 run" {
		t.Fatalf("cloudflared: %+v", cf)
	}
	if strings.Join(cf.Env, " ") != "TUNNEL_TOKEN=SECRET" {
		t.Fatalf("cloudflared env: %v", cf.Env)
	}
	ts := b.TailscaledSpec()
	if ts.Name != "tailscaled" || ts.Path != "/bin/tailscaled" || strings.Join(ts.Args, " ") != "--tun=userspace-networking --statedir=/d/tailscale --socket=/d/tailscale/sock" {
		t.Fatalf("tailscaled: %+v", ts)
	}

	b.Proot = "/usr/bin/proot" // Android: no /etc/resolv.conf, so proot shows them ours
	cf = b.CloudflaredSpec("SECRET")
	if cf.Path != "/usr/bin/proot" || strings.Join(cf.Args, " ") != "-b /d/resolv.conf:/etc/resolv.conf /bin/cloudflared tunnel --no-autoupdate --metrics 127.0.0.1:20241 run" {
		t.Fatalf("cloudflared under proot: %+v", cf)
	}
	if strings.Contains(strings.Join(cf.Args, " "), "SECRET") {
		t.Fatal("the token is on the command line, where every app can read it")
	}
	if ts := b.TailscaledSpec(); ts.Path != "/usr/bin/proot" || ts.Args[2] != "/bin/tailscaled" {
		t.Fatalf("tailscaled under proot: %+v", ts)
	}
}

func token(fields map[string]string) string {
	b, _ := json.Marshal(fields)
	return base64.StdEncoding.EncodeToString(b)
}

func TestParseCloudflareToken(t *testing.T) {
	tok := token(map[string]string{"a": "acct", "t": "00000000-0000-4000-8000-000000000000", "s": "c2VjcmV0"})
	for _, in := range []string{
		tok,
		"  " + tok + "\n",
		"cloudflared service install " + tok,
		"sudo cloudflared service install " + tok,
		"cloudflared.exe service install " + tok,
		strings.TrimRight(tok, "="),
	} {
		got, err := ParseCloudflareToken(in)
		if err != nil || strings.TrimRight(got, "=") != strings.TrimRight(tok, "=") {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "hello", "cloudflared service install", token(map[string]string{"a": "acct"})} {
		if _, err := ParseCloudflareToken(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
```

Append to `internal/config/config_test.go`:

```go
func TestValidateCloudflareHostname(t *testing.T) {
	for h, ok := range map[string]bool{
		"": true, "cams.example.com": true, "a.example.com": true,
		"localhost": false, "https://a.example.com": false, "a.example.com/x": false,
		"-a.example.com": false, "A.example.com": false, "a b.example.com": false,
	} {
		c := Config{Tunnels: Tunnels{CloudflareHostname: h}}
		if err := c.Validate(); (err == nil) != ok {
			t.Errorf("%q: %v", h, err)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tunnel/ ./internal/config/ 2>&1 | tail -6`
Expected: build failures, `undefined: Bins`, `undefined: ParseCloudflareToken`, and `unknown field CloudflareHostname`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, change `Tunnels`:

```go
type Tunnels struct {
	CloudflareToken    string `json:"cloudflareToken"`
	CloudflareHostname string `json:"cloudflareHostname"` // shown in Settings; routing is set in the Cloudflare dashboard
	TailscaleEnabled   bool   `json:"tailscaleEnabled"`
}
```

Add `hostRe` to the `var (...)` block next to `idRe`:

```go
	hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
```

At the top of `Validate`, before the camera loop:

```go
	if h := c.Tunnels.CloudflareHostname; h != "" && !hostRe.MatchString(h) {
		return invalid("Cloudflare hostname %q: use a name like camorage.example.com", h)
	}
```

`internal/tunnel/tunnel.go`:

```go
// Package tunnel runs the portal's remote access as supervised children: Cloudflare Tunnel
// (cloudflared) and Tailscale (tailscaled in userspace mode, plus `tailscale serve` for HTTPS).
package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"camorage/internal/supervisor"
)

// CloudflaredMetrics is cloudflared's metrics listener; its /ready answers 200 while the tunnel is
// connected to Cloudflare.
const CloudflaredMetrics = "127.0.0.1:20241"

// Bins says where the tunnel programs are and how to run them.
type Bins struct {
	Cloudflared, Tailscaled, Tailscale string // executables
	Proot                              string // "" runs them directly; Android needs proot (see ResolvConf)
	ResolvConf                         string // bound over /etc/resolv.conf under proot: Android has none (M0)
	StateDir                           string // tailscaled's state and LocalAPI socket
}

// Socket is tailscaled's LocalAPI socket; the tailscale CLI talks to it too.
func (b Bins) Socket() string { return filepath.Join(b.StateDir, "sock") }

// CloudflaredSpec runs the dashboard-managed tunnel the token belongs to. The token goes in the
// environment: command lines are readable by every app on the phone.
func (b Bins) CloudflaredSpec(token string) supervisor.Spec {
	return b.wrap(supervisor.Spec{
		Name: "cloudflared", Path: b.Cloudflared,
		Args: []string{"tunnel", "--no-autoupdate", "--metrics", CloudflaredMetrics, "run"},
		Env:  []string{"TUNNEL_TOKEN=" + token},
	})
}

// TailscaledSpec runs tailscaled without a TUN device (there is no root on the phone).
func (b Bins) TailscaledSpec() supervisor.Spec {
	return b.wrap(supervisor.Spec{
		Name: "tailscaled", Path: b.Tailscaled,
		Args: []string{"--tun=userspace-networking", "--statedir=" + b.StateDir, "--socket=" + b.Socket()},
	})
}

func (b Bins) wrap(s supervisor.Spec) supervisor.Spec {
	if b.Proot == "" {
		return s
	}
	s.Args = append([]string{"-b", b.ResolvConf + ":/etc/resolv.conf", s.Path}, s.Args...)
	s.Path = b.Proot
	return s
}

// RunTailscale runs the tailscale CLI against this tailscaled and returns its combined output.
func (b Bins) RunTailscale(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, b.Tailscale, append([]string{"--socket=" + b.Socket()}, args...)...).CombinedOutput()
}

// ParseCloudflareToken accepts a tunnel token, or the whole "cloudflared service install <token>"
// command the Cloudflare dashboard offers to copy, and returns the token.
func ParseCloudflareToken(s string) (string, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return "", errors.New("paste the tunnel token from the Cloudflare dashboard")
	}
	tok := f[len(f)-1]
	raw := strings.TrimRight(tok, "=")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		b, err = base64.RawURLEncoding.DecodeString(raw)
	}
	var t struct {
		A string `json:"a"` // account
		T string `json:"t"` // tunnel id
		S string `json:"s"` // secret
	}
	if err != nil || json.Unmarshal(b, &t) != nil || t.A == "" || t.T == "" || t.S == "" {
		return "", errors.New(`that is not a Cloudflare tunnel token: copy the long text after "cloudflared service install" on the tunnel's page in Zero Trust → Networks → Tunnels`)
	}
	return tok, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l internal/ ; go test -race ./internal/tunnel/ ./internal/config/`
Expected: two `ok` lines.

- [ ] **Step 5: Amend the spec**

1. §4 data table, `config.json` row: replace `` `tunnels {cloudflareToken, tailscaleEnabled}` `` with `` `tunnels {cloudflareToken, cloudflareHostname, tailscaleEnabled}` ``.
2. §7 **Cloudflare** bullet: replace `` `proot -b <data>/resolv.conf:/etc/resolv.conf cloudflared tunnel --no-autoupdate run --token <token>` `` with:
   `` `proot -b <data>/resolv.conf:/etc/resolv.conf cloudflared tunnel --no-autoupdate --metrics 127.0.0.1:20241 run` with the token in `TUNNEL_TOKEN` (never argv, which every app can read; the dashboard's whole "cloudflared service install <token>" command is accepted too; `GET 127.0.0.1:20241/ready` = 200 while connected) ``

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/ internal/config/ docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "feat(tunnel): cloudflared/tailscaled process specs (proot on Android), token parsing, hostname setting

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Tunnel manager (start/stop, Tailscale sign-in and serve, live status)

**Files:**
- Create: `internal/tunnel/manager.go`
- Test: `internal/tunnel/manager_test.go`
- Modify: the spec (§7 Tailscale bullet)

**Interfaces:**
- Consumes:
  - `Bins`, `CloudflaredSpec`, `TailscaledSpec`, `RunTailscale`, `Socket` (Task 5).
  - `config.Tunnels` (Task 5).
  - `supervisor.Spec`.
- Produces (Tasks 7 and 9 use these):

```go
type Supervisor interface { Start(supervisor.Spec); Stop(name string) } // *supervisor.Supervisor
type Manager struct {
	Bins        Bins
	Sup         Supervisor
	Hostname    string                                                    // "camorage"
	ServeTarget string                                                    // "http://127.0.0.1:8080"
	ReadyURL    string                                                    // "http://" + CloudflaredMetrics + "/ready"
	OnIP        func(ip string)                                           // Tailscale IPv4 changed ("" = off)
	Run         func(ctx context.Context, args ...string) ([]byte, error) // nil = Bins.RunTailscale
}
func (m *Manager) Apply(t config.Tunnels)
func (m *Manager) Poll(ctx context.Context)
func (m *Manager) Status() Status
type Status struct { Cloudflare Cloudflare `json:"cloudflare"`; Tailscale Tailscale `json:"tailscale"` }
type Cloudflare struct { TokenSet bool `json:"tokenSet"`; Hostname string `json:"hostname"`; Connected bool `json:"connected"` }
type Tailscale struct { Enabled bool `json:"enabled"`; State, AuthURL, URL, IP, Error string } // json: state, authUrl, url, ip, error (last four omitempty)
```

- [ ] **Step 1: Write the failing tests** (`internal/tunnel/manager_test.go`)

```go
package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"camorage/internal/config"
	"camorage/internal/supervisor"
)

type fakeSup struct {
	mu  sync.Mutex
	log []string
}

func (f *fakeSup) Start(s supervisor.Spec) { f.add("start " + s.Name) }
func (f *fakeSup) Stop(name string)       { f.add("stop " + name) }
func (f *fakeSup) add(s string)           { f.mu.Lock(); f.log = append(f.log, s); f.mu.Unlock() }
func (f *fakeSup) take() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := strings.Join(f.log, ", ")
	f.log = nil
	return s
}

// fakeTailscaled answers the LocalAPI status on <dir>/sock with whatever set() gave it last.
type fakeTailscaled struct {
	mu   sync.Mutex
	body string
}

func (f *fakeTailscaled) set(body string) { f.mu.Lock(); f.body = body; f.mu.Unlock() }

func startTailscaled(t *testing.T, dir string) *fakeTailscaled {
	t.Helper()
	f := &fakeTailscaled{}
	l, err := net.Listen("unix", filepath.Join(dir, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/status" {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		io.WriteString(w, f.body)
	}))
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return f
}

// rig is a Manager wired to fakes: ran records tailscale CLI calls, ips records OnIP calls, and
// cli gives the CLI's answer.
type rig struct {
	m   *Manager
	sup *fakeSup
	ran []string
	ips []string
	cli func(args string) ([]byte, error)
}

func newRig(t *testing.T) *rig {
	r := &rig{sup: &fakeSup{}, cli: func(string) ([]byte, error) { return nil, nil }}
	r.m = &Manager{
		Bins: Bins{StateDir: t.TempDir()}, Sup: r.sup,
		Hostname: "camorage", ServeTarget: "http://127.0.0.1:8080",
		ReadyURL: "http://127.0.0.1:1/ready", // nothing listens there: not connected
		OnIP:     func(ip string) { r.ips = append(r.ips, ip) },
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			a := strings.Join(args, " ")
			r.ran = append(r.ran, a)
			return r.cli(a)
		},
	}
	return r
}

const running = `{"BackendState":"Running","AuthURL":"","Self":{"DNSName":"camorage-phone.tail0a1b2c.ts.net.","TailscaleIPs":["fd7a:115c:a1e0::3b01:d011","100.64.0.7"]}}`

func TestApplyActsOnlyOnChanges(t *testing.T) {
	r := newRig(t)
	r.m.Apply(config.Tunnels{}) // boot with nothing on: nothing to do
	on := config.Tunnels{CloudflareToken: "tok", CloudflareHostname: "a.example.com", TailscaleEnabled: true}
	r.m.Apply(on)
	on.CloudflareHostname = "b.example.com" // display only: nothing restarts
	r.m.Apply(on)
	on.CloudflareToken = "tok2" // a new token replaces the running cloudflared
	r.m.Apply(on)
	r.m.Apply(config.Tunnels{})
	if got, want := r.sup.take(), "start cloudflared, start tailscaled, start cloudflared, stop cloudflared, stop tailscaled"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPollAsksForSignInLinkOnce(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	ts := startTailscaled(t, r.m.Bins.StateDir)
	r.cli = func(string) ([]byte, error) { // what `tailscale up --timeout` prints on a fresh node
		return []byte("\nTo authenticate, visit:\n\n\thttps://login.tailscale.com/a/abc123\n\ntimeout waiting for Tailscale service to enter a Running state; check health with \"tailscale status\"\n"), errors.New("exit status 1")
	}
	r.m.Apply(config.Tunnels{TailscaleEnabled: true})
	ts.set(`{"BackendState":"NeedsLogin","AuthURL":"","Self":{"DNSName":"","TailscaleIPs":null}}`)
	r.m.Poll(ctx)
	ts.set(`{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/abc123"}`)
	r.m.Poll(ctx)
	if got := strings.Join(r.ran, " | "); got != "up --hostname=camorage --reset --timeout=10s" {
		t.Fatalf("CLI calls: %q", got)
	}
	st := r.m.Status().Tailscale
	if st.State != "NeedsLogin" || st.AuthURL != "https://login.tailscale.com/a/abc123" || st.Error != "" {
		t.Fatalf("%+v", st)
	}
}

func TestPollRunningServesOnceAndReportsIP(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	ts := startTailscaled(t, r.m.Bins.StateDir)
	r.m.Apply(config.Tunnels{TailscaleEnabled: true})
	ts.set(running)
	r.m.Poll(ctx)
	r.m.Poll(ctx)
	if got := strings.Join(r.ran, " | "); got != "serve --bg --yes http://127.0.0.1:8080" {
		t.Fatalf("CLI calls: %q", got)
	}
	st := r.m.Status().Tailscale
	if st.State != "Running" || st.URL != "https://camorage-phone.tail0a1b2c.ts.net/" || st.IP != "100.64.0.7" || st.Error != "" {
		t.Fatalf("%+v", st)
	}
	// A restarting tailscaled has no IP for a moment: MediaMTX must not be restarted for that.
	ts.set(`{"BackendState":"Starting"}`)
	r.m.Poll(ctx)
	ts.set(running)
	r.m.Poll(ctx)
	if got := strings.Join(r.ips, ","); got != "100.64.0.7" {
		t.Fatalf("OnIP calls: %q", got)
	}
	// Turning Tailscale off stops it and withdraws the IP.
	r.m.Apply(config.Tunnels{})
	if got := r.sup.take(); got != "start tailscaled, stop tailscaled" {
		t.Fatalf("supervisor: %q", got)
	}
	if got := strings.Join(r.ips, ","); got != "100.64.0.7," {
		t.Fatalf("OnIP calls: %q", got)
	}
	if st := r.m.Status().Tailscale; st != (Tailscale{}) {
		t.Fatalf("still shown after turning off: %+v", st)
	}
}

func TestPollShowsWhyTailscaleIsNotUp(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.m.Apply(config.Tunnels{TailscaleEnabled: true}) // nothing answers on the socket yet
	r.m.Poll(ctx)
	if st := r.m.Status().Tailscale; st.State != "" || !strings.HasPrefix(st.Error, "waiting for tailscaled: ") {
		t.Fatalf("no tailscaled: %+v", st)
	}
	if len(r.ran) != 0 || len(r.ips) != 0 {
		t.Fatalf("acted without tailscaled: ran %v, OnIP %v", r.ran, r.ips)
	}
	// A missing CLI is reported, not mistaken for the timeout `up` normally ends with.
	ts := startTailscaled(t, r.m.Bins.StateDir)
	ts.set(`{"BackendState":"NeedsLogin"}`)
	r.cli = func(string) ([]byte, error) { return nil, errors.New(`exec: "tailscale": executable file not found in $PATH`) }
	r.m.Poll(ctx)
	if st := r.m.Status().Tailscale; st.Error != `tailscale up: exec: "tailscale": executable file not found in $PATH` {
		t.Fatalf("missing CLI: %+v", st)
	}
}

func TestServeFailureIsShownAndRetried(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	ts := startTailscaled(t, r.m.Bins.StateDir)
	r.m.Apply(config.Tunnels{TailscaleEnabled: true})
	ts.set(running)
	r.cli = func(string) ([]byte, error) {
		return []byte("Serve is not enabled on your tailnet.\nTo enable, visit:\n\n  https://login.tailscale.com/f/serve?node=n1\n"), errors.New("exit status 1")
	}
	r.m.Poll(ctx)
	st := r.m.Status().Tailscale
	if st.Error != "tailscale serve: Serve is not enabled on your tailnet. To enable, visit: https://login.tailscale.com/f/serve?node=n1" || st.URL != "" {
		t.Fatalf("%+v", st)
	}
	r.cli = func(string) ([]byte, error) { return nil, nil }
	r.m.Poll(ctx)
	if st := r.m.Status().Tailscale; st.Error != "" || st.URL != "https://camorage-phone.tail0a1b2c.ts.net/" {
		t.Fatalf("after HTTPS was enabled: %+v", st)
	}
	if len(r.ran) != 2 {
		t.Fatalf("serve tried %d times, want 2", len(r.ran))
	}
}

func TestCloudflareConnected(t *testing.T) {
	ctx := context.Background()
	var code atomic.Int32
	code.Store(http.StatusServiceUnavailable)
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(int(code.Load())) }))
	defer ready.Close()
	r := newRig(t)
	r.m.ReadyURL = ready.URL + "/ready"
	r.m.Poll(ctx)
	if r.m.Status().Cloudflare.Connected {
		t.Fatal("connected without a token")
	}
	r.m.Apply(config.Tunnels{CloudflareToken: "tok"})
	r.m.Poll(ctx)
	if r.m.Status().Cloudflare.Connected {
		t.Fatal("503 counted as connected")
	}
	code.Store(http.StatusOK)
	r.m.Poll(ctx)
	if !r.m.Status().Cloudflare.Connected {
		t.Fatal("200 not counted as connected")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tunnel/ 2>&1 | tail -4`
Expected: build failure, `undefined: Manager`.

- [ ] **Step 3: Implement** (`internal/tunnel/manager.go`)

```go
package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"camorage/internal/config"
	"camorage/internal/supervisor"
)

// Status is what Settings shows about remote access. The web layer fills TokenSet, Hostname and
// Enabled from the saved settings; the manager knows the live part.
type Status struct {
	Cloudflare Cloudflare `json:"cloudflare"`
	Tailscale  Tailscale  `json:"tailscale"`
}

type Cloudflare struct {
	TokenSet  bool   `json:"tokenSet"`
	Hostname  string `json:"hostname"`
	Connected bool   `json:"connected"`
}

type Tailscale struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`             // tailscaled's BackendState: NeedsLogin, Starting, Running, …
	AuthURL string `json:"authUrl,omitempty"` // sign-in link while NeedsLogin
	URL     string `json:"url,omitempty"`     // https://<machine>.<tailnet>.ts.net/ once `serve` is set up
	IP      string `json:"ip,omitempty"`      // the phone's Tailscale IPv4
	Error   string `json:"error,omitempty"`
}

// Supervisor is the part of *supervisor.Supervisor the manager uses.
type Supervisor interface {
	Start(supervisor.Spec)
	Stop(name string)
}

// Manager keeps cloudflared and tailscaled in line with the settings and reports their state.
type Manager struct {
	Bins        Bins
	Sup         Supervisor
	Hostname    string          // Tailscale machine name asked for at sign-in
	ServeTarget string          // the portal as `tailscale serve` reaches it, e.g. http://127.0.0.1:8080
	ReadyURL    string          // cloudflared's readiness check: "http://" + CloudflaredMetrics + "/ready"
	OnIP        func(ip string) // the Tailscale IPv4 changed ("" = Tailscale off); never called under a lock
	// Run runs the tailscale CLI and returns its combined output; nil means Bins.RunTailscale.
	Run func(ctx context.Context, args ...string) ([]byte, error)

	applyMu sync.Mutex // serialises Apply, so starts and stops keep their order
	mu      sync.Mutex // guards the fields below
	applied config.Tunnels
	served  bool   // `tailscale serve` is set up; tailscaled keeps it in its state across restarts
	ip      string // last Tailscale IPv4 passed to OnIP
	status  Status
}

// Apply starts or stops cloudflared and tailscaled to match t. It acts only on what changed since
// the last call (the first call compares with "all off"), so saving the hostname or any other
// setting never restarts the tunnel you may be connected through.
func (m *Manager) Apply(t config.Tunnels) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	prev := m.applied
	m.applied = t
	m.mu.Unlock()
	if t.CloudflareToken != prev.CloudflareToken {
		if t.CloudflareToken != "" {
			m.Sup.Start(m.Bins.CloudflaredSpec(t.CloudflareToken)) // replaces a running one
		} else {
			m.Sup.Stop("cloudflared")
		}
	}
	if t.TailscaleEnabled == prev.TailscaleEnabled {
		return
	}
	if t.TailscaleEnabled {
		m.Sup.Start(m.Bins.TailscaledSpec())
		return
	}
	m.Sup.Stop("tailscaled")
	m.mu.Lock()
	hadIP := m.ip != ""
	m.ip, m.served, m.status.Tailscale = "", false, Tailscale{}
	m.mu.Unlock()
	if hadIP && m.OnIP != nil {
		m.OnIP("")
	}
}

// Status returns the live state last seen by Poll.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Poll refreshes the live state (is cloudflared connected? what is Tailscale doing?) and moves
// Tailscale along: it asks for a sign-in link when one is needed and sets up `tailscale serve`
// once Tailscale is running.
func (m *Manager) Poll(ctx context.Context) {
	m.mu.Lock()
	t, served := m.applied, m.served
	m.mu.Unlock()

	connected := t.CloudflareToken != "" && m.ready(ctx)
	var ts Tailscale
	if t.TailscaleEnabled {
		ts, served = m.pollTailscale(ctx, served)
	}

	m.mu.Lock()
	m.status.Cloudflare.Connected = connected
	notify := false
	if m.applied.TailscaleEnabled && t.TailscaleEnabled { // not turned off while we were asking
		m.status.Tailscale, m.served = ts, served
		if ts.IP != "" && ts.IP != m.ip { // a restarting tailscaled reports no IP: keep the last one
			m.ip, notify = ts.IP, true
		}
	}
	ip := m.ip
	m.mu.Unlock()
	if notify && m.OnIP != nil {
		m.OnIP(ip)
	}
}

func (m *Manager) pollTailscale(ctx context.Context, served bool) (Tailscale, bool) {
	st, err := localStatus(ctx, m.Bins.Socket())
	if err != nil {
		return Tailscale{Error: "waiting for tailscaled: " + err.Error()}, served
	}
	v := Tailscale{State: st.BackendState}
	switch st.BackendState {
	case "NeedsLogin", "Stopped":
		if st.AuthURL == "" || st.BackendState == "Stopped" {
			// `up` makes tailscaled fetch a sign-in link, which stays in its status after the CLI
			// stops waiting (M1c), so a short timeout is enough and its timeout error is expected.
			out, err := m.run(ctx, "up", "--hostname="+m.Hostname, "--reset", "--timeout=10s")
			if err != nil && !bytes.Contains(out, []byte("timeout waiting")) {
				v.Error = "tailscale up: " + brief(out, err)
			}
		}
		if strings.HasPrefix(st.AuthURL, "https://") {
			v.AuthURL = st.AuthURL
		}
	case "Running":
		v.IP = firstIPv4(st.Self.TailscaleIPs)
		if !served {
			if out, err := m.run(ctx, "serve", "--bg", "--yes", m.ServeTarget); err != nil {
				v.Error = "tailscale serve: " + brief(out, err)
			} else {
				served = true
			}
		}
		if dns := strings.TrimSuffix(st.Self.DNSName, "."); served && dns != "" {
			v.URL = "https://" + dns + "/"
		}
	}
	return v, served
}

func (m *Manager) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if m.Run != nil {
		return m.Run(ctx, args...)
	}
	return m.Bins.RunTailscale(ctx, args...)
}

// ready reports whether cloudflared says the tunnel is connected.
func (m *Manager) ready(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ReadyURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// tsStatus is the part of tailscaled's LocalAPI status the portal reads.
type tsStatus struct {
	BackendState string
	AuthURL      string
	Self         struct {
		DNSName      string
		TailscaleIPs []string
	}
}

// localStatus asks tailscaled over its LocalAPI socket: an HTTP request, instead of starting the
// tailscale CLI every few seconds on the phone.
func localStatus(ctx context.Context, socket string) (tsStatus, error) {
	var st tsStatus
	c := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		DisableKeepAlives: true, // a new client per poll must not leave idle connections behind
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://local-tailscaled.sock/localapi/v0/status", nil)
	if err != nil {
		return st, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("LocalAPI status: HTTP %d", resp.StatusCode)
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}

func firstIPv4(ips []string) string {
	for _, s := range ips {
		if a, err := netip.ParseAddr(s); err == nil && a.Is4() {
			return a.String()
		}
	}
	return ""
}

// brief is a CLI's output on one line (at most 300 bytes), or err when it printed nothing.
func brief(out []byte, err error) string {
	s := strings.Join(strings.Fields(string(out)), " ")
	if s == "" {
		return err.Error()
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l internal/ ; go vet ./internal/tunnel/ && go test -race ./internal/tunnel/`
Expected: `ok  camorage/internal/tunnel`.

- [ ] **Step 5: Amend the spec** (§7 Tailscale bullet)

Replace ``login URL surfaced in Settings; `tailscale serve` → `https://<phone>.<tailnet>.ts.net` → `http://127.0.0.1:8080`.`` with:
``login URL surfaced in Settings; `tailscale serve` → `https://<phone>.<tailnet>.ts.net` → `http://127.0.0.1:8080`. The portal reads tailscaled's LocalAPI status (`/localapi/v0/status` on its socket) every 5 s. When a sign-in link is needed it runs `tailscale up --hostname=camorage --reset --timeout=10s` (the link stays in the status after the CLI stops waiting). Once Running, it runs `tailscale serve --bg --yes http://127.0.0.1:8080` and restarts MediaMTX with the Tailscale IPv4 in `webrtcAdditionalHosts`.``

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/ docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "feat(tunnel): manager applies tunnel settings, drives Tailscale sign-in and serve, reports live state

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Tunnels API

**Files:**
- Create: `internal/web/tunnels.go`
- Modify: `internal/web/web.go` (Deps and routes)
- Test: `internal/web/tunnels_test.go`

**Interfaces:**
- Consumes:
  - `tunnel.Status`, `tunnel.Cloudflare`, `tunnel.Tailscale`, `tunnel.ParseCloudflareToken` (Tasks 5 and 6).
  - `config.Tunnels.CloudflareHostname` and `config.ValidationError{Msg}`.
  - From M1b: `storeError`, `readJSON`, `writeJSON`, `fail`, `okBody`, `s.authed`.
  - Test helpers: `newEnv`, `decode[T]`, `noOrigin`.
- Produces (Tasks 8 and 9 use these):
  - `Deps.Tunnels func() tunnel.Status`, which may be nil.
  - `Deps.OnTunnelsChanged func()`, called in a goroutine after every saved change.
  - `GET /api/tunnels` returns a `tunnel.Status` JSON with the saved settings laid over the live state:
    `{"cloudflare":{"tokenSet":bool,"hostname":"…","connected":bool},"tailscale":{"enabled":bool,"state":"…","authUrl":"…","url":"…","ip":"…","error":"…"}}`.
    The token itself never appears.
  - `PUT /api/tunnels/cloudflare` takes `{"token":"…","hostname":"…"}`.
    - An empty `token` keeps the saved one.
    - With no token saved yet, an empty `token` is a 400.
    - The hostname is normalized: lower-cased, scheme and path removed.
  - `DELETE /api/tunnels/cloudflare` clears the token and the hostname.
  - `PUT /api/tunnels/tailscale` takes `{"enabled":bool}`.
  - All four require a session; the mutating ones also pass the Origin check.

- [ ] **Step 1: Write the failing test** (`internal/web/tunnels_test.go`)

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestTunnelsAPI 2>&1 | tail -4`
Expected: build failure, `unknown field Tunnels in struct literal` / `d.Tunnels undefined`.

- [ ] **Step 3: Implement**

In `internal/web/web.go`:
1. Import `"camorage/internal/tunnel"`.
2. Add to `Deps`, after `HLSBase, WebRTCBase`:
   ```go
   	Tunnels                 func() tunnel.Status // live remote-access state (nil in tests that do not need it)
   	OnTunnelsChanged        func()               // apply the tunnel settings (called in a goroutine)
   ```
   Then run `gofmt -w internal/web/web.go` to re-align the struct.
3. Register these routes before `mux.Handle("GET /", uiHandler())`:
   ```go
   	mux.Handle("GET /api/tunnels", s.authed(s.tunnels))
   	mux.Handle("PUT /api/tunnels/cloudflare", s.authed(s.putCloudflare))
   	mux.Handle("DELETE /api/tunnels/cloudflare", s.authed(s.deleteCloudflare))
   	mux.Handle("PUT /api/tunnels/tailscale", s.authed(s.putTailscale))
   ```

`internal/web/tunnels.go`:

```go
package web

import (
	"net/http"
	"strings"

	"camorage/internal/config"
	"camorage/internal/tunnel"
)

// tunnels reports remote access: the saved settings (the token only as "set") over the live
// state from the tunnel manager, which may lag a save by a moment.
func (s *server) tunnels(w http.ResponseWriter, r *http.Request) {
	var st tunnel.Status
	if s.d.Tunnels != nil {
		st = s.d.Tunnels()
	}
	t := s.d.Store.Get().Tunnels
	if t.CloudflareToken == "" {
		st.Cloudflare = tunnel.Cloudflare{}
	}
	st.Cloudflare.TokenSet, st.Cloudflare.Hostname = t.CloudflareToken != "", t.CloudflareHostname
	if !t.TailscaleEnabled {
		st.Tailscale = tunnel.Tailscale{}
	}
	st.Tailscale.Enabled = t.TailscaleEnabled
	writeJSON(w, http.StatusOK, st)
}

// putCloudflare saves the tunnel token (write-only: an empty token keeps the saved one) and the
// public hostname Settings links to.
func (s *server) putCloudflare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Hostname string `json:"hostname"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	token := ""
	if strings.TrimSpace(in.Token) != "" {
		var err error
		if token, err = tunnel.ParseCloudflareToken(in.Token); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		if token != "" {
			c.Tunnels.CloudflareToken = token
		}
		if c.Tunnels.CloudflareToken == "" {
			return &config.ValidationError{Msg: "paste the tunnel token from the Cloudflare dashboard"}
		}
		c.Tunnels.CloudflareHostname = cleanHostname(in.Hostname)
		return nil
	})
	s.savedTunnels(w, err)
}

func (s *server) deleteCloudflare(w http.ResponseWriter, r *http.Request) {
	err := s.d.Store.Update(func(c *config.Config) error {
		c.Tunnels.CloudflareToken, c.Tunnels.CloudflareHostname = "", ""
		return nil
	})
	s.savedTunnels(w, err)
}

func (s *server) putTailscale(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		c.Tunnels.TailscaleEnabled = in.Enabled
		return nil
	})
	s.savedTunnels(w, err)
}

// savedTunnels answers a tunnel settings change and, if it was saved, applies it.
func (s *server) savedTunnels(w http.ResponseWriter, err error) {
	if err != nil {
		storeError(w, err)
		return
	}
	if s.d.OnTunnelsChanged != nil {
		go s.d.OnTunnelsChanged()
	}
	writeJSON(w, http.StatusOK, okBody)
}

// cleanHostname turns what people paste ("https://Cams.Example.com/") into "cams.example.com";
// the config validates the result.
func cleanHostname(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	host, _, _ := strings.Cut(s, "/")
	return host
}
```

- [ ] **Step 4: Run the web tests**

Run: `gofmt -l internal/ ; go vet ./internal/web/ && go test -race ./internal/web/`
Expected: `ok  camorage/internal/web`.

- [ ] **Step 5: Commit**

```bash
git add internal/web/
git commit -m "feat(web): tunnels API (Cloudflare token write-only, hostname, Tailscale on/off, live state)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Settings → Remote access

**Files:**
- Modify: `internal/web/ui/lib.js` (two summary helpers)
- Test: `internal/web/ui/lib.test.mjs`
- Modify: `internal/web/ui/settings.js`

**Interfaces:**
- Consumes: the `GET /api/tunnels` shape and the PUT/DELETE routes (Task 7); `h`, `api`, `showError` from `dom.js`.
- Produces:
  - `tailscaleSummary(ts) → {text, link?, error?}` and `cloudflareSummary(cf) → {text, link?}`.
  - A Settings "Remote access" section. Task 10 checks it in the browser.

- [ ] **Step 1: Write the failing tests**

In `internal/web/ui/lib.test.mjs`, add `tailscaleSummary, cloudflareSummary` to the import list from `./lib.js`, then append:

```js
test('tailscaleSummary names the next step', () => {
  assert.deepEqual(tailscaleSummary({ enabled: false }), { text: 'Off.' });
  assert.equal(tailscaleSummary({ enabled: true, state: 'Running', url: 'https://p.t.ts.net/' }).link, 'https://p.t.ts.net/');
  const s = tailscaleSummary({ enabled: true, state: 'NeedsLogin', authUrl: 'https://login.tailscale.com/a/x' });
  assert.equal(s.link, 'https://login.tailscale.com/a/x');
  assert.match(s.text, /Sign in/);
  assert.match(tailscaleSummary({ enabled: true, state: 'NeedsMachineAuth' }).text, /Approve/);
  assert.deepEqual(tailscaleSummary({ enabled: true, state: '', error: 'waiting for tailscaled: x' }),
    { text: 'waiting for tailscaled: x', error: true });
  assert.deepEqual(tailscaleSummary({ enabled: true, state: 'Starting' }), { text: 'Starting…' });
});

test('cloudflareSummary links the public hostname', () => {
  assert.deepEqual(cloudflareSummary({ tokenSet: false }), { text: 'Off.' });
  assert.deepEqual(cloudflareSummary({ tokenSet: true, hostname: 'cams.example.com', connected: true }),
    { text: 'Connected.', link: 'https://cams.example.com/' });
  const c = cloudflareSummary({ tokenSet: true, hostname: '', connected: false });
  assert.equal(c.link, undefined);
  assert.match(c.text, /Connecting/);
});
```

- [ ] **Step 2: Run them to verify they fail**

Run: `node --test --test-reporter=tap internal/web/ui/lib.test.mjs 2>&1 | grep -E '^not ok|SyntaxError|does not provide' | head -3`
Expected: a failure naming `tailscaleSummary`, e.g. `The requested module './lib.js' does not provide an export named 'cloudflareSummary'`.

- [ ] **Step 3: Implement the helpers** (append to `internal/web/ui/lib.js`)

```js
// tailscaleSummary turns the tailscale part of GET /api/tunnels into one status line, with the
// link to follow when there is one.
export function tailscaleSummary(ts) {
  if (!ts.enabled) return { text: 'Off.' };
  if (ts.url) return { text: 'On. The portal on your tailnet:', link: ts.url };
  if (ts.state === 'NeedsLogin' && ts.authUrl) {
    return { text: 'Sign in to add this phone to your tailnet (link expired? turn Tailscale off and on):', link: ts.authUrl };
  }
  if (ts.state === 'NeedsMachineAuth') return { text: 'Approve this phone in the Tailscale admin console.' };
  if (ts.error) return { text: ts.error, error: true };
  if (ts.state === 'Running') return { text: 'On, but with no HTTPS name: turn on MagicDNS and HTTPS in the Tailscale admin console.' };
  return { text: 'Starting…' };
}

// cloudflareSummary does the same for the cloudflare part.
export function cloudflareSummary(cf) {
  if (!cf.tokenSet) return { text: 'Off.' };
  const link = cf.hostname ? `https://${cf.hostname}/` : undefined;
  if (cf.connected) return { text: 'Connected.', link };
  return { text: 'Connecting… If this stays, check the token and the tunnel’s public hostname in Cloudflare.', link };
}
```

- [ ] **Step 4: Run the lib tests**

Run: `node --test --test-reporter=tap internal/web/ui/lib.test.mjs 2>&1 | grep -E '^# (pass|fail)'`
Expected: `# pass 10` and `# fail 0`.

- [ ] **Step 5: Replace `internal/web/ui/settings.js`**

```js
import { h, api, showError } from './dom.js';
import { cloudflareSummary, tailscaleSummary } from './lib.js';

const TOKEN_HINT = 'eyJhIjoi… or the whole "cloudflared service install …" command';

// renderSettings shows phone health, remote access (Tailscale, Cloudflare Tunnel) and the
// background programs, and lets you change the admin password or sign out.
export async function renderSettings(root) {
  const healthBox = h('div', { class: 'card' });
  const procBox = h('div', { class: 'card' });

  // Tailscale: one button; the status line says what to do next.
  let tsOn = false;
  const tsLine = h('p');
  const tsBtn = h('button', {
    onclick: async () => {
      if (tsOn && !confirm('Turn off Tailscale? If you are connected through it, this page stops working.')) return;
      tsBtn.disabled = true;
      try {
        await api('/api/tunnels/tailscale', { method: 'PUT', body: { enabled: !tsOn } });
        await refresh();
      } catch (err) {
        if (err.message !== 'signed out') showError(root, err);
      } finally {
        tsBtn.disabled = false;
      }
    },
  }, 'Turn on Tailscale');
  const tsBox = h('div', { class: 'card' }, h('h4', {}, 'Tailscale'), tsLine,
    h('p', { class: 'muted' }, 'Needs MagicDNS and HTTPS certificates turned on in the Tailscale admin console. '
      + 'The first visit can take ~20 s while the certificate is issued.'),
    tsBtn);

  // Cloudflare Tunnel: the token is write-only; the hostname comes back only as a link.
  let fillHost = true;
  const cfLine = h('p');
  const cfHost = h('input', { type: 'text', placeholder: 'camorage.example.com', autocomplete: 'off', spellcheck: 'false' });
  const cfToken = h('input', { type: 'password', autocomplete: 'off', placeholder: TOKEN_HINT });
  const cfMsg = h('p', { class: 'muted' });
  const cfRemove = h('button', {
    type: 'button',
    class: 'danger',
    hidden: true,
    onclick: async () => {
      if (!confirm('Remove the Cloudflare tunnel? If you are connected through it, this page stops working.')) return;
      try {
        await api('/api/tunnels/cloudflare', { method: 'DELETE' });
        fillHost = true;
        say(cfMsg, 'ok', 'Removed.');
        await refresh();
      } catch (err) {
        say(cfMsg, 'error', err.message);
      }
    },
  }, 'Remove');
  const cfForm = h('form', {
    class: 'card form',
    onsubmit: async (e) => {
      e.preventDefault();
      try {
        await api('/api/tunnels/cloudflare', { method: 'PUT', body: { token: cfToken.value, hostname: cfHost.value } });
        cfToken.value = '';
        fillHost = true;
        say(cfMsg, 'ok', 'Saved. The tunnel connects within a few seconds.');
        await refresh();
      } catch (err) {
        say(cfMsg, 'error', err.message);
      }
    },
  }, h('h4', {}, 'Cloudflare Tunnel'), cfLine,
  h('p', { class: 'muted' }, 'In Cloudflare Zero Trust → Networks → Tunnels: create a tunnel, add a public hostname '
    + 'whose service is HTTP 127.0.0.1:8080, and paste the tunnel token here. Video over Cloudflare uses HLS.'),
  h('label', {}, 'Public hostname', cfHost), h('label', {}, 'Tunnel token', cfToken), cfMsg,
  h('button', { class: 'primary', type: 'submit' }, 'Save'), ' ', cfRemove);

  const cur = h('input', { type: 'password', autocomplete: 'current-password', required: true });
  const next = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const again = h('input', { type: 'password', autocomplete: 'new-password', minlength: 8, required: true });
  const msg = h('p', { class: 'muted' });
  const pwForm = h('form', {
    class: 'card form',
    onsubmit: async (e) => {
      e.preventDefault();
      if (next.value !== again.value) {
        say(msg, 'error', 'The new passwords do not match.');
        return;
      }
      try {
        await api('/api/password', { method: 'POST', body: { current: cur.value, next: next.value } });
        pwForm.reset();
        say(msg, 'ok', 'Password changed. Other browsers have been signed out.');
      } catch (err) {
        say(msg, 'error', err.message);
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
  root.append(h('h3', {}, 'Phone'), healthBox, h('h3', {}, 'Remote access'), tsBox, cfForm,
    h('h3', {}, 'Background programs'), procBox, pwForm, signOut);

  async function refresh() {
    try {
      const [st, tn] = await Promise.all([api('/api/status'), api('/api/tunnels')]);
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

      tsOn = tn.tailscale.enabled;
      tsBtn.textContent = tsOn ? 'Turn off Tailscale' : 'Turn on Tailscale';
      showLine(tsLine, tailscaleSummary(tn.tailscale));
      showLine(cfLine, cloudflareSummary(tn.cloudflare));
      cfRemove.hidden = !tn.cloudflare.tokenSet;
      cfToken.placeholder = tn.cloudflare.tokenSet ? 'saved — paste a new token to replace it' : TOKEN_HINT;
      if (fillHost) { // only after load, save or remove: never while someone is typing
        cfHost.value = tn.cloudflare.hostname;
        fillHost = false;
      }
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

function say(el, cls, text) {
  el.className = cls;
  el.textContent = text;
}

// showLine renders a {text, link, error} summary from lib.js.
function showLine(el, s) {
  el.className = s.error ? 'error' : '';
  el.replaceChildren(s.text, ...(s.link ? [' ', h('a', { href: s.link, target: '_blank', rel: 'noopener' }, s.link)] : []));
}
```

- [ ] **Step 6: Check syntax and run the tests**

Run: `node --check internal/web/ui/settings.js && echo syntax-ok && node --test --test-reporter=tap internal/web/ui/lib.test.mjs 2>&1 | grep -E '^# (pass|fail)' && go test ./internal/web/`
Expected: `syntax-ok`, `# pass 10`, `# fail 0`, `ok  camorage/internal/web` (the UI embed still builds).

- [ ] **Step 7: Commit**

```bash
git add internal/web/ui/
git commit -m "feat(ui): Settings → Remote access (Tailscale on/off with sign-in link, Cloudflare token and hostname)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Wire it into camorage; deploy and end-to-end checks

**Files:**
- Modify: `cmd/camorage/main.go`
- Modify: `scripts/itest.sh`
- Modify: `scripts/deploy.sh`

**Interfaces:**
- Consumes:
  - `platform.CACerts`, `platform.ResolvConf`, `config.WriteFileIfChanged` (Task 3).
  - `tunnel.Bins`, `tunnel.Manager`, `tunnel.CloudflaredMetrics` (Tasks 5 and 6).
  - `web.Deps.Tunnels`, `web.Deps.OnTunnelsChanged` (Task 7).
  - `mediamtx.Config(recDir, cams, record, tailscaleIP)` (M1a).
- Produces the running system:
  - Tunnels start at boot from `config.json`.
  - `Poll` runs every 5 s; `resolv.conf` is re-checked every minute.
  - A new Tailscale IP restarts MediaMTX with `webrtcAdditionalHosts: [ip]`.

- [ ] **Step 1: Add the failing end-to-end checks to `scripts/itest.sh`**

1. Change the camorage start line so the script keeps its PID:
   ```bash
   "$T/camorage" -data "$T/data" -listen 127.0.0.1:18080 > "$T/camorage.log" 2>&1 &
   CAMO=$!
   ```
2. After the line `echo "ok: UI served; HLS playlist through the portal proxy"`, insert:
   ```bash
   # M1c: remote-access API. No cloudflared on this machine: the token is stored, never returned, and a start is attempted.
   TOK=$(printf '{"a":"acct","t":"00000000-0000-4000-8000-000000000000","s":"c2VjcmV0LXZhbHVl"}' | base64 -w0)
   R=$(api PUT /api/tunnels/cloudflare -d "{\"token\":\"cloudflared service install $TOK\",\"hostname\":\"https://Cams.Example.com/\"}"); [[ $R == *'"ok":true'* ]] || fail "save Cloudflare token: $R"
   R=$(api GET /api/tunnels); [[ $R == *'"tokenSet":true'* && $R == *'"hostname":"cams.example.com"'* && $R != *"$TOK"* ]] || fail "tunnels: $R"
   for _ in $(seq 1 20); do [[ $(api GET /api/status) == *'"name":"cloudflared"'* ]] && break; sleep 0.5; done
   [[ $(api GET /api/status) == *'"name":"cloudflared"'* ]] || fail "cloudflared was not started"
   R=$(api DELETE /api/tunnels/cloudflare); [[ $R == *'"ok":true'* ]] || fail "remove Cloudflare: $R"
   for _ in $(seq 1 20); do [[ $(api GET /api/status) != *'"name":"cloudflared"'* ]] && break; sleep 0.5; done
   [[ $(api GET /api/status) != *'"name":"cloudflared"'* ]] || fail "cloudflared was not stopped"
   [ "$(curl -s -o /dev/null -w '%{http_code}' -b "$T/jar" "$B/live/hls/test-cam_sub%2F..%2Ftest-cam/index.m3u8")" = 404 ] || fail "an encoded ../ reached MediaMTX"
   echo "ok: tunnels API keeps the token secret, starts and stops cloudflared; encoded ../ refused"
   ```
3. Just before `echo "ITEST PASS"`, insert:
   ```bash
   # M1c: a Tailscale sign-in link from a real tailscaled, and tunnels coming back after a portal
   # restart. Skipped on machines without Tailscale. Contacts Tailscale's login server; the new node
   # is never signed in.
   if command -v tailscaled >/dev/null && command -v tailscale >/dev/null; then
     ln -s "$(command -v tailscaled)" "$T/tailscaled"; ln -s "$(command -v tailscale)" "$T/tailscale"
     R=$(api PUT /api/tunnels/tailscale -d '{"enabled":true}'); [[ $R == *'"ok":true'* ]] || fail "turn on Tailscale: $R"
     for _ in $(seq 1 60); do R=$(api GET /api/tunnels); [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] && break; sleep 1; done
     [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] || fail "no Tailscale sign-in link: $R"
     echo "ok: Tailscale sign-in link shown"
     kill "$CAMO"; wait "$CAMO" 2>/dev/null || true
     [ "$(pgrep -fc "[t]ailscaled --tun=userspace-networking --statedir=$T")" = 0 ] || fail "tailscaled outlived the portal"
     "$T/camorage" -data "$T/data" -listen 127.0.0.1:18080 >> "$T/camorage.log" 2>&1 &
     CAMO=$!
     for _ in $(seq 1 60); do R=$(api GET /api/tunnels 2>/dev/null); [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] && break; sleep 1; done
     [[ $R == *'"authUrl":"https://login.tailscale.com/'* ]] || fail "Tailscale did not come back after a restart: $R"
     echo "ok: a portal restart stops tailscaled and starts it again"
   else
     echo "skip: no tailscaled on this machine"
   fi
   ```

- [ ] **Step 2: Run the itest to verify it fails**

Run: `scripts/itest.sh 2>&1 | tail -8`
Expected, after about a minute: `ITEST FAIL: cloudflared was not started`. Task 7's API saves the token, but nothing applies it yet: `main` passes no `OnTunnelsChanged`.

- [ ] **Step 3: Wire `cmd/camorage/main.go`**

1. Imports: add `"net"`, `"os/exec"` and `"camorage/internal/tunnel"`.
2. Replace everything from the start of `main` up to and including the line `restartMediaMTX()` with:

```go
func main() {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	binDir := filepath.Dir(exe) // mediamtx, cloudflared, tailscale and tailscaled live next to camorage
	dataDir := flag.String("data", filepath.Join(home, ".camorage"), "data directory")
	listen := flag.String("listen", ":8080", "HTTP listen address")
	mtxBin := flag.String("mediamtx", filepath.Join(binDir, "mediamtx"), "MediaMTX binary")
	flag.Parse()
	_, port, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatalf("-listen %q: %v", *listen, err)
	}

	time.Local = platform.LocalZone() // Go on Android otherwise runs in UTC
	platform.WakeLock()
	tsState := filepath.Join(*dataDir, "tailscale")
	if err := os.MkdirAll(tsState, 0o700); err != nil { // also creates the data dir
		log.Fatal(err)
	}
	// Android 6 trusts no Let's Encrypt root and has no /etc/resolv.conf. cloudflared and tailscaled
	// inherit SSL_CERT_FILE, and proot shows them the generated resolv.conf.
	certs := filepath.Join(*dataDir, "certs.pem")
	if err := config.WriteFileIfChanged(certs, platform.CACerts); err != nil {
		log.Fatal(err)
	}
	os.Setenv("SSL_CERT_FILE", certs)
	resolvConf := filepath.Join(*dataDir, "resolv.conf")
	refreshDNS := func() {
		b := platform.ResolvConf(platform.Getprop("net.dns1"), platform.Getprop("net.dns2"))
		if err := config.WriteFileIfChanged(resolvConf, b); err != nil {
			log.Printf("resolv.conf: %v", err)
		}
	}
	refreshDNS()

	store, err := config.Open(filepath.Join(*dataDir, "config.json"))
	if err != nil {
		log.Fatal(err)
	}
	sup := supervisor.New(filepath.Join(*dataDir, "logs"))
	mtx := mediamtx.NewClient()
	mtxConf := filepath.Join(*dataDir, "mediamtx.yml")

	// ponytail: any camera change rewrites the config and restarts MediaMTX (a ~2 s gap in every
	// stream); move to MediaMTX's per-path API if cameras start changing often.
	var mtxMu sync.Mutex
	tsIP := "" // guarded by mtxMu: the phone's Tailscale IPv4, offered to WebRTC clients on the tailnet
	restartMediaMTX := func() {
		mtxMu.Lock()
		defer mtxMu.Unlock()
		cfg := store.Get()
		if cfg.RecDir == "" {
			return // not set up yet
		}
		b, err := mediamtx.Config(cfg.RecDir, cfg.Cameras, recorder.Desired(cfg.Cameras, time.Now()), tsIP)
		if err == nil {
			err = config.WriteFileAtomic(mtxConf, b)
		}
		if err != nil {
			log.Printf("mediamtx config: %v", err)
			return
		}
		sup.Start(supervisor.Spec{Name: "mediamtx", Path: *mtxBin, Args: []string{mtxConf}})
	}
	restartMediaMTX()

	tunnels := &tunnel.Manager{
		Bins: tunnel.Bins{
			Cloudflared: filepath.Join(binDir, "cloudflared"),
			Tailscaled:  filepath.Join(binDir, "tailscaled"),
			Tailscale:   filepath.Join(binDir, "tailscale"),
			Proot:       prootPath(),
			ResolvConf:  resolvConf,
			StateDir:    tsState,
		},
		Sup:         sup,
		Hostname:    "camorage",
		ServeTarget: "http://127.0.0.1:" + port,
		ReadyURL:    "http://" + tunnel.CloudflaredMetrics + "/ready",
		// ponytail: a new Tailscale IP restarts MediaMTX (~2 s gap), in practice once per start;
		// patch webrtcAdditionalHosts through MediaMTX's API if that gap ever matters.
		OnIP: func(ip string) {
			mtxMu.Lock()
			tsIP = ip
			mtxMu.Unlock()
			restartMediaMTX()
		},
	}
	tunnels.Apply(store.Get().Tunnels)
```

3. After `defer stop()`, add:

```go
	go every(ctx, 5*time.Second, func() { tunnels.Poll(ctx) })
	go every(ctx, time.Minute, refreshDNS) // the phone's DNS servers change with the network
```

4. In `web.New(web.Deps{…})`, add after `Volumes: platform.Volumes, ONVIF: onvif.LAN{},`:

```go
			Tunnels: tunnels.Status, OnTunnelsChanged: func() { tunnels.Apply(store.Get().Tunnels) },
```

5. Add below `every`:

```go
// prootPath is proot when this system has no /etc/resolv.conf (Android): cloudflared and
// tailscaled then run under proot with the portal's resolv.conf in its place (M0). Elsewhere they
// run directly.
func prootPath() string {
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return ""
	}
	p, err := exec.LookPath("proot")
	if err != nil {
		log.Print("no /etc/resolv.conf and no proot: cloudflared and tailscaled cannot resolve names (pkg install proot)")
		return ""
	}
	return p
}
```

- [ ] **Step 4: Update `scripts/deploy.sh`**

1. Replace the line `"${SSH[@]}" 'mkdir -p ~/camorage/bin && { [ -x ~/camorage/bin/mediamtx ] || cp ~/spike/mediamtx ~/camorage/bin/mediamtx; }'` with:
   ```bash
   "${SSH[@]}" 'mkdir -p ~/camorage/bin && cd ~/camorage/bin && for b in mediamtx cloudflared tailscale tailscaled; do [ -x $b ] || cp ~/spike/$b $b; done'
   ```
2. Replace the last line (`curl -fsS "http://$PHONE_IP:8080/api/health" && echo`) with:
   ```bash
   for _ in $(seq 1 20); do curl -fsS "http://$PHONE_IP:8080/api/health" && echo && exit 0; sleep 1; done
   echo "portal did not answer on http://$PHONE_IP:8080" && exit 1
   ```

- [ ] **Step 5: Build, vet, unit tests, then the itest**

Run: `gofmt -l cmd internal ; go vet ./... && go test -race ./... 2>&1 | tail -12 && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o /dev/null ./cmd/camorage && echo armv7-ok`
Expected:
- No gofmt output.
- Every package `ok`, including `camorage/internal/tunnel`.
- `armv7-ok`.

Run: `scripts/itest.sh 2>&1 | tail -14`
Expected:
- The 7 earlier `ok:` lines.
- `ok: tunnels API keeps the token secret, starts and stops cloudflared; encoded ../ refused`.
- `ok: Tailscale sign-in link shown`.
- `ok: a portal restart stops tailscaled and starts it again`.
- `ITEST PASS`.

- [ ] **Step 6: Commit**

```bash
git add cmd/camorage/main.go scripts/itest.sh scripts/deploy.sh
git commit -m "feat: run Cloudflare Tunnel and Tailscale from camorage; e2e checks the tunnels API and Tailscale sign-in

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Phone acceptance: Tailscale, Cloudflare, resources, crash safety

**Files:**
- Modify: the spec (§7 Cloudflare "verified" note, §8 measured numbers)
- Modify: `~/.claude/projects/-home-user-src-camorage/memory/camorage-phone-deployment.md` (status)

**Interfaces:**
- Consumes everything above, plus:
  - The phone: SSH via `spike/env.sh`.
  - `~/spike/{cloudflared,tailscale,tailscaled,ts/,measure.sh}` on the phone.
  - Chrome via the chrome-devtools MCP (`navigate_page`, `fill`, `click`, `take_snapshot`, `evaluate_script`).
- Produces: the acceptance evidence, written into the ledger.

Steps 6 and 7 need the user. **Stop and ask them.** Keep asking short and exact, and never ask for the token in chat.

- [ ] **Step 1: Move the M0 Tailscale node into camorage's data dir (one-time)**

Run: `. spike/env.sh; $P 'pgrep -x tailscaled >/dev/null && echo "a tailscaled is running: stop it first" || { mkdir -p ~/.camorage/tailscale && cp -rn ~/spike/ts/. ~/.camorage/tailscale/ && rm -f ~/.camorage/tailscale/sock && ls ~/.camorage/tailscale; }'`
Expected: `certs derpmap.cached.json files profile-data tailscaled.state`.

- [ ] **Step 2: Deploy**

Run: `scripts/deploy.sh 2>&1 | tail -4 && . spike/env.sh && $P 'ls ~/camorage/bin; cat ~/.camorage/resolv.conf; grep -c BEGIN ~/.camorage/certs.pem'`
Expected:
- `{"ok":true,"setupDone":true}`.
- The bin list shows `camorage cloudflared mediamtx tailscale tailscaled`.
- `resolv.conf` holds three `nameserver` lines (IPv6 router, `192.168.1.1`, `1.1.1.1`).
- `121`.

- [ ] **Step 3: Turn on Tailscale from Settings (browser)**

1. In Chrome, open `http://192.168.1.29:8080/#/settings`. Sign in with the password in `.cache/admin-password` if asked.
2. Take a snapshot and check that the "Remote access" section shows Tailscale `Off.` and Cloudflare `Off.`.
3. Click **Turn on Tailscale**.
4. Poll with `take_snapshot` every ~10 s for up to 90 s.

Expected: the Tailscale line reads `On. The portal on your tailnet: https://camorage-phone.tail0a1b2c.ts.net/`. "Background programs" lists `tailscaled` as running.

Then run: `. spike/env.sh; $P 'grep -A2 webrtcAdditionalHosts ~/.camorage/mediamtx.yml'`
Expected: `"100.64.0.7"`, so MediaMTX was restarted with the Tailscale IP.

- [ ] **Step 4: Use the portal over the tailnet from the laptop**

Run: `curl -sS -m 90 https://camorage-phone.tail0a1b2c.ts.net/api/health`
Expected: `{"ok":true,"setupDone":true}`. Allow ~20 s if the certificate is re-issued.

In Chrome, open `https://camorage-phone.tail0a1b2c.ts.net/`, sign in, then check:
- **Live:** the tiles play and the badges show `● REC`.
- **Full screen:** the mode badge reads `WebRTC`.
- **Playback:** clicking a recorded span plays it.

Also check the session cookie is `Secure` and the direct-IP path works:
- In a fresh tab, open `http://100.64.0.7:8080/` and sign in. The live grid appears (Review Focus 2).

- [ ] **Step 5: Measure the full stack while streaming over the tailnet**

Keep the live grid and one full-screen WebRTC view open over Tailscale, then run:
`. spike/env.sh; $P 'bash ~/spike/measure.sh 60 $(pgrep -x camorage) $(pgrep -x mediamtx) $(pgrep -x tailscaled) $(pgrep -x proot)'`
Expected:
- One line per process, and a `TOTAL`.
- Budget (spec §8): total RSS < 150 MB, and CPU < 100 % of one core (25 % of the phone).
- Record the numbers in the ledger. Over budget is a finding for the final message, not a stop.

- [ ] **Step 6: Tailscale off the LAN (user)**

Ask the user: *"On your phone, turn off Wi-Fi (mobile data only) with the Tailscale app connected. Open https://camorage-phone.tail0a1b2c.ts.net, sign in, and tap a camera. Does the badge in the full-screen view say WebRTC or HLS?"*

Expected: `WebRTC`. The spec §7 M1 acceptance asks for exactly this case. If it says HLS, record it as a finding with the answer. The portal still works.

- [ ] **Step 7: Cloudflare Tunnel (user, then verify)**

Ask the user to do this in the Cloudflare dashboard. Prerequisite: `example.com` is a zone in their Cloudflare account.
1. Zero Trust → Networks → Tunnels → **Create a tunnel** → **Cloudflared** → name it `camorage` → Save.
2. On "Install and run connectors", **copy the command shown** (`cloudflared service install eyJ…`). Do not run it anywhere, and do not paste it in the chat.
3. Next → **Public Hostname:** Subdomain `camorage`, Domain `example.com`, Service Type `HTTP`, URL `127.0.0.1:8080` → Save tunnel.
4. In the portal (`http://192.168.1.29:8080/#/settings`) → Remote access → Cloudflare Tunnel:
   - Public hostname: `cams.example.com`.
   - Tunnel token: paste the copied command.
   - Click **Save**.
5. Optional, recommended: Zero Trust → Access → Applications → add a self-hosted app for `cams.example.com`, with an email one-time-PIN policy for their own address.

When the user says it is done:
- Run: `curl -sS -m 30 https://cams.example.com/api/health`
  Expected: `{"ok":true,"setupDone":true}`. If Access was enabled, expect a `302` to `…cloudflareaccess.com` instead, which is also a pass.
- Run: `curl -sS -b .cache/jar http://192.168.1.29:8080/api/tunnels`
  Expected: `"tokenSet":true,"hostname":"cams.example.com","connected":true`, and no `eyJ` anywhere in the body.

In Chrome, open `https://cams.example.com/`, sign in, and check:
- **Live:** the tiles play over HLS.
- **Full screen:** it shows video; the mode may be `HLS`.
- **Playback:** plays a span.

Record which full-screen mode was used.

- [ ] **Step 8: The token never leaves the phone**

Run: `. spike/env.sh; $P 'ps -A -o args | grep -c "[e]yJ"; stat -c %a ~/.camorage/config.json'`
Expected:
- `0`: the token is on no process's command line.
- `600`.

- [ ] **Step 9: Crash safety under proot (Review Focus 4)**

Run: `. spike/env.sh; $P 'pkill -9 -x camorage; sleep 1; cd ~/camorage && ./start.sh; sleep 20; for p in cloudflared tailscaled mediamtx camorage; do echo "$p $(pgrep -x $p | wc -l)"; done'`
Expected: `cloudflared 1`, `tailscaled 1`, `mediamtx 1`, `camorage 1`. The orphaned groups were killed before the restart.

Then re-run the two health `curl`s from steps 4 and 7.
Expected: both answer `{"ok":true,…}`.

- [ ] **Step 10: Record the results**

1. In the spec, §7 **Cloudflare** bullet: replace `the named-tunnel `run --token` form is verified in M1` with `the named-tunnel form with TUNNEL_TOKEN was verified in M1c at cams.example.com`. If that phrase is gone, append the sentence to the bullet instead.
2. In §8, replace `Not yet measured: tailscaled, the portal, and 3 more camera ingests — M1 measures the full stack.` with the numbers from step 5, e.g.
   `M1c measured the full stack with 1 camera while streaming over Tailscale: camorage X %/Y MB, MediaMTX …, tailscaled …, proot …; TOTAL … % of one core, … MB. 3 more camera ingests are not measured yet.`
3. In the memory file, update the status line:
   - M1c done.
   - Tailscale URL `https://camorage-phone.tail0a1b2c.ts.net`, and Cloudflare `https://cams.example.com`.
   - Tunnels are managed by camorage (state in `~/.camorage/tailscale`); `~/spike/ts` is no longer used.
   - Next: M2 motion.

- [ ] **Step 11: Commit**

```bash
git add docs/superpowers/specs/2026-09-30-camorage-portal-design.md
git commit -m "docs: M1c acceptance on the phone (Tailscale, Cloudflare Tunnel, full-stack resources)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
