// Package onvif finds cameras on the LAN (WS-Discovery) and asks them for their RTSP stream URLs.
package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrAuth means the camera rejected the user name or password.
var ErrAuth = errors.New("camera rejected the user name or password")

// Device is a camera that answered a WS-Discovery probe.
type Device struct {
	XAddr    string `json:"xaddr"` // device service URL
	IP       string `json:"ip"`
	Name     string `json:"name"`
	Hardware string `json:"hardware"`
}

// Profile is one media profile of a camera with its RTSP URI (as the camera reports it).
type Profile struct {
	Token    string `json:"token"`
	Encoding string `json:"encoding"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	URI      string `json:"uri"`
}

// LAN is the production backend: a 3-second discovery on the phone's network.
type LAN struct{}

func (LAN) Discover(ctx context.Context) ([]Device, error) { return Discover(ctx, 3*time.Second) }

func (LAN) Streams(ctx context.Context, xaddr, user, pass string) ([]Profile, error) {
	return Streams(ctx, xaddr, user, pass)
}

const probeTarget = "239.255.255.250:3702"

// Discover multicasts a WS-Discovery probe for video transmitters and collects the answers.
func Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	return discover(ctx, probeTarget, timeout)
}

func discover(ctx context.Context, target string, timeout time.Duration) ([]Device, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	dst, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return nil, err
	}
	probe := probeMessage()
	for i := 0; i < 2; i++ { // UDP may drop one
		if _, err := conn.WriteToUDP(probe, dst); err != nil {
			return nil, err
		}
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	seen := map[string]bool{}
	var out []Device
	buf := make([]byte, 64<<10)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // deadline reached
		}
		dev, ok := parseProbeMatch(buf[:n])
		if !ok || seen[dev.XAddr] {
			continue
		}
		seen[dev.XAddr] = true
		dev.IP = from.IP.String()
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

func probeMessage() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">` +
		`<e:Header><w:MessageID>uuid:` + newUUID() + `</w:MessageID><w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>` +
		`<w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header>` +
		`<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`)
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func parseProbeMatch(b []byte) (Device, bool) {
	var env struct {
		Matches []struct {
			XAddrs string `xml:"XAddrs"`
			Scopes string `xml:"Scopes"`
		} `xml:"Body>ProbeMatches>ProbeMatch"`
	}
	if xml.Unmarshal(b, &env) != nil || len(env.Matches) == 0 {
		return Device{}, false
	}
	m := env.Matches[0]
	xaddrs := strings.Fields(m.XAddrs)
	if len(xaddrs) == 0 {
		return Device{}, false
	}
	d := Device{XAddr: xaddrs[0]}
	for _, s := range strings.Fields(m.Scopes) {
		if i := strings.Index(s, "onvif.org/name/"); i >= 0 {
			d.Name, _ = url.PathUnescape(s[i+len("onvif.org/name/"):])
		}
		if i := strings.Index(s, "onvif.org/hardware/"); i >= 0 {
			d.Hardware, _ = url.PathUnescape(s[i+len("onvif.org/hardware/"):])
		}
	}
	return d, true
}

