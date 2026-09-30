// Package mediamtx generates MediaMTX's config and talks to its control API and playback server.
package mediamtx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"camorage/internal/config"
	"camorage/internal/supervisor"
)

// Loopback-only listeners: the portal is the only client. ICE must be reachable by viewers.
const (
	APIAddr      = "127.0.0.1:9997"
	PlaybackAddr = "127.0.0.1:9996"
	RTSPAddr     = "127.0.0.1:8554"
	HLSAddr      = "127.0.0.1:8888"
	WebRTCAddr   = "127.0.0.1:8889"
	ICEAddr      = ":8189"
)

// ProcessSpec runs MediaMTX with config file conf. TZ=UTC pins the time base of segment names,
// which the portal parses as UTC: MediaMTX uses its local zone, UTC on Android only because
// Termux sets no TZ (M0), while a TZ in the environment would shift the keep rule.
func ProcessSpec(bin, conf string) supervisor.Spec {
	return supervisor.Spec{Name: "mediamtx", Path: bin, Args: []string{conf}, Env: []string{"TZ=UTC"}}
}

// SubPath is the MediaMTX path of a camera's substream.
func SubPath(camID string) string { return camID + "_sub" }

// Config renders MediaMTX's configuration as JSON (valid YAML, so no YAML library is needed).
// record gives each camera's initial record flag; tailscaleIP, if set, is offered to WebRTC clients.
func Config(recDir string, cams []config.Camera, record map[string]bool, tailscaleIP string) ([]byte, error) {
	hosts := []string{}
	if tailscaleIP != "" {
		hosts = append(hosts, tailscaleIP)
	}
	paths := map[string]any{}
	for _, c := range cams {
		if !c.Enabled {
			continue
		}
		paths[c.ID] = map[string]any{"source": c.MainURL, "record": record[c.ID]}
		if c.SubURL != "" {
			paths[SubPath(c.ID)] = map[string]any{"source": c.SubURL, "record": false}
		}
	}
	return json.MarshalIndent(map[string]any{
		"logLevel": "info",
		"api":      true, "apiAddress": APIAddr,
		"playback": true, "playbackAddress": PlaybackAddr,
		"rtspAddress": RTSPAddr, "rtspTransports": []string{"tcp"},
		"rtmp": false, "srt": false, "moq": false,
		"hls": true, "hlsAddress": HLSAddr, "hlsVariant": "fmp4",
		"webrtc": true, "webrtcAddress": WebRTCAddr,
		"webrtcLocalUDPAddress": ICEAddr, "webrtcLocalTCPAddress": ICEAddr,
		"webrtcAdditionalHosts": hosts,
		"pathDefaults": map[string]any{
			"rtspTransport":         "tcp",
			"recordFormat":          "fmp4",
			"recordPartDuration":    "1s",
			"recordSegmentDuration": "1m",
			"recordDeleteAfter":     "0s",
			"recordPath":            filepath.Join(recDir, "%path", "%Y-%m-%d_%H-%M-%S-%f"),
		},
		"paths": paths,
	}, "", "  ")
}

type Client struct {
	API, Playback string // base URLs
	HTTP          *http.Client
}

func NewClient() *Client {
	return &Client{API: "http://" + APIAddr, Playback: "http://" + PlaybackAddr, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

type PathState struct {
	Name         string `json:"name"`
	Available    bool   `json:"available"` // a live stream; `online` is true even for dead sources (M0)
	InboundBytes uint64 `json:"inboundBytes"`
}

type Span struct {
	Start    time.Time // UTC
	Duration time.Duration
}

type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("mediamtx: HTTP %d: %s", e.Code, e.Body) }

func statusErr(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(b))}
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusErr(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Paths returns the live state of every path, keyed by name.
func (c *Client) Paths(ctx context.Context) (map[string]PathState, error) {
	var body struct {
		Items []PathState `json:"items"`
	}
	if err := c.getJSON(ctx, c.API+"/v3/paths/list", &body); err != nil {
		return nil, err
	}
	out := make(map[string]PathState, len(body.Items))
	for _, p := range body.Items {
		out[p.Name] = p
	}
	return out, nil
}

// Record reads a path's configured record flag.
func (c *Client) Record(ctx context.Context, name string) (bool, error) {
	var body struct {
		Record bool `json:"record"`
	}
	err := c.getJSON(ctx, c.API+"/v3/config/paths/get/"+url.PathEscape(name), &body)
	return body.Record, err
}

// SetRecord switches recording of a path on or off at runtime.
func (c *Client) SetRecord(ctx context.Context, name string, on bool) error {
	b, _ := json.Marshal(map[string]bool{"record": on})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.API+"/v3/config/paths/patch/"+url.PathEscape(name), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusErr(resp)
	}
	return nil
}

// Spans lists recorded spans of path overlapping [from, to). No recordings is not an error.
func (c *Client) Spans(ctx context.Context, path string, from, to time.Time) ([]Span, error) {
	q := url.Values{"path": {path}, "start": {from.UTC().Format(time.RFC3339Nano)}, "end": {to.UTC().Format(time.RFC3339Nano)}}
	var raw []struct {
		Start    time.Time `json:"start"`
		Duration float64   `json:"duration"` // seconds
	}
	if err := c.getJSON(ctx, c.Playback+"/list?"+q.Encode(), &raw); err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Span, 0, len(raw))
	for _, r := range raw {
		out = append(out, Span{Start: r.Start, Duration: time.Duration(r.Duration * float64(time.Second))})
	}
	return out, nil
}

// VideoURL is the playback-server URL for [start, start+dur) of path; format "fmp4" or "mp4".
func (c *Client) VideoURL(path string, start time.Time, dur time.Duration, format string) string {
	q := url.Values{
		"path":     {path},
		"start":    {start.UTC().Format(time.RFC3339Nano)},
		"duration": {strconv.FormatFloat(dur.Seconds(), 'f', -1, 64)},
		"format":   {format},
	}
	return c.Playback + "/get?" + q.Encode()
}

// Download saves [start, start+dur) of path as an mp4 file at dst. It stops at the first gap in
// the recording (MediaMTX's /get does): callers ask for one recorded piece at a time.
func (c *Client) Download(ctx context.Context, path string, start time.Time, dur time.Duration, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.VideoURL(path, start, dur, "mp4"), nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: c.HTTP.Transport}).Do(req) // no timeout: an hour takes a while; ctx bounds it
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusErr(resp)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
