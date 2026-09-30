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
