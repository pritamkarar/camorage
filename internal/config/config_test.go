package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func cam(id string) Camera {
	return Camera{ID: id, Name: "Cam " + id, Enabled: true, MainURL: "rtsp://192.168.1.10/live/ch00_0"}
}

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestOpenCreatesDefaultWithSessionKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if c.Version != CurrentVersion || len(c.SessionKey) != 64 {
		t.Fatalf("got %+v", c)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", st.Mode().Perm())
	}
}

func TestOpenRefusesNewerVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"version":99}`), 0o600)
	if _, err := Open(p); err == nil {
		t.Fatal("expected error for newer config version")
	}
}

func TestUpdatePersistsAndAppliesDefaults(t *testing.T) {
	s, p := openTemp(t)
	if err := s.Update(func(c *Config) error { c.Cameras = append(c.Cameras, cam("cam1")); return nil }); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Get().Cameras
	if len(got) != 1 || got[0].Mode != "continuous" || got[0].LocalDays != 1 || got[0].Motion.PostRollSec != 30 || got[0].Motion.Source != "phone" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	s, _ := openTemp(t)
	_ = s.Update(func(c *Config) error { c.Cameras = []Camera{cam("a")}; return nil })
	c := s.Get()
	c.Cameras[0].Name = "changed"
	if s.Get().Cameras[0].Name == "changed" {
		t.Fatal("Get leaked internal state")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Camera){
		"bad id":                       func(c *Camera) { c.ID = "Bad_ID" },
		"no name":                      func(c *Camera) { c.Name = "" },
		"not rtsp":                     func(c *Camera) { c.MainURL = "http://1.2.3.4/x" },
		"unescaped # in password":      func(c *Camera) { c.MainURL = "rtsp://admin:pa#ss@1.2.3.4/live" },
		"unescaped / in password":      func(c *Camera) { c.MainURL = "rtsp://admin:pa/ss@1.2.3.4/live" },
		"digits then # in password":    func(c *Camera) { c.MainURL = "rtsp://admin:12#34@cam/live" },
		"digits then / in password":    func(c *Camera) { c.MainURL = "rtsp://admin:12/34@cam/live" },
		"digits then ? in password":    func(c *Camera) { c.MainURL = "rtsp://admin:1234?x@cam/live" },
		"empty port, / in password":    func(c *Camera) { c.MainURL = "rtsp://admin::/x@cam/live" },
		"reserved MediaMTX path 'all'": func(c *Camera) { c.ID = "all" },
		"bad sub":                      func(c *Camera) { c.SubURL = "rtsp://" },
		"bad mode":                     func(c *Camera) { c.Mode = "sometimes" },
		"bad day":                      func(c *Camera) { c.Schedule = []Window{{Days: []int{0}, Start: "08:00", End: "09:00"}} },
		"no days":                      func(c *Camera) { c.Schedule = []Window{{Start: "08:00", End: "09:00"}} },
		"bad time":                     func(c *Camera) { c.Schedule = []Window{{Days: []int{1}, Start: "8:00", End: "24:00"}} },
		"local days":                   func(c *Camera) { c.LocalDays = 400 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := openTemp(t)
			err := s.Update(func(c *Config) error { k := cam("cam1"); mutate(&k); c.Cameras = []Camera{k}; return nil })
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want ValidationError, got %v", err)
			}
			if len(s.Get().Cameras) != 0 {
				t.Fatal("invalid config was saved")
			}
		})
	}
}

func TestValidateRejectsDuplicateID(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Update(func(c *Config) error { c.Cameras = []Camera{cam("a"), cam("a")}; return nil }); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestValidateAcceptsAtInPassword(t *testing.T) {
	s, _ := openTemp(t)
	err := s.Update(func(c *Config) error {
		k := cam("cam1")
		k.MainURL = "rtsp://admin:p@ss@1.2.3.4/live"
		c.Cameras = []Camera{k}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUpdatesKeepAll(t *testing.T) {
	s, _ := openTemp(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Update(func(c *Config) error { c.Cameras = append(c.Cameras, cam(fmt.Sprintf("c%d", i))); return nil }); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if n := len(s.Get().Cameras); n != 20 {
		t.Fatalf("got %d cameras, want 20", n)
	}
}

func TestSlugAndNewCameraID(t *testing.T) {
	if got := Slug("Front Gate!"); got != "front-gate" {
		t.Fatalf("Slug = %q", got)
	}
	if got := Slug("!!!"); got != "cam" {
		t.Fatalf("Slug = %q", got)
	}
	c := Config{Cameras: []Camera{{ID: "front-gate"}, {ID: "front-gate-2"}}}
	if got := c.NewCameraID("Front Gate"); got != "front-gate-3" {
		t.Fatalf("NewCameraID = %q", got)
	}
	if got := (&Config{}).NewCameraID("All"); got != "all-2" {
		t.Fatalf("NewCameraID(All) = %q; 'all' is reserved by MediaMTX", got)
	}
}

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

func TestValidateMotionSettings(t *testing.T) {
	base := Camera{ID: "gate", Name: "Gate", Enabled: true, MainURL: "rtsp://10.0.0.2/live", Mode: "motion"}
	base.ApplyDefaults()
	good := base
	good.Motion.Ignore = make([]bool, 144)
	if err := (&Config{Cameras: []Camera{good}}).Validate(); err != nil {
		t.Fatalf("a valid camera was refused: %v", err)
	}
	for name, mod := range map[string]func(*Camera){
		"onvif source":   func(c *Camera) { c.Motion.Source = "onvif" },
		"sensitivity":    func(c *Camera) { c.Motion.Sensitivity = "extreme" },
		"short mask":     func(c *Camera) { c.Motion.Ignore = make([]bool, 10) },
		"long pre-roll":  func(c *Camera) { c.Motion.PreRollSec = 500 },
		"negative pre":   func(c *Camera) { c.Motion.PreRollSec = -1 },
		"long post-roll": func(c *Camera) { c.Motion.PostRollSec = 5000 },
	} {
		c := base
		mod(&c)
		var ve *ValidationError
		if err := (&Config{Cameras: []Camera{c}}).Validate(); !errors.As(err, &ve) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
}

func TestValidateStorage(t *testing.T) {
	cam := Camera{ID: "gate", Name: "Gate", Enabled: true, MainURL: "rtsp://10.0.0.2/live"}
	cam.ApplyDefaults()
	drive := StorageTarget{ID: "gdrive", Name: "Google Drive", Type: "drive", Remote: "gdrive:"}
	ok := Config{StorageTargets: []StorageTarget{drive}, Cameras: []Camera{cam}}
	ok.Cameras[0].Cloud = &Cloud{TargetID: "gdrive", Days: 30}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	for name, mod := range map[string]func(*Config){
		"bad id":         func(c *Config) { c.StorageTargets[0].ID = "G Drive" },
		"no name":        func(c *Config) { c.StorageTargets[0].Name = "" },
		"unknown type":   func(c *Config) { c.StorageTargets[0].Type = "ftp" },
		"foreign remote": func(c *Config) { c.StorageTargets[0].Remote = "other:" },
		"duplicate":      func(c *Config) { c.StorageTargets = append(c.StorageTargets, c.StorageTargets[0]) },
		"missing target": func(c *Config) { c.Cameras[0].Cloud = &Cloud{TargetID: "nope", Days: 30} },
		"zero days":      func(c *Config) { c.Cameras[0].Cloud = &Cloud{TargetID: "gdrive", Days: 0} },
		"too many days":  func(c *Config) { c.Cameras[0].Cloud = &Cloud{TargetID: "gdrive", Days: 4000} },
	} {
		c := Config{StorageTargets: []StorageTarget{drive}, Cameras: []Camera{cam}}
		c.Cameras[0].Cloud = &Cloud{TargetID: "gdrive", Days: 30}
		mod(&c)
		var ve *ValidationError
		if err := c.Validate(); !errors.As(err, &ve) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
}

func TestNewTargetID(t *testing.T) {
	c := Config{StorageTargets: []StorageTarget{{ID: "google-drive"}}}
	if got := c.NewTargetID("Google Drive"); got != "google-drive-2" {
		t.Fatalf("got %q", got)
	}
	if got := c.NewTargetID("B2 bucket"); got != "b2-bucket" {
		t.Fatalf("got %q", got)
	}
}
