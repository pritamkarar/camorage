// Package config holds camorage's settings: one JSON file owned by the portal.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CurrentVersion is the config schema version this build writes.
const CurrentVersion = 1

// DefaultLocalDays applies to cameras without localDays and to footage of deleted cameras.
const DefaultLocalDays = 1

// reservedID is MediaMTX's wildcard path name; a camera with this id makes MediaMTX reject the
// whole config, which would stop every camera.
const reservedID = "all"

// maskBlocks is the size of an ignore mask: the 16×9 block grid of internal/motion.
const maskBlocks = 16 * 9

// Window is a weekly recording window in phone local time.
type Window struct {
	Days  []int  `json:"days"`  // ISO weekdays: 1 = Monday … 7 = Sunday
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"; End <= Start crosses midnight and belongs to the start day
}

type ONVIF struct {
	XAddr string `json:"xaddr"`
	User  string `json:"user"`
	Pass  string `json:"pass"`
}

// Motion settings for phone-side detection (spec §5.2).
type Motion struct {
	Source      string `json:"source"`      // "phone" | "onvif"
	Sensitivity string `json:"sensitivity"` // "low" | "medium" | "high"
	Ignore      []bool `json:"ignore"`      // 16×9 block mask, row-major
	PreRollSec  int    `json:"preRollSec"`
	PostRollSec int    `json:"postRollSec"`
}

// Cloud copy settings of a camera (spec §5.5).
type Cloud struct {
	TargetID string `json:"targetId"`
	Days     int    `json:"days"`
	// Since is when cloud copy was switched on or pointed at another target (set by the portal):
	// older footage is not uploaded, so switching it on never floods the uplink with a day of video.
	Since *time.Time `json:"since,omitempty"`
}

type Camera struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	MainURL  string   `json:"mainUrl"`
	SubURL   string   `json:"subUrl"`
	ONVIF    ONVIF    `json:"onvif"`
	Mode     string   `json:"mode"` // "continuous" | "motion"
	Schedule []Window `json:"schedule"`
	Motion   Motion   `json:"motion"`
	// MotionSince is when the camera switched to motion mode (set by the portal, not the client):
	// footage from before it is kept like continuous recordings.
	MotionSince *time.Time `json:"motionSince,omitempty"`
	LocalDays   int        `json:"localDays"`
	Cloud       *Cloud     `json:"cloud"`
}

