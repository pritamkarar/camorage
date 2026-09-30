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
// command the Cloudflare dashboard offers to copy, and returns the token in the exact form
// cloudflared accepts.
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
	return base64.StdEncoding.EncodeToString(b), nil // the padded form cloudflared insists on
}
