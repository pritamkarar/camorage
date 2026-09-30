package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"camorage/internal/config"
)

// masked stands in for stored secrets in responses; sending it back keeps the stored value.
const masked = "********"

func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, has := u.User.Password(); !has {
		return raw
	}
	// url.URL.String() percent-encodes '*' in userinfo, so mask with a placeholder it leaves alone
	// and swap in the stars afterwards (the result still parses back to the masked password).
	u.User = url.UserPassword(u.User.Username(), "MASKED")
	return strings.Replace(u.String(), ":MASKED@", ":"+masked+"@", 1)
}

// unmaskURL restores the stored password when the client echoes the masked one back.
func unmaskURL(incoming, stored string) string {
	in, err := url.Parse(incoming)
	if err != nil || in.User == nil {
		return incoming
	}
	if p, _ := in.User.Password(); p != masked {
		return incoming
	}
	st, err := url.Parse(stored)
	if err != nil || st.User == nil {
		return incoming
	}
	sp, _ := st.User.Password()
	in.User = url.UserPassword(in.User.Username(), sp)
	return in.String()
}

func (s *server) listCameras(w http.ResponseWriter, r *http.Request) {
	cams := s.d.Store.Get().Cameras
	out := make([]config.Camera, 0, len(cams))
	for _, c := range cams {
		c.MainURL, c.SubURL = maskURL(c.MainURL), maskURL(c.SubURL)
		if c.ONVIF.Pass != "" {
			c.ONVIF.Pass = masked
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) addCamera(w http.ResponseWriter, r *http.Request) {
	var in config.Camera
	if !readJSON(w, r, &in) {
		return
	}
	var id string
	err := s.d.Store.Update(func(c *config.Config) error {
		in.ID = c.NewCameraID(in.Name)
		id = in.ID
		in.MotionSince = nil // the portal decides when motion mode started
		if in.Mode == "motion" {
			t := s.d.Now()
			in.MotionSince = &t
		}
		cloudSince(&in, nil, s.d.Now())
		c.Cameras = append(c.Cameras, in)
		return nil
	})
	if err != nil {
		storeError(w, err)
		return
	}
	s.camerasChanged()
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *server) updateCamera(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in config.Camera
	if !readJSON(w, r, &in) {
		return
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		for i, old := range c.Cameras {
			if old.ID != id {
				continue
			}
			in.ID = id
			in.MainURL = unmaskURL(in.MainURL, old.MainURL)
			in.SubURL = unmaskURL(in.SubURL, old.SubURL)
			if in.ONVIF.Pass == masked {
				in.ONVIF.Pass = old.ONVIF.Pass
			}
			// the keep rule applies from the switch to motion mode on (recorder.Janitor)
			switch {
			case in.Mode != "motion":
				in.MotionSince = nil
			case old.Mode == "motion":
				in.MotionSince = old.MotionSince
			default:
				t := s.d.Now()
				in.MotionSince = &t
			}
			cloudSince(&in, &old, s.d.Now())
			c.Cameras[i] = in
			return nil
		}
		return errNotFound
	})
	if err != nil {
		storeError(w, err)
		return
	}
	s.camerasChanged()
	writeJSON(w, http.StatusOK, okBody)
}

func (s *server) deleteCamera(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.d.Store.Update(func(c *config.Config) error {
		for i, cam := range c.Cameras {
			if cam.ID == id {
				c.Cameras = append(c.Cameras[:i], c.Cameras[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	if err != nil {
		storeError(w, err)
		return
	}
	s.camerasChanged() // its recordings age out with DefaultLocalDays
	writeJSON(w, http.StatusOK, okBody)
}

func (s *server) camerasChanged() {
	if s.d.OnCamerasChanged != nil {
		go s.d.OnCamerasChanged()
	}
}

// cloudSince keeps cloud.since while cloud copy stays on the same target in the same mode, and sets
// it to now when cloud copy is switched on, retargeted or the mode changes: footage from before is
// not uploaded (spec §5.5), and after a switch from motion mode its kept minutes, already in the
// cloud as event clips, do not go up again as hours.
func cloudSince(in, old *config.Camera, now time.Time) {
	if in.Cloud == nil {
		return
	}
	in.ApplyDefaults() // an omitted mode is continuous, as stored
	if old != nil && old.Cloud != nil && old.Cloud.TargetID == in.Cloud.TargetID && old.Mode == in.Mode {
		in.Cloud.Since = old.Cloud.Since
		return
	}
	in.Cloud.Since = &now
}

func storeError(w http.ResponseWriter, err error) {
	var ve *config.ValidationError
	switch {
	case errors.As(err, &ve):
		fail(w, http.StatusBadRequest, ve.Msg)
	case errors.Is(err, errNotFound):
		fail(w, http.StatusNotFound, "no such camera")
	default:
		fail(w, http.StatusInternalServerError, err.Error())
	}
}
