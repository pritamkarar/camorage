package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"camorage/internal/auth"
	"camorage/internal/config"
	"camorage/internal/platform"
)

// volumes lists candidate recording folders. It is unauthenticated, so only before setup.
func (s *server) volumes(w http.ResponseWriter, r *http.Request) {
	if s.d.Store.Get().Admin.Hash != "" {
		fail(w, http.StatusConflict, "already set up")
		return
	}
	vols := []platform.Volume{}
	if s.d.Volumes != nil {
		vols = append(vols, s.d.Volumes()...)
	}
	writeJSON(w, http.StatusOK, vols)
}

// changePassword checks the current password, stores the new one and rotates the session key,
// which signs out every other browser. A wrong current password is 403: the UI reads 401 as
// "signed out".
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Next) < 8 {
		fail(w, http.StatusBadRequest, "the new password must be at least 8 characters")
		return
	}
	ip := auth.ClientIP(r)
	if !s.d.Limiter.Allowed(ip) {
		fail(w, http.StatusTooManyRequests, "too many failed attempts; try again in 15 minutes")
		return
	}
	if !auth.Verify(in.Current, s.d.Store.Get().Admin.Hash) {
		s.d.Limiter.Fail(ip)
		fail(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	hash, err := auth.Hash(in.Next)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.d.Store.Update(func(c *config.Config) error {
		c.Admin.Hash, c.SessionKey = hash, hex.EncodeToString(key)
		return nil
	}); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	auth.SetCookie(w, r, s.sessions().Issue(), sessionTTL)
	writeJSON(w, http.StatusOK, okBody)
}
