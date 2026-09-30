package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Drive sign-in with a Desktop OAuth client of the user's own (rclone's shared one is rate-limited,
// spec §2). The user opens AuthURL; after consent Google sends the browser to DriveRedirect, where
// nothing listens, so the page does not load; the user pastes that address back and the portal
// exchanges the code for a token.
const (
	DriveRedirect = "http://127.0.0.1:53682/"
	DriveScope    = "https://www.googleapis.com/auth/drive.file" // only the files the portal creates
)

// Google talks to Google's OAuth endpoints.
type Google struct {
	AuthEndpoint, TokenEndpoint string
	HTTP                        *http.Client
	Now                         func() time.Time
}

func NewGoogle() Google {
	return Google{
		AuthEndpoint:  "https://accounts.google.com/o/oauth2/auth",
		TokenEndpoint: "https://oauth2.googleapis.com/token",
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		Now:           time.Now,
	}
}

// AuthURL is the sign-in link; prompt=consent makes Google always send a refresh token.
func (g Google) AuthURL(clientID, state string) string {
	q := url.Values{
		"access_type": {"offline"}, "prompt": {"consent"}, "response_type": {"code"},
		"client_id": {clientID}, "redirect_uri": {DriveRedirect}, "scope": {DriveScope}, "state": {state},
	}
	return g.AuthEndpoint + "?" + q.Encode()
}

var errPaste = errors.New("paste the whole address of the page that did not load: it starts with " + DriveRedirect)

// CodeFromRedirect takes the address the browser ended on after sign-in and returns its code and
// state.
func CodeFromRedirect(pasted string) (code, state string, err error) {
	s := strings.TrimSpace(pasted)
	i := strings.Index(s, "?")
	if i < 0 {
		return "", "", errPaste
	}
	q, err := url.ParseQuery(s[i+1:])
	if err != nil {
		return "", "", errPaste
	}
	if e := q.Get("error"); e != "" {
		return "", "", fmt.Errorf("Google did not grant access: %s", e)
	}
	if q.Get("code") == "" || q.Get("state") == "" {
		return "", "", errPaste
	}
	return q.Get("code"), q.Get("state"), nil
}

// Exchange trades the code for a token, returned in the form rclone keeps in rclone.conf.
func (g Google) Exchange(ctx context.Context, clientID, secret, code string) (string, error) {
	form := url.Values{
		"client_id": {clientID}, "client_secret": {secret}, "code": {code},
		"grant_type": {"authorization_code"}, "redirect_uri": {DriveRedirect},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach Google: %w", err)
	}
	defer resp.Body.Close()
	var t struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return "", fmt.Errorf("unexpected answer from Google (HTTP %d)", resp.StatusCode)
	}
	if t.Error != "" {
		return "", fmt.Errorf("Google refused the sign-in: %s %s", t.Error, t.Description)
	}
	if t.RefreshToken == "" {
		return "", errors.New("Google sent no refresh token: remove the app's access at https://myaccount.google.com/permissions and sign in again")
	}
	b, _ := json.Marshal(map[string]string{
		"access_token": t.AccessToken, "token_type": t.TokenType, "refresh_token": t.RefreshToken,
		"expiry": g.Now().Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339),
	})
	return string(b), nil
}

// DriveSection is the rclone.conf section of a Drive target. root_folder_id = root is needed by
// rclone < 1.51 with the drive.file scope (the phone has 1.50.1) and is harmless later.
func DriveSection(clientID, secret, token string) map[string]string {
	return map[string]string{
		"type": "drive", "scope": "drive.file", "root_folder_id": "root",
		"client_id": clientID, "client_secret": secret, "token": token,
	}
}

// S3Section is the rclone.conf section of an S3-compatible target: provider "AWS", or "Other"
// with an endpoint (Backblaze B2, Cloudflare R2, Wasabi, MinIO …).
func S3Section(provider, endpoint, region, keyID, secret string) map[string]string {
	kv := map[string]string{"type": "s3", "provider": provider, "access_key_id": keyID, "secret_access_key": secret, "env_auth": "false"}
	if endpoint != "" {
		kv["endpoint"] = endpoint
	}
	if region != "" {
		kv["region"] = region
	}
	return kv
}
