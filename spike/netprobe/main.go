// Throwaway M0 probe: can a GOOS=linux Go binary resolve DNS and verify TLS on Android 6?
// usage: netprobe          (Go defaults)
//        netprobe fixed    (DNS dialer from getprop + embedded CA bundle)
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed certs.pem
var certsPEM []byte

var targets = []string{
	"https://oauth2.googleapis.com/",     // Drive OAuth
	"https://letsencrypt.org/",            // ISRG Root X1 chain (absent from Android 6 store)
	"https://controlplane.tailscale.com/", // Tailscale control plane
	"https://api.cloudflare.com/",         // Cloudflare
}

func dnsServer() string {
	out, err := exec.Command("/system/bin/getprop", "net.dns1").Output()
	if ip := strings.TrimSpace(string(out)); err == nil && net.ParseIP(ip) != nil {
		return net.JoinHostPort(ip, "53")
	}
	return "1.1.1.1:53"
}

func main() {
	client := &http.Client{Timeout: 15 * time.Second}
	if len(os.Args) > 1 && os.Args[1] == "fixed" {
		srv := dnsServer()
		fmt.Println("dns server:", srv)
		net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, srv)
		}}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(certsPEM) {
			fmt.Println("FAIL: no certs parsed from bundle")
			os.Exit(1)
		}
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	}
	for _, u := range targets {
		resp, err := client.Get(u)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", u, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("OK   %s: HTTP %d\n", u, resp.StatusCode)
	}
}
