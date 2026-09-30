package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"camorage/internal/config"
	"camorage/internal/recorder"
	"camorage/internal/storage"
)

// Cloud is the part of the storage package the API uses.
type Cloud interface {
	Check(ctx context.Context, remote string) error
	SetSection(name string, kv map[string]string) error
	RemoveSection(name string) error
	AuthURL(clientID, state string) string
	Exchange(ctx context.Context, clientID, secret, code string) (string, error)
	Uploads() map[string]storage.UploadStatus
	List(ctx context.Context, dir string) ([]storage.File, error)
	Cat(ctx context.Context, path string, offset, count int64) (io.ReadCloser, error)
}

// signIn is a Drive sign-in waiting for the user to paste Google's redirect back.
type signIn struct {
	name, clientID, secret string
	expires                time.Time
}

const signInTTL = 15 * time.Minute

type targetOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// storageInfo reports local usage, the cloud targets (never their secrets) and upload status.
func (s *server) storageInfo(w http.ResponseWriter, r *http.Request) {
	cfg := s.d.Store.Get()
	h := s.d.Health(cfg.RecDir)
	var u recorder.Usage
	if s.d.Usage != nil && cfg.RecDir != "" {
		u, _ = s.d.Usage(cfg.RecDir)
	}
	targets := []targetOut{}
	for _, t := range cfg.StorageTargets {
		targets = append(targets, targetOut{t.ID, t.Name, t.Type})
	}
	uploads := map[string]storage.UploadStatus{}
	if s.d.Cloud != nil {
		uploads = s.d.Cloud.Uploads()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recDir": cfg.RecDir, "usedMB": u.Bytes >> 20, "perDayMB": u.PerDay >> 20,
		"freeMB": h.DiskFreeMB, "totalMB": h.DiskTotalMB,
		"daysFit": recorder.DaysFit(u, h.DiskFreeMB<<20, h.DiskTotalMB<<20),
		"targets": targets, "uploads": uploads,
	})
}

func (s *server) cloud(w http.ResponseWriter) bool {
	if s.d.Cloud == nil {
		fail(w, http.StatusServiceUnavailable, "cloud storage is not available")
		return false
	}
	return true
}

