package web

import (
	"net/http"
	"testing"

	"camorage/internal/platform"
)

func TestSetupVolumesOnlyBeforeSetup(t *testing.T) {
	e := newEnv(t, func(d *Deps) {
		d.Volumes = func() []platform.Volume {
			return []platform.Volume{{Path: "/sd/camorage-rec", Label: "SD card X", FreeMB: 5000}}
		}
	})
	w := e.do("GET", "/api/setup/volumes", "")
	vols := decode[[]platform.Volume](t, w)
	if w.Code != http.StatusOK || len(vols) != 1 || vols[0].Label != "SD card X" {
		t.Fatalf("volumes: %d %+v", w.Code, vols)
	}
	e.setUp()
	if w := e.do("GET", "/api/setup/volumes", ""); w.Code != http.StatusConflict {
		t.Fatalf("after setup: %d, want 409", w.Code)
	}
}

func TestChangePasswordRotatesSessions(t *testing.T) {
	e := newEnv(t)
	e.setUp()
	old := e.cookie
	if w := e.do("POST", "/api/password", `{"current":"wrong-horse","next":"new-password-1"}`); w.Code != http.StatusForbidden {
		t.Fatalf("wrong current: %d, want 403 (never 401, which the UI reads as signed out)", w.Code)
	}
	if w := e.do("POST", "/api/password", `{"current":"correct-horse","next":"short"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("short new password: %d", w.Code)
	}
	if w := e.do("POST", "/api/password", `{"current":"correct-horse","next":"new-password-1"}`, noOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", w.Code)
	}
	w := e.do("POST", "/api/password", `{"current":"correct-horse","next":"new-password-1"}`)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) == 0 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	fresh := w.Result().Cookies()[0]

	e.cookie = old
	if w := e.do("GET", "/api/cameras", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("old session still valid: %d", w.Code)
	}
	e.cookie = fresh
	if w := e.do("GET", "/api/cameras", ""); w.Code != http.StatusOK {
		t.Fatalf("fresh session: %d", w.Code)
	}
	e.cookie = nil
	if w := e.do("POST", "/api/login", `{"password":"new-password-1"}`); w.Code != http.StatusOK {
		t.Fatalf("login with new password: %d", w.Code)
	}
}
