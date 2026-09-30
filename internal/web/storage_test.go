package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"camorage/internal/recorder"
	"camorage/internal/storage"
)

type fakeCloud struct {
	mu         sync.Mutex // requests may run at once
	checkDelay time.Duration
	sections   map[string]map[string]string
	checked    []string
	checkErr   error
	exchErr    error
	files      map[string][]byte // cloud path → content
	hang       bool              // List waits until its context ends (an unreachable cloud)
}

func newFakeCloud() *fakeCloud {
	return &fakeCloud{sections: map[string]map[string]string{}, files: map[string][]byte{}}
}
func (f *fakeCloud) Check(_ context.Context, remote string) error {
	time.Sleep(f.checkDelay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, remote)
	return f.checkErr
}
func (f *fakeCloud) SetSection(name string, kv map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sections[name] = kv
	return nil
}
func (f *fakeCloud) RemoveSection(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sections, name)
	return nil
}
func (f *fakeCloud) AuthURL(clientID, state string) string {
	return "https://accounts.example/auth?client_id=" + clientID + "&state=" + state
}
func (f *fakeCloud) Exchange(_ context.Context, clientID, secret, code string) (string, error) {
	if f.exchErr != nil {
		return "", f.exchErr
	}
	return `{"access_token":"at-` + code + `","refresh_token":"rt"}`, nil
}
func (f *fakeCloud) Uploads() map[string]storage.UploadStatus {
	return map[string]storage.UploadStatus{"front-gate": {Queued: 2, LastError: "network is unreachable"}}
}
func (f *fakeCloud) List(ctx context.Context, dir string) ([]storage.File, error) {
	if f.hang {
		<-ctx.Done()
		return nil, errors.New("rclone lsjson: signal: killed")
	}
	var out []storage.File
	for p, b := range f.files {
		if strings.HasPrefix(p, dir+"/") {
			out = append(out, storage.File{Name: strings.TrimPrefix(p, dir+"/"), Size: int64(len(b))})
		}
	}
	return out, nil
}
func (f *fakeCloud) Cat(_ context.Context, path string, offset, count int64) (io.ReadCloser, error) {
	b, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b[offset : offset+count])), nil
}

func storageEnv(t *testing.T) (*env, *fakeCloud) {
	fc := newFakeCloud()
	e := newEnv(t, func(d *Deps) {
		d.Cloud = fc
		d.Usage = func(string) (recorder.Usage, error) { return recorder.Usage{Bytes: 460 << 20, PerDay: 1440 << 20}, nil }
	})
	e.setUp()
	return e, fc
}

