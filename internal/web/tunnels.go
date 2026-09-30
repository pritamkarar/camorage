package web

import (
	"net/http"
	"strings"

	"camorage/internal/config"
	"camorage/internal/tunnel"
)

// tunnels reports remote access: the saved settings (the token only as "set") over the live
// state from the tunnel manager, which may lag a save by a moment.
func (s *server) tunnels(w http.ResponseWriter, r *http.Request) {
	var st tunnel.Status
	if s.d.Tunnels != nil {
		st = s.d.Tunnels()
	}
	t := s.d.Store.Get().Tunnels
	if t.CloudflareToken == "" {
		st.Cloudflare = tunnel.Cloudflare{}
	}
	st.Cloudflare.TokenSet, st.Cloudflare.Hostname = t.CloudflareToken != "", t.CloudflareHostname
	if !t.TailscaleEnabled {
		st.Tailscale = tunnel.Tailscale{}
	}
	st.Tailscale.Enabled = t.TailscaleEnabled
	writeJSON(w, http.StatusOK, st)
}

// putCloudflare saves the tunnel token (write-only: an empty token keeps the saved one) and the
// public hostname Settings links to.
func (s *server) putCloudflare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Hostname string `json:"hostname"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	token := ""
	if strings.TrimSpace(in.Token) != "" {
		var err error
		if token, err = tunnel.ParseCloudflareToken(in.Token); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		if token != "" {
			c.Tunnels.CloudflareToken = token
		}
		if c.Tunnels.CloudflareToken == "" {
			return &config.ValidationError{Msg: "paste the tunnel token from the Cloudflare dashboard"}
		}
		c.Tunnels.CloudflareHostname = cleanHostname(in.Hostname)
		return nil
	})
	s.savedTunnels(w, err)
}

func (s *server) deleteCloudflare(w http.ResponseWriter, r *http.Request) {
	err := s.d.Store.Update(func(c *config.Config) error {
		c.Tunnels.CloudflareToken, c.Tunnels.CloudflareHostname = "", ""
		return nil
	})
	s.savedTunnels(w, err)
}

func (s *server) putTailscale(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	err := s.d.Store.Update(func(c *config.Config) error {
		c.Tunnels.TailscaleEnabled = in.Enabled
		return nil
	})
	s.savedTunnels(w, err)
}

// savedTunnels answers a tunnel settings change and, if it was saved, applies it.
func (s *server) savedTunnels(w http.ResponseWriter, err error) {
	if err != nil {
		storeError(w, err)
		return
	}
	if s.d.OnTunnelsChanged != nil {
		go s.d.OnTunnelsChanged()
	}
	writeJSON(w, http.StatusOK, okBody)
}

// cleanHostname turns what people paste ("https://Cams.Example.com/") into "cams.example.com";
// the config validates the result.
func cleanHostname(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	host, _, _ := strings.Cut(s, "/")
	return host
}
