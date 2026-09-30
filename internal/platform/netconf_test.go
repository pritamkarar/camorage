package platform

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
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

func TestResolverTakesNameserversInTurn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(p, []byte("nameserver 127.0.0.2\nnameserver 127.0.0.3\n"), 0o600)
	r := Resolver(p)
	dial := func() string {
		t.Helper()
		c, err := r.Dial(context.Background(), "udp", "127.0.0.53:53") // the address Go would pick is ignored
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.RemoteAddr().String()
	}
	if got := []string{dial(), dial(), dial()}; got[0] != "127.0.0.2:53" || got[1] != "127.0.0.3:53" || got[2] != "127.0.0.2:53" {
		t.Fatalf("dialled %v", got)
	}
	// read again for every query: the phone's DNS servers change with the network
	os.WriteFile(p, []byte("nameserver 127.0.0.4\n"), 0o600)
	if got := dial(); got != "127.0.0.4:53" {
		t.Fatalf("after the change: %s", got)
	}
	os.Remove(p)
	if got := dial(); got != "1.1.1.1:53" {
		t.Fatalf("without a file: %s", got)
	}
}
