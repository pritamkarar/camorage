// Package web serves camorage's JSON API (the UI arrives in M1b).
package web

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"camorage/internal/auth"
	"camorage/internal/config"
	"camorage/internal/mediamtx"
	"camorage/internal/platform"
	"camorage/internal/recorder"
	"camorage/internal/schedule"
	"camorage/internal/supervisor"
	"camorage/internal/tunnel"
)

// MediaMTX is the part of *mediamtx.Client the API uses.
type MediaMTX interface {
	Paths(ctx context.Context) (map[string]mediamtx.PathState, error)
	Record(ctx context.Context, name string) (bool, error)
	Spans(ctx context.Context, path string, from, to time.Time) ([]mediamtx.Span, error)
	VideoURL(path string, start time.Time, dur time.Duration, format string) string
}

type Deps struct {
	Store               *config.Store
	MTX                 MediaMTX
	Processes           func() []supervisor.Status
	Health              func(recDir string) platform.Health
	Zone                *time.Location
	Limiter             *auth.Limiter
	Now                 func() time.Time
	OnSetup             func() // recordings dir chosen: start MediaMTX (called in a goroutine)
	OnCamerasChanged    func() // regenerate MediaMTX config and restart it (called in a goroutine)
	HTTP                *http.Client
	Volumes             func() []platform.Volume                    // first-run recording folder choices
	ONVIF               ONVIF                                       // camera discovery backend (onvif.LAN{} in production)
	HLSBase, WebRTCBase string                                      // MediaMTX's loopback HLS and WebRTC servers
	Tunnels             func() tunnel.Status                        // live remote-access state (nil in tests that do not need it)
	OnTunnelsChanged    func()                                      // apply the tunnel settings (called in a goroutine)
	Motion              Motion                                      // motion events and state (nil in tests that do not need it)
	Cloud               Cloud                                       // cloud storage (nil in tests that do not need it)
	Usage               func(recDir string) (recorder.Usage, error) // what recordings take (nil: unknown)
	Version             string                                      // camorage's version (release builds set it with -ldflags -X main.version)
}

const (
	internetMaxFails = 20 // failed sign-ins from the internet, all addresses together, per 15 minutes
	sessionTTL       = 30 * 24 * time.Hour
	maxBody          = 64 << 10
)

var (
	errAlreadySetUp = errors.New("already set up")
	errNotFound     = errors.New("not found")
	okBody          = map[string]bool{"ok": true}
)

type server struct {
	d Deps
	// authMu makes lockout check → argon2 → failure count atomic, so parallel guesses cannot all
	// slip past the limiter, and caps argon2's 19 MiB to one at a time on a 2 GB phone.
	// ponytail: a flood of guesses from many IPs queues the owner's login behind them; per-IP
	// queues if that ever matters.
	authMu sync.Mutex
	// internet caps failed sign-ins through Cloudflare as a whole, so rotating addresses cannot
	// out-guess the per-address lockout. Tailscale and the home network are not counted.
	internet *auth.Limiter
	signMu   sync.Mutex
	signIns  map[string]signIn // Drive sign-ins in progress, by OAuth state
	addMu    sync.Mutex        // one storage target added at a time (ids are chosen before the check)
}

func New(d Deps) http.Handler {
	internet := auth.NewLimiter()
	internet.Max, internet.Now = internetMaxFails, d.Now
	s := &server{d: d, internet: internet}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/setup", s.sameOrigin(s.setup))
	mux.HandleFunc("POST /api/login", s.sameOrigin(s.login))
	mux.HandleFunc("POST /api/logout", s.sameOrigin(s.logout))
	mux.Handle("GET /api/status", s.authed(s.status))
	mux.Handle("GET /api/cameras", s.authed(s.listCameras))
	mux.Handle("POST /api/cameras", s.authed(s.addCamera))
	mux.Handle("PUT /api/cameras/{id}", s.authed(s.updateCamera))
	mux.Handle("DELETE /api/cameras/{id}", s.authed(s.deleteCamera))
	mux.Handle("GET /api/playback/spans", s.authed(s.spans))
	mux.Handle("GET /api/playback/video", s.authed(s.video))
	mux.Handle("GET /api/playback/events", s.authed(s.events))
	mux.Handle("GET /api/playback/cloud-spans", s.authed(s.cloudSpans))
	mux.Handle("GET /api/playback/cloud", s.authed(s.cloudClip))
	mux.HandleFunc("GET /api/setup/volumes", s.volumes)
	mux.Handle("POST /api/password", s.authed(s.changePassword))
	mux.Handle("POST /api/onvif/discover", s.authed(s.onvifDiscover))
	mux.Handle("POST /api/onvif/streams", s.authed(s.onvifStreams))
	mux.Handle("GET /live/hls/{rest...}", s.authed(s.hls))
	mux.Handle("POST /live/whep/{path}", s.authed(s.whepOffer))
	mux.Handle("PATCH /live/whep/{path}/{session}", s.authed(s.whepSession))
	mux.Handle("DELETE /live/whep/{path}/{session}", s.authed(s.whepSession))
	mux.Handle("GET /api/tunnels", s.authed(s.tunnels))
	mux.Handle("PUT /api/tunnels/cloudflare", s.authed(s.putCloudflare))
	mux.Handle("DELETE /api/tunnels/cloudflare", s.authed(s.deleteCloudflare))
	mux.Handle("PUT /api/tunnels/tailscale", s.authed(s.putTailscale))
	mux.Handle("GET /api/storage", s.authed(s.storageInfo))
	mux.Handle("POST /api/storage/drive/start", s.authed(s.driveStart))
	mux.Handle("POST /api/storage/drive/finish", s.authed(s.driveFinish))
	mux.Handle("POST /api/storage/s3", s.authed(s.addS3))
	mux.Handle("POST /api/storage/local", s.authed(s.addLocal))
	mux.Handle("POST /api/storage/{id}/test", s.authed(s.testTarget))
	mux.Handle("DELETE /api/storage/{id}", s.authed(s.deleteTarget))
	mux.Handle("GET /", uiHandler())
	return securityHeaders(mux)
}