// StorageTarget is a cloud destination; its credentials live only in <data>/rclone.conf, in the
// section named after its ID.
type StorageTarget struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`   // "drive" | "s3" (AWS, Backblaze B2, Cloudflare R2, Wasabi …) | "local" (a folder; tests)
	Remote string `json:"remote"` // rclone root: "<id>:" (drive), "<id>:<bucket>" (s3), "<id>:<dir>" (local)
}

type Tunnels struct {
	CloudflareToken    string `json:"cloudflareToken"`
	CloudflareHostname string `json:"cloudflareHostname"` // shown in Settings; routing is set in the Cloudflare dashboard
	TailscaleEnabled   bool   `json:"tailscaleEnabled"`
}

type Admin struct {
	Hash string `json:"hash"` // argon2id; empty until first-run setup
}

type Config struct {
	Version        int             `json:"version"`
	Admin          Admin           `json:"admin"`
	SessionKey     string          `json:"sessionKey"` // hex-encoded 32 bytes
	RecDir         string          `json:"recDir"`     // empty until first-run setup
	Cameras        []Camera        `json:"cameras"`
	StorageTargets []StorageTarget `json:"storageTargets"`
	Tunnels        Tunnels         `json:"tunnels"`
}

// ValidationError is a user-fixable problem with submitted settings (HTTP 400).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

var (
	idRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	hhmmRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

// ApplyDefaults fills zero values that have a sensible default.
func (c *Camera) ApplyDefaults() {
	if c.Mode == "" {
		c.Mode = "continuous"
	}
	if c.LocalDays == 0 {
		c.LocalDays = DefaultLocalDays
	}
	if c.Motion.Source == "" {
		c.Motion.Source = "phone"
	}
	if c.Motion.Sensitivity == "" {
		c.Motion.Sensitivity = "medium"
	}
	if c.Motion.PreRollSec == 0 {
		c.Motion.PreRollSec = 10
	}
	if c.Motion.PostRollSec == 0 {
		c.Motion.PostRollSec = 30
	}
}

func validRTSP(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return invalid("%s: %v (percent-encode special characters in the password, e.g. %%23 for # and %%2F for /)", field, err)
	}
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return invalid("%s: must start with rtsp:// or rtsps://", field)
	}
	if u.User == nil && strings.Contains(raw, "@") {
		// '#', '/' or '?' in a password end the host part early ("admin:12#34@cam" parses as host
		// admin, port 12): the camera would never connect and the password could not be masked.
		return invalid("%s: special characters in the user name or password must be percent-encoded (%%23 for #, %%2F for /, %%3F for ?)", field)
	}
	if u.Hostname() == "" {
		return invalid("%s: missing host", field)
	}
	return nil
}

// Validate reports the first problem as a *ValidationError.
func (c *Config) Validate() error {
	if h := c.Tunnels.CloudflareHostname; h != "" && !hostRe.MatchString(h) {
		return invalid("Cloudflare hostname %q: use a name like camorage.example.com", h)
	}
	targets := map[string]bool{}
	for _, t := range c.StorageTargets {
		switch {
		case !idRe.MatchString(t.ID):
			return invalid("storage %q: use 1-32 lowercase letters, digits or '-'", t.ID)
		case targets[t.ID]:
			return invalid("storage %q is used twice", t.ID)
		case t.Name == "":
			return invalid("storage %s: name is required", t.ID)
		case t.Type != "drive" && t.Type != "s3" && t.Type != "local":
			return invalid("storage %s: type must be drive, s3 or local", t.ID)
		case !strings.HasPrefix(t.Remote, t.ID+":"):
			return invalid("storage %s: remote must start with %q", t.ID, t.ID+":")
		}
		targets[t.ID] = true
	}
	seen := map[string]bool{}
	for _, cam := range c.Cameras {
		if !idRe.MatchString(cam.ID) {
			return invalid("camera id %q: use 1-32 lowercase letters, digits or '-'", cam.ID)
		}
		if cam.ID == reservedID {
			return invalid("camera id %q is reserved by MediaMTX; choose another", cam.ID)
		}
		if seen[cam.ID] {
			return invalid("camera id %q is used twice", cam.ID)
		}
		seen[cam.ID] = true
		if cam.Name == "" {
			return invalid("camera %s: name is required", cam.ID)
		}
		if err := validRTSP("mainUrl", cam.MainURL); err != nil {
			return invalid("camera %s: %v", cam.ID, err)
		}
		if cam.SubURL != "" {
			if err := validRTSP("subUrl", cam.SubURL); err != nil {
				return invalid("camera %s: %v", cam.ID, err)
			}
		}
		if cam.Mode != "continuous" && cam.Mode != "motion" {
			return invalid("camera %s: mode must be continuous or motion", cam.ID)
		}
		m := cam.Motion
		if m.Source != "phone" {
			return invalid("camera %s: motion source must be phone (camera ONVIF motion events are not supported yet)", cam.ID)
		}
		if m.Sensitivity != "low" && m.Sensitivity != "medium" && m.Sensitivity != "high" {
			return invalid("camera %s: motion sensitivity must be low, medium or high", cam.ID)
		}
		if n := len(m.Ignore); n != 0 && n != maskBlocks {
			return invalid("camera %s: the ignore mask must have %d blocks (16×9), got %d", cam.ID, maskBlocks, n)
		}
		if m.PreRollSec < 1 || m.PreRollSec > 120 {
			return invalid("camera %s: seconds kept before motion must be 1-120", cam.ID)
		}
		if m.PostRollSec < 1 || m.PostRollSec > 600 {
			return invalid("camera %s: seconds kept after motion must be 1-600", cam.ID)
		}
		if cam.Cloud != nil {
			if !targets[cam.Cloud.TargetID] {
				return invalid("camera %s: cloud copy target %q does not exist", cam.ID, cam.Cloud.TargetID)
			}
			if cam.Cloud.Days < 1 || cam.Cloud.Days > 3650 {
				return invalid("camera %s: days to keep in the cloud must be 1-3650", cam.ID)
			}
		}
		if cam.LocalDays < 1 || cam.LocalDays > 365 {
			return invalid("camera %s: localDays must be 1-365", cam.ID)
		}
		for _, w := range cam.Schedule {
			if len(w.Days) == 0 {
				return invalid("camera %s: a schedule window needs at least one day", cam.ID)
			}
			for _, d := range w.Days {
				if d < 1 || d > 7 {
					return invalid("camera %s: schedule day %d must be 1 (Mon) to 7 (Sun)", cam.ID, d)
				}
			}
			if !hhmmRe.MatchString(w.Start) || !hhmmRe.MatchString(w.End) {
				return invalid("camera %s: schedule times must be HH:MM", cam.ID)
			}
		}
	}
	return nil
}

// CameraByID returns the camera with id.
func (c *Config) CameraByID(id string) (Camera, bool) {
	for _, cam := range c.Cameras {
		if cam.ID == id {
			return cam, true
		}
	}
	return Camera{}, false
}

// Slug turns a display name into a camera id candidate.
func Slug(name string) string {
	var b []byte
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b = append(b, byte(r))
			dash = false
		} else if !dash && len(b) > 0 {
			b = append(b, '-')
			dash = true
		}
	}
	s := strings.TrimRight(string(b), "-")
	if len(s) > 28 { // leave room for a "-NN" suffix within 32
		s = strings.TrimRight(s[:28], "-")
	}
	if s == "" {
		s = "cam"
	}
	return s
}

// NewCameraID returns Slug(name), suffixed -2, -3 … until unused.
func (c *Config) NewCameraID(name string) string {
	base := Slug(name)
	id := base
	for n := 2; ; n++ {
		if _, taken := c.CameraByID(id); !taken && id != reservedID {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

// NewTargetID returns Slug(name), suffixed -2, -3 … until no storage target uses it.
func (c *Config) NewTargetID(name string) string {
	base := Slug(name)
	id := base
	for n := 2; ; n++ {
		taken := false
		for _, t := range c.StorageTargets {
			taken = taken || t.ID == id
		}
		if !taken {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

// Store is the only writer of config.json; all access goes through it.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// Open loads path, or creates it with defaults (and a fresh session key) if missing.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		s.cfg = Config{Version: CurrentVersion, SessionKey: hex.EncodeToString(key)}
		if err := s.save(s.cfg); err != nil {
			return nil, err
		}
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.cfg.Version > CurrentVersion {
		return nil, fmt.Errorf("%s: version %d is newer than this camorage understands (%d)", path, s.cfg.Version, CurrentVersion)
	}
	return s, nil
}

// Get returns a deep copy; callers may modify it freely.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.cfg)
}

// Update applies fn to a copy, fills camera defaults, validates, saves atomically, then publishes it.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cfg)
	if err := fn(&next); err != nil {
		return err
	}
	for i := range next.Cameras {
		next.Cameras[i].ApplyDefaults()
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := s.save(next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func (s *Store) save(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(s.path, b)
}

// WriteFileAtomic writes via temp file + fsync + rename (mode 0600), so a crash never leaves a torn file.
func WriteFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WriteFileIfChanged is WriteFileAtomic, skipped when path already holds b, so readers that
// watch the file (Go's resolver re-reads resolv.conf when it changes) are left alone.
func WriteFileIfChanged(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	return WriteFileAtomic(path, b)
}

func clone(c Config) Config {
	b, _ := json.Marshal(c)
	var out Config
	_ = json.Unmarshal(b, &out)
	return out
}
