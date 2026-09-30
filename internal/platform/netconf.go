package platform

import (
	"context"
	_ "embed"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync/atomic"
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

// Resolver answers DNS through the nameservers listed in resolvConf (the file the portal keeps
// current, see ResolvConf), taking them in turn. Go's own resolver reads /etc/resolv.conf, which
// Android does not have, and then asks localhost, which fails (M0). The file is read again for
// every query; without it, 1.1.1.1 is used.
func Resolver(resolvConf string) *net.Resolver {
	var next atomic.Uint32
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		servers := nameservers(resolvConf)
		if len(servers) == 0 {
			servers = []string{"1.1.1.1"}
		}
		s := servers[int(next.Add(1)-1)%len(servers)]
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
	}}
}

func nameservers(path string) []string {
	b, _ := os.ReadFile(path)
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "nameserver" {
			out = append(out, f[1])
		}
	}
	return out
}
