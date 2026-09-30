package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthURL(t *testing.T) {
	u, err := url.Parse(NewGoogle().AuthURL("id-1.apps.googleusercontent.com", "st4te"))
	if err != nil || u.Host != "accounts.google.com" {
		t.Fatalf("%v %v", u, err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"client_id": "id-1.apps.googleusercontent.com", "state": "st4te", "redirect_uri": "http://127.0.0.1:53682/",
		"scope": "https://www.googleapis.com/auth/drive.file", "access_type": "offline", "prompt": "consent", "response_type": "code",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
}

func TestCodeFromRedirect(t *testing.T) {
	code, state, err := CodeFromRedirect("  http://127.0.0.1:53682/?state=st4te&code=4/0Ab-cd&scope=https://www.googleapis.com/auth/drive.file\n")
	if err != nil || code != "4/0Ab-cd" || state != "st4te" {
		t.Fatalf("%q %q %v", code, state, err)
	}
	if _, _, err := CodeFromRedirect("http://127.0.0.1:53682/?error=access_denied&state=st4te"); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("refusal: %v", err)
	}
	for _, bad := range []string{"", "https://accounts.google.com/o/oauth2/auth?client_id=x", "hello"} {
		if _, _, err := CodeFromRedirect(bad); err == nil || !strings.Contains(err.Error(), "127.0.0.1:53682") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestExchange(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm
		switch r.PostForm.Get("code") {
		case "good":
			w.Write([]byte(`{"access_token":"ya29.a","expires_in":3599,"refresh_token":"1//r","scope":"https://www.googleapis.com/auth/drive.file","token_type":"Bearer"}`))
		case "again": // a second sign-in with the same client may come without a refresh token
			w.Write([]byte(`{"access_token":"ya29.b","expires_in":3599,"token_type":"Bearer"}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant","error_description":"Malformed auth code."}`))
		}
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	g := Google{TokenEndpoint: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}

	tok, err := g.Exchange(context.Background(), "cid", "csecret", "good")
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("client_id") != "cid" || form.Get("client_secret") != "csecret" || form.Get("grant_type") != "authorization_code" || form.Get("redirect_uri") != "http://127.0.0.1:53682/" {
		t.Fatalf("form %v", form)
	}
	var got map[string]string
	json.Unmarshal([]byte(tok), &got)
	if got["access_token"] != "ya29.a" || got["refresh_token"] != "1//r" || got["token_type"] != "Bearer" || got["expiry"] != "2026-10-01T09:59:59Z" {
		t.Fatalf("token %s", tok)
	}
	if _, err := g.Exchange(context.Background(), "cid", "csecret", "again"); err == nil || !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("no refresh token: %v", err)
	}
	if _, err := g.Exchange(context.Background(), "cid", "csecret", "stale"); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("refused code: %v", err)
	}
}

func TestTargetSections(t *testing.T) {
	d := DriveSection("cid", "csecret", `{"x":1}`)
	if d["type"] != "drive" || d["scope"] != "drive.file" || d["root_folder_id"] != "root" || d["client_id"] != "cid" || d["client_secret"] != "csecret" || d["token"] != `{"x":1}` {
		t.Fatalf("drive %v", d)
	}
	s := S3Section("Other", "s3.us-west-004.backblazeb2.com", "", "kid", "ksecret")
	if s["type"] != "s3" || s["provider"] != "Other" || s["endpoint"] != "s3.us-west-004.backblazeb2.com" || s["access_key_id"] != "kid" || s["secret_access_key"] != "ksecret" || s["env_auth"] != "false" {
		t.Fatalf("s3 %v", s)
	}
	if _, has := s["region"]; has {
		t.Fatal("empty region written")
	}
}
