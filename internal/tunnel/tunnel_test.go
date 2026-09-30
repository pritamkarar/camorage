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
		// cloudflared decodes strictly (padded standard base64): a double-click that misses the
		// trailing "==" must still give it the token it accepts
		got, err := ParseCloudflareToken(in)
		if err != nil || got != tok {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "hello", "cloudflared service install", token(map[string]string{"a": "acct"})} {
		if _, err := ParseCloudflareToken(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
