package onvif

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A ProbeMatch as sent by the real camera (macro-video-soft).
const probeMatchXML = `<?xml version="1.0" encoding="UTF-8"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:wsdd="http://schemas.xmlsoap.org/ws/2005/04/discovery"><SOAP-ENV:Body><wsdd:ProbeMatches><wsdd:ProbeMatch><wsdd:XAddrs>http://192.168.1.129:8899/onvif/device_service</wsdd:XAddrs><wsdd:Scopes>onvif://www.onvif.org/location/country/china onvif://www.onvif.org/name/IP-Camera onvif://www.onvif.org/hardware/IPC%20BO</wsdd:Scopes></wsdd:ProbeMatch></wsdd:ProbeMatches></SOAP-ENV:Body></SOAP-ENV:Envelope>`

func TestDiscoverCollectsProbeMatches(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() { // a fake camera: answers every probe (the client sends two)
		buf := make([]byte, 64<<10)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if bytes.Contains(buf[:n], []byte("NetworkVideoTransmitter")) {
				pc.WriteTo([]byte(probeMatchXML), from)
			}
		}
	}()
	devs, err := discover(context.Background(), pc.LocalAddr().String(), 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("want 1 device (duplicates merged), got %+v", devs)
	}
	d := devs[0]
	if d.XAddr != "http://192.168.1.129:8899/onvif/device_service" || d.IP != "127.0.0.1" || d.Name != "IP-Camera" || d.Hardware != "IPC BO" {
		t.Fatalf("device = %+v", d)
	}
}

// validToken checks a WS-UsernameToken digest like a camera does (±5 min around its own clock).
func validToken(body, user, pass string, camNow time.Time) bool {
	if user == "" {
		return true
	}
	get := func(tag string) string {
		// the tag name must end here: "<Username" must not match "<UsernameToken>"
		m := regexp.MustCompile(`<(?:\w+:)?` + tag + `(?:\s[^>]*)?>([^<]*)<`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		return m[1]
	}
	nonce, err := base64.StdEncoding.DecodeString(get("Nonce"))
	if err != nil || get("Username") != user {
		return false
	}
	created := get("Created")
	ts, err := time.Parse("2006-01-02T15:04:05Z", created)
	if err != nil || ts.Sub(camNow) > 5*time.Minute || camNow.Sub(ts) > 5*time.Minute {
		return false
	}
	sum := sha1.Sum([]byte(string(nonce) + created + pass))
	return get("Password") == base64.StdEncoding.EncodeToString(sum[:])
}

// fakeCamera speaks just enough ONVIF: clock, capabilities, two profiles, their stream URIs.
func fakeCamera(t *testing.T, user, pass string, clockSkew time.Duration) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		reply := func(code int, inner string) {
			w.Header().Set("Content-Type", "application/soap+xml")
			w.WriteHeader(code)
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body>%s</s:Body></s:Envelope>`, inner)
		}
		camNow := time.Now().UTC().Add(clockSkew)
		if strings.Contains(body, "GetSystemDateAndTime") {
			reply(200, fmt.Sprintf(`<tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime><tt:UTCDateTime><tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time><tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
				camNow.Hour(), camNow.Minute(), camNow.Second(), camNow.Year(), int(camNow.Month()), camNow.Day()))
			return
		}
		if !validToken(body, user, pass, camNow) {
			reply(400, `<s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:NotAuthorized</s:Value></s:Subcode></s:Code></s:Fault>`)
			return
		}
		switch {
		case strings.Contains(body, "GetCapabilities"):
			reply(200, `<tds:GetCapabilitiesResponse><tds:Capabilities><tt:Media><tt:XAddr>`+srv.URL+`/onvif/media_service</tt:XAddr></tt:Media></tds:Capabilities></tds:GetCapabilitiesResponse>`)
		case strings.Contains(body, "GetProfiles"):
			reply(200, `<trt:GetProfilesResponse><trt:Profiles token="PROFILE_000"><tt:VideoEncoderConfiguration><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1280</tt:Width><tt:Height>720</tt:Height></tt:Resolution></tt:VideoEncoderConfiguration></trt:Profiles><trt:Profiles token="PROFILE_001"><tt:VideoEncoderConfiguration><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution></tt:VideoEncoderConfiguration></trt:Profiles></trt:GetProfilesResponse>`)
		case strings.Contains(body, "GetStreamUri"):
			ch := "0"
			if strings.Contains(body, "PROFILE_001") {
				ch = "1"
			}
			reply(200, `<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>rtsp://192.168.1.129/live/ch00_`+ch+`</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`)
		default:
			reply(400, `<s:Fault/>`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamsWithAuthAndCameraClockSkew(t *testing.T) {
	srv := fakeCamera(t, "admin", "p#ss", time.Hour) // the camera's clock is an hour ahead
	ps, err := Streams(context.Background(), srv.URL+"/onvif/device_service", "admin", "p#ss")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Token != "PROFILE_000" || ps[0].Width != 1280 || ps[0].Encoding != "H264" || ps[1].URI != "rtsp://192.168.1.129/live/ch00_1" {
		t.Fatalf("profiles = %+v", ps)
	}
}

func TestStreamsWithoutAuth(t *testing.T) {
	srv := fakeCamera(t, "", "", 0)
	if ps, err := Streams(context.Background(), srv.URL+"/onvif/device_service", "", ""); err != nil || len(ps) != 2 {
		t.Fatalf("got %+v, %v", ps, err)
	}
}

func TestStreamsWrongPassword(t *testing.T) {
	srv := fakeCamera(t, "admin", "right", 0)
	if _, err := Streams(context.Background(), srv.URL+"/onvif/device_service", "admin", "wrong"); !errors.Is(err, ErrAuth) {
		t.Fatalf("want ErrAuth, got %v", err)
	}
}

func TestStreamsRejectsNonHTTPXAddr(t *testing.T) {
	if _, err := Streams(context.Background(), "file:///etc/passwd", "", ""); err == nil {
		t.Fatal("expected an error for a non-http xaddr")
	}
}

func TestMainAndSubEncodesCredentials(t *testing.T) {
	ps := []Profile{{Token: "b", Width: 640, Height: 360, URI: "rtsp://1.2.3.4/sub"}, {Token: "a", Width: 1280, Height: 720, URI: "rtsp://1.2.3.4/main"}}
	main, sub := MainAndSub(ps, "admin", "p#ss/1")
	if main != "rtsp://admin:p%23ss%2F1@1.2.3.4/main" || sub != "rtsp://admin:p%23ss%2F1@1.2.3.4/sub" {
		t.Fatalf("main=%s sub=%s", main, sub)
	}
	main, sub = MainAndSub(ps[:1], "", "")
	if main != "rtsp://1.2.3.4/sub" || sub != "" {
		t.Fatalf("single profile: main=%s sub=%s", main, sub)
	}
}
