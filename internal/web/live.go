package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"regexp"
	"strings"

	"camorage/internal/mediamtx"
)

// whepSessionRe matches MediaMTX's WHEP session ids (UUIDs).
var whepSessionRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// livePath reports whether p is the MediaMTX path of an enabled camera's main stream or
// substream; the live proxy refuses everything else.
func (s *server) livePath(p string) bool {
	cfg := s.d.Store.Get()
	for _, c := range cfg.Cameras {
		if c.Enabled && (p == c.ID || (c.SubURL != "" && p == mediamtx.SubPath(c.ID))) {
			return true
		}
	}
	return false
}

// hls proxies GET /live/hls/<path>/<file> to MediaMTX's HLS server.
func (s *server) hls(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	stream, _, _ := strings.Cut(rest, "/")
	// PathValue is decoded: "cam%2F..%2Fother" arrives as "cam/../other", which MediaMTX would
	// resolve to another path. Only paths that are already clean go through.
	if path.Clean("/"+rest) != "/"+rest || !s.livePath(stream) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.HLSBase, "/"+rest, func(loc string) string { return "/live/hls" + loc })
}

// whepOffer proxies POST /live/whep/<path> (an SDP offer) to MediaMTX's WHEP endpoint.
func (s *server) whepOffer(w http.ResponseWriter, r *http.Request) {
	stream := r.PathValue("path")
	if !s.livePath(stream) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.WebRTCBase, "/"+stream+"/whep", whepLocation)
}

// whepSession proxies PATCH (trickle ICE) and DELETE (hang up) of a WHEP session.
func (s *server) whepSession(w http.ResponseWriter, r *http.Request) {
	stream, session := r.PathValue("path"), r.PathValue("session")
	if !s.livePath(stream) || !whepSessionRe.MatchString(session) {
		fail(w, http.StatusNotFound, "no such stream")
		return
	}
	s.proxy(w, r, s.d.WebRTCBase, "/"+stream+"/whep/"+session, whepLocation)
}

// whepLocation maps MediaMTX's session URL "/<path>/whep/<id>[?q]" to "/live/whep/<path>/<id>[?q]".
func whepLocation(loc string) string {
	path, rest, found := strings.Cut(strings.TrimPrefix(loc, "/"), "/whep/")
	if !found {
		return loc
	}
	return "/live/whep/" + path + "/" + rest
}

// proxy forwards r to base+path, keeping the query. The portal's cookies stay here (MediaMTX then
// falls back to ?session= URLs), MediaMTX's own cookies are dropped, and absolute Location headers
// are mapped back into the portal's URL space by fixLocation.
func (s *server) proxy(w http.ResponseWriter, r *http.Request, base, path string, fixLocation func(string) string) {
	target, err := url.Parse(base)
	if err != nil || target.Host == "" {
		fail(w, http.StatusInternalServerError, "live proxy is not configured")
		return
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = target.Scheme, target.Host
			pr.Out.URL.Path, pr.Out.URL.RawPath = path, ""
			pr.Out.Host = target.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Origin") // already checked by the portal; keeps MediaMTX's CORS out of it
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			for k := range resp.Header {
				if strings.HasPrefix(k, "Access-Control-") { // MediaMTX allows any origin; the portal does not
					resp.Header.Del(k)
				}
			}
			if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, "/") {
				resp.Header.Set("Location", fixLocation(loc))
			}
			return nil
		},
	}
	rp.ServeHTTP(w, r)
}
