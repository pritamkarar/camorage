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
func (f *fakeSup) Stop(name string)        { f.add("stop " + name) }
func (f *fakeSup) add(s string)            { f.mu.Lock(); f.log = append(f.log, s); f.mu.Unlock() }
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
	r.cli = func(string) ([]byte, error) {
		return nil, errors.New(`exec: "tailscale": executable file not found in $PATH`)
	}
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
