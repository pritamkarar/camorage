package web

import (
	"context"
	"errors"
	"net/http"

	"camorage/internal/onvif"
)

// ONVIF is the camera-discovery backend; onvif.LAN{} in production.
type ONVIF interface {
	Discover(ctx context.Context) ([]onvif.Device, error)
	Streams(ctx context.Context, xaddr, user, pass string) ([]onvif.Profile, error)
}

func (s *server) onvifDiscover(w http.ResponseWriter, r *http.Request) {
	devs, err := s.d.ONVIF.Discover(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "discovery: "+err.Error())
		return
	}
	if devs == nil {
		devs = []onvif.Device{}
	}
	writeJSON(w, http.StatusOK, devs)
}

// onvifStreams asks a camera for its streams and returns ready-to-save RTSP URLs with the
// credentials percent-encoded into them.
func (s *server) onvifStreams(w http.ResponseWriter, r *http.Request) {
	var in struct {
		XAddr string `json:"xaddr"`
		User  string `json:"user"`
		Pass  string `json:"pass"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ps, err := s.d.ONVIF.Streams(r.Context(), in.XAddr, in.User, in.Pass)
	if errors.Is(err, onvif.ErrAuth) {
		fail(w, http.StatusBadRequest, "the camera rejected this user name or password")
		return
	}
	if err != nil {
		fail(w, http.StatusBadGateway, "camera: "+err.Error())
		return
	}
	main, sub := onvif.MainAndSub(ps, in.User, in.Pass)
	writeJSON(w, http.StatusOK, map[string]any{"mainUrl": main, "subUrl": sub, "profiles": ps})
}