// Streams asks the camera at xaddr (its device-service URL) for the RTSP URI of every profile.
// ponytail: WS-Security digest only; cameras that want HTTP Digest auth need it added here.
func Streams(ctx context.Context, xaddr, user, pass string) ([]Profile, error) {
	u, err := url.Parse(xaddr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("xaddr must be an http(s) URL")
	}
	c := &client{http: &http.Client{Timeout: 8 * time.Second}, user: user, pass: pass}
	c.syncClock(ctx, xaddr)

	var caps struct {
		XAddr string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
	}
	if err := c.call(ctx, xaddr, `<tds:GetCapabilities><tds:Category>Media</tds:Category></tds:GetCapabilities>`, &caps); err != nil {
		return nil, fmt.Errorf("GetCapabilities: %w", err)
	}
	media := caps.XAddr
	if media == "" {
		media = xaddr
	}
	var profs struct {
		Profiles []struct {
			Token    string `xml:"token,attr"`
			Encoding string `xml:"VideoEncoderConfiguration>Encoding"`
			Width    int    `xml:"VideoEncoderConfiguration>Resolution>Width"`
			Height   int    `xml:"VideoEncoderConfiguration>Resolution>Height"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	if err := c.call(ctx, media, `<trt:GetProfiles/>`, &profs); err != nil {
		return nil, fmt.Errorf("GetProfiles: %w", err)
	}
	var out []Profile
	for _, p := range profs.Profiles {
		var su struct {
			URI string `xml:"Body>GetStreamUriResponse>MediaUri>Uri"`
		}
		body := `<trt:GetStreamUri><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup>` +
			`<trt:ProfileToken>` + xmlEscape(p.Token) + `</trt:ProfileToken></trt:GetStreamUri>`
		if err := c.call(ctx, media, body, &su); err != nil {
			return nil, fmt.Errorf("GetStreamUri %s: %w", p.Token, err)
		}
		out = append(out, Profile{Token: p.Token, Encoding: p.Encoding, Width: p.Width, Height: p.Height, URI: su.URI})
	}
	if len(out) == 0 {
		return nil, errors.New("the camera reported no media profiles")
	}
	return out, nil
}

type client struct {
	http       *http.Client
	user, pass string
	offset     time.Duration // camera clock minus our clock
}

// syncClock reads the camera's UTC clock (allowed without credentials) so WS-Security timestamps
// fall within the camera's tolerance even when its clock is wrong.
func (c *client) syncClock(ctx context.Context, xaddr string) {
	user := c.user
	c.user = ""
	defer func() { c.user = user }()
	var r struct {
		D struct {
			Hour   int `xml:"Time>Hour"`
			Minute int `xml:"Time>Minute"`
			Second int `xml:"Time>Second"`
			Year   int `xml:"Date>Year"`
			Month  int `xml:"Date>Month"`
			Day    int `xml:"Date>Day"`
		} `xml:"Body>GetSystemDateAndTimeResponse>SystemDateAndTime>UTCDateTime"`
	}
	if c.call(ctx, xaddr, `<tds:GetSystemDateAndTime/>`, &r) == nil && r.D.Year > 2000 {
		cam := time.Date(r.D.Year, time.Month(r.D.Month), r.D.Day, r.D.Hour, r.D.Minute, r.D.Second, 0, time.UTC)
		c.offset = time.Until(cam)
	}
}

func (c *client) call(ctx context.Context, to, body string, out any) error {
	env := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` +
		c.header() + `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to, strings.NewReader(env))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || bytes.Contains(b, []byte("NotAuthorized")) {
		return ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return xml.Unmarshal(b, out)
}

// header is a WS-Security UsernameToken with a password digest: base64(sha1(nonce+created+pass)).
func (c *client) header() string {
	if c.user == "" {
		return ""
	}
	nonce := make([]byte, 16)
	rand.Read(nonce)
	created := time.Now().Add(c.offset).UTC().Format("2006-01-02T15:04:05Z")
	sum := sha1.Sum(append(append(append([]byte{}, nonce...), created...), c.pass...))
	return `<s:Header><Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"><UsernameToken>` +
		`<Username>` + xmlEscape(c.user) + `</Username>` +
		`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + base64.StdEncoding.EncodeToString(sum[:]) + `</Password>` +
		`<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` + base64.StdEncoding.EncodeToString(nonce) + `</Nonce>` +
		`<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` + created + `</Created>` +
		`</UsernameToken></Security></s:Header>`
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// MainAndSub picks the highest-resolution profile as the main stream and the lowest as the
// substream ("" when there is only one), with credentials embedded.
func MainAndSub(ps []Profile, user, pass string) (main, sub string) {
	if len(ps) == 0 {
		return "", ""
	}
	sorted := append([]Profile(nil), ps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Width*sorted[i].Height > sorted[j].Width*sorted[j].Height
	})
	main = WithCredentials(sorted[0].URI, user, pass)
	if last := sorted[len(sorted)-1]; len(sorted) > 1 && last.URI != sorted[0].URI {
		sub = WithCredentials(last.URI, user, pass)
	}
	return main, sub
}

// WithCredentials puts user:pass into an RTSP URI, percent-encoded (so # / ? @ are safe).
func WithCredentials(uri, user, pass string) string {
	if user == "" {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	u.User = url.UserPassword(user, pass)
	return u.String()
}
