package motion

import (
	"errors"
	"io"
	"regexp"
	"strconv"
)

var versionRe = regexp.MustCompile(`ffmpeg version n?(\d+)\.(\d+)`)

// ParseVersion reads major.minor from `ffmpeg -version`. Unknown builds (e.g. git snapshots) get
// the newest flags.
func ParseVersion(out string) (major, minor int) {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return 99, 0
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor
}

// ReaderArgs is the keyframe reader for ffmpeg major.minor: it decodes only the keyframes of the
// RTSP stream at url, scaled to 64×36 gray, and writes them raw to stdout, one per keyframe
// (M0: ~3 % of one core per camera on the phone).
func ReaderArgs(major, minor int, url string) []string {
	timeout := []string{"-stimeout", "5000000"} // ffmpeg 4.x; its -timeout means "listen"
	if major >= 5 {
		timeout = []string{"-timeout", "5000000"}
	}
	vsync := []string{"-vsync", "0"} // without it ffmpeg 4.x duplicates frames to a constant rate
	if major > 5 || (major == 5 && minor >= 1) {
		vsync = []string{"-fps_mode", "passthrough"}
	}
	args := append([]string{"-nostdin", "-loglevel", "error", "-rtsp_transport", "tcp"}, timeout...)
	args = append(args, "-skip_frame", "nokey", "-i", url)
	args = append(args, vsync...)
	// flush each frame at once: the timestamp of a frame is when the portal reads it
	return append(args, "-vf", "scale=64:36,format=gray", "-flush_packets", "1", "-f", "rawvideo", "pipe:1")
}

// ReadFrames calls fn with each FrameSize-byte frame from r until r ends. fn must not keep the
// slice: it is reused.
func ReadFrames(r io.Reader, fn func([]byte)) error {
	buf := make([]byte, FrameSize)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		fn(buf)
	}
}
