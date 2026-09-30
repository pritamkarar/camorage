package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"camorage/internal/storage"
)

// cloudListTimeout bounds listing a cloud folder: an unreachable cloud must not hold up the
// timeline, which shows the phone's own recordings too.
var cloudListTimeout = 15 * time.Second

type cloudSpanOut struct {
	File        string  `json:"file"`
	Start       string  `json:"start"` // RFC3339, phone local time
	DurationSec float64 `json:"durationSec"`
	Size        int64   `json:"size"`
}

// cloudDir is the folder of cam's clips for day in its cloud target, or "" without cloud copy.
func (s *server) cloudDir(cam string, day time.Time) string {
	cfg := s.d.Store.Get()
	c, _ := cfg.CameraByID(cam)
	if c.Cloud == nil || s.d.Cloud == nil {
		return ""
	}
	for _, t := range cfg.StorageTargets {
		if t.ID == c.Cloud.TargetID {
			return storage.Join(t.Remote, "camorage/"+cam+"/"+day.Format("2006-01-02"))
		}
	}
	return ""
}

// cloudList lists dir within cloudListTimeout, answering the request itself when it cannot.
func (s *server) cloudList(w http.ResponseWriter, r *http.Request, dir string) ([]storage.File, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), cloudListTimeout)
	defer cancel()
	files, err := s.d.Cloud.List(ctx, dir)
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		fail(w, http.StatusGatewayTimeout, "the cloud storage did not answer in time")
	case err != nil:
		fail(w, http.StatusBadGateway, err.Error())
	default:
		return files, true
	}
	return nil, false
}

// cloudSpans lists a camera's cloud clips for one local day (timeline blocks).
func (s *server) cloudSpans(w http.ResponseWriter, r *http.Request) {
	cam, day, ok := s.dayQuery(w, r)
	if !ok {
		return
	}
	out := []cloudSpanOut{}
	if dir := s.cloudDir(cam, day); dir != "" {
		files, ok := s.cloudList(w, r, dir)
		if !ok {
			return
		}
		for _, f := range files {
			if start, length, _, ok := storage.ParseClip(day, f.Name); ok {
				out = append(out, cloudSpanOut{f.Name, start.Format(time.RFC3339), length.Seconds(), f.Size})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	writeJSON(w, http.StatusOK, out)
}

// cloudClip streams a cloud clip, honouring one byte range, so the video element can seek.
func (s *server) cloudClip(w http.ResponseWriter, r *http.Request) {
	cam, day, ok := s.dayQuery(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("file")
	if !storage.ValidClipName(name) {
		fail(w, http.StatusBadRequest, "not a clip name")
		return
	}
	dir := s.cloudDir(cam, day)
	if dir == "" {
		fail(w, http.StatusNotFound, "this camera has no cloud copy")
		return
	}
	// ponytail: lists the folder for every range request (~1 s on Drive); cache it if seeking feels slow.
	files, ok := s.cloudList(w, r, dir)
	if !ok {
		return
	}
	size := int64(-1)
	for _, f := range files {
		if f.Name == name {
			size = f.Size
		}
	}
	if size < 0 {
		fail(w, http.StatusNotFound, "no such clip")
		return
	}
	from, to, partial, ok := byteRange(r.Header.Get("Range"), size)
	h := w.Header()
	h.Set("Accept-Ranges", "bytes")
	if !ok {
		h.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	h.Set("Content-Type", "video/mp4")
	h.Set("Content-Length", strconv.FormatInt(to-from+1, 10))
	if r.URL.Query().Get("download") == "1" {
		h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s_%s_%s"`, cam, day.Format("2006-01-02"), name))
	}
	body, err := s.d.Cloud.Cat(r.Context(), dir+"/"+name, from, to-from+1)
	if err != nil {
		h.Del("Content-Length")
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	defer body.Close()
	if partial {
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, size))
		w.WriteHeader(http.StatusPartialContent)
	}
	io.Copy(w, body)
}

// byteRange reads a single-range Range header against a file of size bytes: the inclusive range
// to send, whether it is partial, and false when it cannot be satisfied. An absent header is the
// whole file.
func byteRange(header string, size int64) (from, to int64, partial, ok bool) {
	if header == "" {
		return 0, size - 1, false, size > 0
	}
	spec, found := strings.CutPrefix(header, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	a, b, _ := strings.Cut(spec, "-")
	switch {
	case a == "": // the last b bytes
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, false
		}
		return max(size-n, 0), size - 1, true, size > 0
	default:
		start, err := strconv.ParseInt(a, 10, 64)
		if err != nil || start >= size {
			return 0, 0, false, false
		}
		end := size - 1
		if b != "" {
			if end, err = strconv.ParseInt(b, 10, 64); err != nil || end < start {
				return 0, 0, false, false
			}
			end = min(end, size-1)
		}
		return start, end, true, true
	}
}