func (s *server) sessions() auth.Sessions {
	key, _ := hex.DecodeString(s.d.Store.Get().SessionKey)
	return auth.Sessions{Key: key, TTL: sessionTTL, Now: s.d.Now}
}

func (s *server) sameOrigin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.SameOrigin(r) {
			fail(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		h(w, r)
	}
}

func (s *server) authed(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(auth.CookieName)
		if err != nil || !s.sessions().Valid(c.Value) {
			fail(w, http.StatusUnauthorized, "login required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !auth.SameOrigin(r) {
			fail(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		h(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return false
	}
	return true
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "setupDone": s.d.Store.Get().Admin.Hash != ""})
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		RecDir   string `json:"recDir"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if s.d.Store.Get().Admin.Hash != "" { // before touching the filesystem; re-checked under the store lock
		fail(w, http.StatusConflict, "already set up")
		return
	}
	if len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if !filepath.IsAbs(in.RecDir) {
		fail(w, http.StatusBadRequest, "recDir must be an absolute path")
		return
	}
	if err := checkWritable(in.RecDir); err != nil {
		fail(w, http.StatusBadRequest, "recDir is not writable: "+err.Error())
		return
	}
	s.authMu.Lock() // one argon2 at a time (memory), same as login
	defer s.authMu.Unlock()
	hash, err := auth.Hash(in.Password)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	err = s.d.Store.Update(func(c *config.Config) error {
		if c.Admin.Hash != "" {
			return errAlreadySetUp
		}
		c.Admin.Hash, c.RecDir = hash, filepath.Clean(in.RecDir)
		return nil
	})
	if errors.Is(err, errAlreadySetUp) {
		fail(w, http.StatusConflict, "already set up")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	auth.SetCookie(w, r, s.sessions().Issue(), sessionTTL)
	if s.d.OnSetup != nil {
		go s.d.OnSetup()
	}
	writeJSON(w, http.StatusOK, okBody)
}

func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".camorage-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	ip, internet := auth.ClientIP(r), auth.FromInternet(r)
	if !s.d.Limiter.Allowed(ip) {
		fail(w, http.StatusTooManyRequests, "too many failed logins; try again in 15 minutes")
		return
	}
	if internet && !s.internet.Allowed("internet") {
		fail(w, http.StatusTooManyRequests, "too many failed logins from the internet; try again in 15 minutes, or sign in over Tailscale or at home")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	hash := s.d.Store.Get().Admin.Hash
	if hash == "" {
		fail(w, http.StatusConflict, "not set up yet")
		return
	}
	if !auth.Verify(in.Password, hash) {
		s.d.Limiter.Fail(ip)
		if internet {
			s.internet.Fail("internet")
		}
		fail(w, http.StatusUnauthorized, "wrong password")
		return
	}
	s.d.Limiter.Reset(ip)
	auth.SetCookie(w, r, s.sessions().Issue(), sessionTTL)
	writeJSON(w, http.StatusOK, okBody)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearCookie(w, r)
	writeJSON(w, http.StatusOK, okBody)
}

type camStatus struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Available      bool   `json:"available"`
	Recording      bool   `json:"recording"`
	Motion         bool   `json:"motion"`
	ScheduleActive bool   `json:"scheduleActive"`
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	cfg := s.d.Store.Get()
	ctx := r.Context()
	paths, pathsErr := s.d.MTX.Paths(ctx)
	now := s.d.Now().In(s.d.Zone)
	cams := []camStatus{}
	for _, c := range cfg.Cameras {
		cs := camStatus{ID: c.ID, Name: c.Name, Enabled: c.Enabled, ScheduleActive: c.Enabled && schedule.Active(c.Schedule, now)}
		if c.Enabled && pathsErr == nil {
			cs.Available = paths[c.ID].Available
			cs.Recording, _ = s.d.MTX.Record(ctx, c.ID)
			cs.Motion = s.d.Motion != nil && s.d.Motion.Active(c.ID)
		}
		cams = append(cams, cs)
	}
	out := map[string]any{"time": now.Format(time.RFC3339), "cameras": cams, "health": s.d.Health(cfg.RecDir), "processes": s.d.Processes(), "version": s.d.Version}
	if pathsErr != nil {
		out["mediamtxError"] = pathsErr.Error()
	}
	writeJSON(w, http.StatusOK, out)
}