// driveStart remembers a Drive sign-in and returns Google's sign-in link.
func (s *server) driveStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string `json:"name"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if !readJSON(w, r, &in) || !s.cloud(w) {
		return
	}
	in.Name, in.ClientID, in.ClientSecret = strings.TrimSpace(in.Name), strings.TrimSpace(in.ClientID), strings.TrimSpace(in.ClientSecret)
	if in.Name == "" || in.ClientID == "" || in.ClientSecret == "" {
		fail(w, http.StatusBadRequest, "name, client ID and client secret are required")
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)
	s.signMu.Lock()
	if s.signIns == nil {
		s.signIns = map[string]signIn{}
	}
	for k, v := range s.signIns {
		if s.d.Now().After(v.expires) {
			delete(s.signIns, k)
		}
	}
	s.signIns[state] = signIn{in.Name, in.ClientID, in.ClientSecret, s.d.Now().Add(signInTTL)}
	s.signMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"authUrl": s.d.Cloud.AuthURL(in.ClientID, state)})
}

// driveFinish takes the address Google sent the browser to, exchanges its code and adds the target.
func (s *server) driveFinish(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &in) || !s.cloud(w) {
		return
	}
	code, state, err := storage.CodeFromRedirect(in.URL)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.signMu.Lock()
	si, ok := s.signIns[state]
	delete(s.signIns, state)
	s.signMu.Unlock()
	if !ok || s.d.Now().After(si.expires) {
		fail(w, http.StatusBadRequest, "this sign-in has expired or was already used: start again")
		return
	}
	token, err := s.d.Cloud.Exchange(r.Context(), si.clientID, si.secret, code)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.addTarget(w, r, si.name, "drive", func(id string) (string, map[string]string) {
		return id + ":", storage.DriveSection(si.clientID, si.secret, token)
	})
}

func (s *server) addS3(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name            string `json:"name"`
		Provider        string `json:"provider"`
		Endpoint        string `json:"endpoint"`
		Region          string `json:"region"`
		Bucket          string `json:"bucket"`
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
	}
	if !readJSON(w, r, &in) || !s.cloud(w) {
		return
	}
	if in.Provider != "AWS" && in.Provider != "Other" {
		in.Provider = "Other"
	}
	bucket := strings.Trim(strings.TrimSpace(in.Bucket), "/")
	if strings.TrimSpace(in.Name) == "" || bucket == "" || in.AccessKeyID == "" || in.SecretAccessKey == "" {
		fail(w, http.StatusBadRequest, "name, bucket, access key ID and secret access key are required")
		return
	}
	if in.Provider == "Other" && strings.TrimSpace(in.Endpoint) == "" {
		fail(w, http.StatusBadRequest, "an endpoint is required for S3-compatible storage other than Amazon S3")
		return
	}
	s.addTarget(w, r, strings.TrimSpace(in.Name), "s3", func(id string) (string, map[string]string) {
		return id + ":" + bucket, storage.S3Section(in.Provider, strings.TrimSpace(in.Endpoint), strings.TrimSpace(in.Region), strings.TrimSpace(in.AccessKeyID), in.SecretAccessKey)
	})
}

// addLocal adds a folder on the phone as a "cloud" target (the itest's stand-in for Drive).
func (s *server) addLocal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	}
	if !readJSON(w, r, &in) || !s.cloud(w) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || !filepath.IsAbs(in.Dir) {
		fail(w, http.StatusBadRequest, "name and an absolute dir are required")
		return
	}
	s.addTarget(w, r, strings.TrimSpace(in.Name), "local", func(id string) (string, map[string]string) {
		return id + ":" + filepath.Clean(in.Dir), map[string]string{"type": "local"}
	})
}

// addTarget writes the target's rclone section, proves it works, and only then stores the target;
// a failed check removes the section again.
func (s *server) addTarget(w http.ResponseWriter, r *http.Request, name, typ string, build func(id string) (remote string, section map[string]string)) {
	s.addMu.Lock() // a double-click on Add would otherwise pick the same id twice and drop its section
	defer s.addMu.Unlock()
	cfg := s.d.Store.Get()
	id := cfg.NewTargetID(name)
	remote, section := build(id)
	if err := s.d.Cloud.SetSection(id, section); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.d.Cloud.Check(ctx, remote); err != nil {
		s.d.Cloud.RemoveSection(id)
		fail(w, http.StatusBadRequest, "the storage did not work: "+err.Error())
		return
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		c.StorageTargets = append(c.StorageTargets, config.StorageTarget{ID: id, Name: name, Type: typ, Remote: remote})
		return nil
	})
	if err != nil {
		s.d.Cloud.RemoveSection(id)
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *server) target(w http.ResponseWriter, id string) (config.StorageTarget, bool) {
	for _, t := range s.d.Store.Get().StorageTargets {
		if t.ID == id {
			return t, true
		}
	}
	fail(w, http.StatusNotFound, "no such storage")
	return config.StorageTarget{}, false
}

func (s *server) testTarget(w http.ResponseWriter, r *http.Request) {
	t, ok := s.target(w, r.PathValue("id"))
	if !ok || !s.cloud(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.d.Cloud.Check(ctx, t.Remote); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, okBody)
}

var errTargetInUse = errors.New("in use")

// deleteTarget removes a target no camera copies to. Clips already copied there stay.
func (s *server) deleteTarget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.target(w, id); !ok || !s.cloud(w) {
		return
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		for _, cam := range c.Cameras {
			if cam.Cloud != nil && cam.Cloud.TargetID == id {
				return errTargetInUse
			}
		}
		for i, t := range c.StorageTargets {
			if t.ID == id {
				c.StorageTargets = append(c.StorageTargets[:i], c.StorageTargets[i+1:]...)
				break
			}
		}
		return nil
	})
	if errors.Is(err, errTargetInUse) {
		fail(w, http.StatusConflict, "a camera still copies to this storage: switch its cloud copy off first")
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}
	s.d.Cloud.RemoveSection(id)
	writeJSON(w, http.StatusOK, okBody)
}
