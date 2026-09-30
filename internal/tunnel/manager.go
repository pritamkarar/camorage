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