// driveSignIn walks the Drive flow and returns the new target's id.
func driveSignIn(t *testing.T, e *env) string {
	t.Helper()
	w := e.do("POST", "/api/storage/drive/start", `{"name":"Google Drive","clientId":"cid.apps.googleusercontent.com","clientSecret":"GOCSPX-secret"}`)
	start := decode[struct {
		AuthURL string `json:"authUrl"`
	}](t, w)
	u, _ := url.Parse(start.AuthURL)
	state := u.Query().Get("state")
	if w.Code != http.StatusOK || len(state) < 16 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	w = e.do("POST", "/api/storage/drive/finish", `{"url":"http://127.0.0.1:53682/?state=`+state+`&code=4/xyz&scope=x"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("finish: %d %s", w.Code, w.Body)
	}
	return decode[struct {
		ID string `json:"id"`
	}](t, w).ID
}

func TestDriveSignIn(t *testing.T) {
	e, fc := storageEnv(t)
	id := driveSignIn(t, e)
	if id != "google-drive" {
		t.Fatalf("id %q", id)
	}
	sec := fc.sections["google-drive"]
	if sec["type"] != "drive" || sec["client_secret"] != "GOCSPX-secret" || sec["token"] != `{"access_token":"at-4/xyz","refresh_token":"rt"}` {
		t.Fatalf("section %v", sec)
	}
	if strings.Join(fc.checked, ",") != "google-drive:" {
		t.Fatalf("checked %v", fc.checked)
	}
	w := e.do("GET", "/api/storage", "")
	body := w.Body.String()
	if strings.Contains(body, "GOCSPX") || strings.Contains(body, "at-4") {
		t.Fatal("secrets in the API")
	}
	st := decode[struct {
		UsedMB   uint64                          `json:"usedMB"`
		PerDayMB uint64                          `json:"perDayMB"`
		Targets  []map[string]string             `json:"targets"`
		Uploads  map[string]storage.UploadStatus `json:"uploads"`
	}](t, w)
	if st.UsedMB != 460 || st.PerDayMB != 1440 || len(st.Targets) != 1 || st.Targets[0]["type"] != "drive" || st.Uploads["front-gate"].Queued != 2 {
		t.Fatalf("storage %+v", st)
	}
	// the sign-in is used up
	if w := e.do("POST", "/api/storage/drive/finish", `{"url":"http://127.0.0.1:53682/?state=whatever&code=4/xyz"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown state: %d", w.Code)
	}
}

func TestDriveSignInFailureLeavesNothing(t *testing.T) {
	e, fc := storageEnv(t)
	fc.checkErr = errors.New("rclone lsd: Failed to lsd: couldn't find root directory ID: googleapi: Error 404")
	w := e.do("POST", "/api/storage/drive/start", `{"name":"Google Drive","clientId":"cid","clientSecret":"sec"}`)
	u, _ := url.Parse(decode[map[string]string](t, w)["authUrl"])
	w = e.do("POST", "/api/storage/drive/finish", `{"url":"http://127.0.0.1:53682/?state=`+u.Query().Get("state")+`&code=4/xyz"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "404") {
		t.Fatalf("finish: %d %s", w.Code, w.Body)
	}
	if len(fc.sections) != 0 || len(e.store.Get().StorageTargets) != 0 {
		t.Fatalf("left behind: %v %v", fc.sections, e.store.Get().StorageTargets)
	}
	if w := e.do("POST", "/api/storage/drive/finish", `{"url":"https://accounts.google.com/o/oauth2/auth?client_id=x"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "127.0.0.1:53682") {
		t.Fatalf("wrong paste: %d %s", w.Code, w.Body)
	}
}

func TestS3AndDeleteTarget(t *testing.T) {
	e, fc := storageEnv(t)
	w := e.do("POST", "/api/storage/s3", `{"name":"B2","provider":"Other","endpoint":"s3.us-west-004.backblazeb2.com","region":"","bucket":"cams","accessKeyId":"kid","secretAccessKey":"ksecret"}`)
	if w.Code != http.StatusOK || fc.sections["b2"]["secret_access_key"] != "ksecret" || strings.Join(fc.checked, ",") != "b2:cams" {
		t.Fatalf("s3: %d %s %v %v", w.Code, w.Body, fc.sections, fc.checked)
	}
	if tg := e.store.Get().StorageTargets; len(tg) != 1 || tg[0].Remote != "b2:cams" {
		t.Fatalf("targets %+v", tg)
	}
	if w := e.do("POST", "/api/storage/s3", `{"name":"B2","provider":"Other","bucket":"","accessKeyId":"k","secretAccessKey":"s"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("no bucket: %d", w.Code)
	}
	e.do("POST", "/api/cameras", strings.Replace(gate, "{", `{"cloud":{"targetId":"b2","days":30},`, 1))
	e.waitChanged()
	if w := e.do("DELETE", "/api/storage/b2", ""); w.Code != http.StatusConflict {
		t.Fatalf("delete a target in use: %d", w.Code)
	}
	e.do("PUT", "/api/cameras/front-gate", gate) // cloud copy off
	e.waitChanged()
	if w := e.do("DELETE", "/api/storage/b2", ""); w.Code != http.StatusOK || len(fc.sections) != 0 || len(e.store.Get().StorageTargets) != 0 {
		t.Fatalf("delete: %d %v", w.Code, fc.sections)
	}
}

func TestCloudSinceFollowsSettings(t *testing.T) {
	e, _ := storageEnv(t)
	driveSignIn(t, e)
	e.do("POST", "/api/storage/local", `{"name":"Folder","dir":"/tmp/cloud"}`)
	e.do("POST", "/api/cameras", gate)
	e.waitChanged()
	cloudOf := func() *time.Time {
		cfg := e.store.Get()
		c, _ := cfg.CameraByID("front-gate")
		if c.Cloud == nil {
			return nil
		}
		return c.Cloud.Since
	}
	on := strings.Replace(gate, "{", `{"cloud":{"targetId":"google-drive","days":30},`, 1)
	e.do("PUT", "/api/cameras/front-gate", on)
	e.waitChanged()
	first := cloudOf()
	if first == nil || !first.Equal(e.now) {
		t.Fatalf("switched on: %v", first)
	}
	e.now = e.now.Add(time.Hour)
	e.do("PUT", "/api/cameras/front-gate", strings.Replace(gate, "{", `{"cloud":{"targetId":"google-drive","days":7,"since":"2020-01-01T00:00:00Z"},`, 1))
	e.waitChanged()
	if s := cloudOf(); s == nil || !s.Equal(*first) {
		t.Fatalf("same target moved since to %v", s)
	}
	e.do("PUT", "/api/cameras/front-gate", strings.Replace(gate, "{", `{"cloud":{"targetId":"folder","days":7},`, 1))
	e.waitChanged()
	if s := cloudOf(); s == nil || !s.Equal(e.now) {
		t.Fatalf("new target: %v", s)
	}
	// a mode change starts cloud copy afresh: motion mode's kept minutes are already in the cloud
	e.now = e.now.Add(time.Hour)
	e.do("PUT", "/api/cameras/front-gate", strings.Replace(gate, "{", `{"mode":"motion","cloud":{"targetId":"folder","days":7},`, 1))
	e.waitChanged()
	if s := cloudOf(); s == nil || !s.Equal(e.now) {
		t.Fatalf("mode switch: since %v", s)
	}
}

func TestAddingTwiceAtOnceKeepsEveryTargetWorking(t *testing.T) {
	e, fc := storageEnv(t)
	fc.checkDelay = 200 * time.Millisecond // a double-click on Add lands while the first check runs
	body := `{"name":"B2","provider":"Other","endpoint":"s3.example.com","bucket":"cams","accessKeyId":"k","secretAccessKey":"s"}`
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); e.do("POST", "/api/storage/s3", body) }()
	}
	wg.Wait()
	targets := e.store.Get().StorageTargets
	if len(targets) == 0 {
		t.Fatal("nothing added")
	}
	for _, tg := range targets {
		if fc.sections[tg.ID] == nil {
			t.Errorf("storage %s has no rclone section: every upload to it would fail", tg.ID)
		}
	}
}
